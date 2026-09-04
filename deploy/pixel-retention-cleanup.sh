#!/usr/bin/env bash

set -euo pipefail

readonly retention_minutes=43200
readonly release_root="/opt/sub2api/releases"
readonly maintenance_root="/opt/sub2api/release-maintenance"
readonly upload_root="${maintenance_root}/uploads"
readonly quarantine_root="${maintenance_root}/quarantine"
readonly lock_file="/run/pixel-release-maintenance.lock"

for required_command in dirname find findmnt flock id mktemp mv readlink rm rmdir stat systemctl; do
  if ! command -v "${required_command}" >/dev/null 2>&1; then
    echo "required command is unavailable: ${required_command}" >&2
    exit 1
  fi
done

exec 9>"${lock_file}"
if ! flock -n 9; then
  echo "retention cleanup is already running" >&2
  exit 1
fi

resolved_release_root=$(readlink -f -- "${release_root}")
current_release=$(readlink -f -- /opt/sub2api/current)

if [[ "${resolved_release_root}" != "${release_root}" ]]; then
  echo "unexpected release root: ${resolved_release_root}" >&2
  exit 1
fi
release_root_owner=$(stat -c %u -- "${resolved_release_root}")
release_root_writable=$(find "${resolved_release_root}" -xdev -maxdepth 0 \
  -type d -perm /022 -print -quit) || exit 1
if [[ "${release_root_owner}" != "0" || -n "${release_root_writable}" ]]; then
  echo "release root must be root-owned and not group/other writable" >&2
  exit 1
fi

expected_upload_uid=$(id -u s766)
expected_upload_gid=$(id -g s766)
[[ "${expected_upload_uid}" =~ ^[0-9]+$ && "${expected_upload_gid}" =~ ^[0-9]+$ ]] || exit 1
resolved_maintenance_root=$(readlink -e -- "${maintenance_root}") || {
  echo "release maintenance root is missing or unreadable: ${maintenance_root}" >&2
  exit 1
}
resolved_upload_root=$(readlink -e -- "${upload_root}") || {
  echo "release upload root is missing or unreadable: ${upload_root}" >&2
  exit 1
}
resolved_quarantine_root=$(readlink -e -- "${quarantine_root}") || {
  echo "retention quarantine is missing or unreadable: ${quarantine_root}" >&2
  exit 1
}
if [[ "${resolved_maintenance_root}" != "${maintenance_root}" ||
      "${resolved_upload_root}" != "${upload_root}" ||
      "${resolved_quarantine_root}" != "${quarantine_root}" ||
      -L "${maintenance_root}" || ! -d "${maintenance_root}" ||
      -L "${upload_root}" || ! -d "${upload_root}" ||
      -L "${quarantine_root}" || ! -d "${quarantine_root}" ||
      "$(dirname -- "${resolved_upload_root}")" != "${resolved_maintenance_root}" ||
      "$(dirname -- "${resolved_quarantine_root}")" != "${resolved_maintenance_root}" ]]; then
  echo "release maintenance paths must be canonical direct children" >&2
  exit 1
fi
maintenance_identity=$(stat -c '%u:%g:%a' -- "${resolved_maintenance_root}")
upload_identity=$(stat -c '%u:%g:%a' -- "${resolved_upload_root}")
quarantine_owner=$(stat -c '%u:%g' -- "${quarantine_root}")
quarantine_mode=$(stat -c %a -- "${quarantine_root}")
if [[ "${maintenance_identity}" != "0:${expected_upload_gid}:750" ||
      "${upload_identity}" != "0:${expected_upload_gid}:1730" ||
      "${quarantine_owner}" != "0:0" || "${quarantine_mode}" != "700" ]]; then
  echo "release maintenance paths have unexpected owner, group, or mode" >&2
  exit 1
fi
upload_mount=$(findmnt --raw --noheadings --output TARGET --target "${resolved_upload_root}") || exit 1
quarantine_mount=$(findmnt --raw --noheadings --output TARGET --target "${resolved_quarantine_root}") || exit 1
upload_mount=$(readlink -e -- "${upload_mount}") || exit 1
quarantine_mount=$(readlink -e -- "${quarantine_mount}") || exit 1
upload_device=$(stat -c %d -- "${resolved_upload_root}")
quarantine_device=$(stat -c %d -- "${resolved_quarantine_root}")
upload_mountpoint=$(stat -c %m -- "${resolved_upload_root}") || exit 1
quarantine_mountpoint=$(stat -c %m -- "${resolved_quarantine_root}") || exit 1
if [[ "${upload_mount}" != "${quarantine_mount}" ||
      "${upload_device}" != "${quarantine_device}" ||
      "${upload_mountpoint}" != "${quarantine_mountpoint}" ]]; then
  echo "upload and retention quarantine must share one mount and device" >&2
  exit 1
fi
if [[ "${quarantine_mount}" == "${resolved_quarantine_root}" ]]; then
  echo "retention quarantine must not be a separate mountpoint" >&2
  exit 1
fi
if [[ -L "${quarantine_root}" || ! -d "${quarantine_root}" ]]; then
  echo "retention quarantine must be root:root mode 0700" >&2
  exit 1
fi
stale_quarantine_entry=$(find "${quarantine_root}" -xdev -mindepth 1 \
  -maxdepth 1 -print -quit) || exit 1
if [[ -n "${stale_quarantine_entry}" ]]; then
  echo "stale retention quarantine requires manual recovery: ${stale_quarantine_entry}" >&2
  exit 1
fi
deleted_releases=0
deleted_uploads=0
candidate_list=$(mktemp "${quarantine_root}/.candidates.XXXXXXXXXX")
readonly candidate_list

cleanup_candidate_list() {
  rm -f -- "${candidate_list}"
}
trap cleanup_candidate_list EXIT

find "${resolved_release_root}" -xdev -mindepth 1 -maxdepth 1 -type d \
  -mmin "+${retention_minutes}" -printf '%D:%i\0%p\0' > "${candidate_list}"

while IFS= read -r -d '' listed_identity &&
      IFS= read -r -d '' candidate; do
  if [[ "$(dirname -- "${candidate}")" != "${resolved_release_root}" ]]; then
    echo "unsafe release directory entry: ${candidate}" >&2
    exit 1
  fi

  revalidation_rc=0
  revalidated_identity=$(find "${candidate}" -xdev -maxdepth 0 -type d \
    -mmin "+${retention_minutes}" -printf '%D:%i' 2>/dev/null) || revalidation_rc=$?
  if ((revalidation_rc != 0)); then
    if [[ ! -e "${candidate}" && ! -L "${candidate}" ]]; then
      echo "skipping vanished release entry: ${candidate}"
      continue
    fi
    echo "release entry revalidation failed: ${candidate}" >&2
    exit 1
  fi
  if [[ "${revalidated_identity}" != "${listed_identity}" ||
        -L "${candidate}" || ! -d "${candidate}" ]]; then
    echo "skipping replaced or refreshed release entry: ${candidate}"
    continue
  fi
  resolved_candidate=$(readlink -e -- "${candidate}") || {
    if [[ ! -e "${candidate}" && ! -L "${candidate}" ]]; then
      echo "skipping vanished release entry: ${candidate}"
      continue
    fi
    echo "release entry canonicalization failed: ${candidate}" >&2
    exit 1
  }
  if [[ "${resolved_candidate}" != "${candidate}" ]]; then
    echo "skipping non-canonical release entry: ${candidate}"
    continue
  fi

  # Re-read all protection pointers immediately before deletion. The deployment
  # switch uses the same flock so rollback/current cannot change inside this check.
  current_release=$(readlink -f -- /opt/sub2api/current)
  pixel_pid=$(systemctl show pixel.service -p MainPID --value) || exit 1
  if [[ ! "${pixel_pid}" =~ ^[0-9]+$ ]]; then
    echo "pixel.service returned an invalid MainPID" >&2
    exit 1
  fi
  if [[ "${pixel_pid}" == "0" ]]; then
    echo "pixel.service is not running; skipping release deletion"
    continue
  fi
  pixel_executable=$(readlink -e -- "/proc/${pixel_pid}/exe") || {
    echo "cannot prove the active pixel executable for PID ${pixel_pid}" >&2
    exit 1
  }
  pixel_working_directory=$(readlink -e -- "/proc/${pixel_pid}/cwd") || {
    echo "cannot prove the active pixel working directory for PID ${pixel_pid}" >&2
    exit 1
  }
  pixel_pid_after=$(systemctl show pixel.service -p MainPID --value) || exit 1
  if [[ ! "${pixel_pid_after}" =~ ^[1-9][0-9]*$ || "${pixel_pid_after}" != "${pixel_pid}" ]]; then
    echo "pixel.service PID changed during release protection checks" >&2
    exit 1
  fi

  if [[ "${resolved_candidate}" == "${current_release}" ]]; then
    echo "skipping current release: ${resolved_candidate}"
    continue
  fi
  case "${pixel_executable}" in
    "${resolved_candidate}"/*)
      echo "skipping active executable release: ${resolved_candidate}"
      continue
      ;;
  esac
  case "${pixel_working_directory}" in
    "${resolved_candidate}"|"${resolved_candidate}"/*)
      echo "skipping active working directory: ${resolved_candidate}"
      continue
      ;;
  esac
  candidate_mount=$(findmnt --raw --noheadings --output TARGET --target "${resolved_candidate}") || {
    echo "cannot determine release mount identity: ${resolved_candidate}" >&2
    exit 1
  }
  candidate_mount=$(readlink -e -- "${candidate_mount}") || exit 1
  if [[ "${candidate_mount}" == "${resolved_candidate}" ]]; then
    echo "skipping mounted release candidate: ${resolved_candidate}"
    continue
  fi

  delete_revalidation_rc=0
  delete_identity=$(find "${candidate}" -xdev -maxdepth 0 -type d \
    -mmin "+${retention_minutes}" -printf '%D:%i' 2>/dev/null) || delete_revalidation_rc=$?
  if ((delete_revalidation_rc != 0)); then
    if [[ ! -e "${candidate}" && ! -L "${candidate}" ]]; then
      echo "skipping vanished release entry before deletion: ${candidate}"
      continue
    fi
    echo "release entry final revalidation failed: ${candidate}" >&2
    exit 1
  fi
  if [[ "${delete_identity}" != "${listed_identity}" ||
        -L "${candidate}" || ! -d "${candidate}" ]]; then
    echo "skipping replaced or refreshed release entry before deletion: ${candidate}"
    continue
  fi

  # Delete the original direct directory entry, never its readlink target. GNU
  # rm unlinks a command-line symlink instead of traversing it if a privileged
  # actor replaces the entry after the final identity check.
  rm -rf --one-file-system -- "${candidate}"
  deleted_releases=$((deleted_releases + 1))
done < "${candidate_list}"

: > "${candidate_list}"
find "${resolved_upload_root}" -xdev -mindepth 1 -maxdepth 1 -type f \
  -uid "${expected_upload_uid}" -links 1 -mmin "+${retention_minutes}" \
  -printf '%D:%i\0%p\0' > "${candidate_list}"

while IFS= read -r -d '' listed_identity &&
      IFS= read -r -d '' candidate; do
    if [[ "$(dirname -- "${candidate}")" != "${resolved_upload_root}" ]]; then
      echo "unsafe upload directory entry: ${candidate}" >&2
      exit 1
    fi

    # The upload directory is writable by the SSH user. Revalidate the exact
    # regular-file inode and age, then atomically move the current directory
    # entry into a root-only same-filesystem quarantine. Only the quarantined
    # inode is deleted; a concurrent replacement at the original path is not.
    revalidation_rc=0
    revalidated_identity=$(find "${candidate}" -xdev -maxdepth 0 -type f \
      -uid "${expected_upload_uid}" -links 1 -mmin "+${retention_minutes}" \
      -printf '%D:%i' 2>/dev/null) || revalidation_rc=$?
    if ((revalidation_rc != 0)); then
      if [[ ! -e "${candidate}" && ! -L "${candidate}" ]]; then
        echo "skipping vanished upload entry: ${candidate}"
        continue
      fi
      echo "upload entry revalidation failed: ${candidate}" >&2
      exit 1
    fi
    if [[ "${revalidated_identity}" != "${listed_identity}" ]]; then
      echo "skipping replaced or refreshed upload entry: ${candidate}"
      continue
    fi
    if [[ -L "${candidate}" || ! -f "${candidate}" ]]; then
      echo "skipping non-regular upload entry: ${candidate}"
      continue
    fi

    quarantine_dir=$(mktemp -d "${quarantine_root}/upload.XXXXXXXXXX")
    quarantine_entry="${quarantine_dir}/entry"
    move_rc=0
    mv -T -- "${candidate}" "${quarantine_entry}" || move_rc=$?
    if ((move_rc != 0)); then
      rmdir -- "${quarantine_dir}"
      if [[ ! -e "${candidate}" && ! -L "${candidate}" ]]; then
        echo "skipping vanished upload entry before quarantine: ${candidate}"
        continue
      fi
      echo "upload entry quarantine move failed: ${candidate}" >&2
      exit 1
    fi

    moved_validation_rc=0
    moved_identity=$(find "${quarantine_entry}" -xdev -maxdepth 0 -type f \
      -uid "${expected_upload_uid}" -links 1 -mmin "+${retention_minutes}" \
      -printf '%D:%i') || moved_validation_rc=$?
    if ((moved_validation_rc != 0)); then
      restore_rc=0
      mv -T -n -- "${quarantine_entry}" "${candidate}" || restore_rc=$?
      if ((restore_rc == 0)) &&
         [[ ! -e "${quarantine_entry}" && ! -L "${quarantine_entry}" ]]; then
        rmdir -- "${quarantine_dir}"
      else
        echo "upload evidence read failed and the entry is preserved for manual recovery: ${quarantine_entry}" >&2
      fi
      echo "quarantined upload entry revalidation failed: ${candidate}" >&2
      exit 1
    fi
    if [[ "${moved_identity}" != "${listed_identity}" ||
          -L "${quarantine_entry}" || ! -f "${quarantine_entry}" ]]; then
      restore_rc=0
      mv -T -n -- "${quarantine_entry}" "${candidate}" || restore_rc=$?
      if ((restore_rc == 0)) &&
         [[ ! -e "${quarantine_entry}" && ! -L "${quarantine_entry}" ]]; then
        rmdir -- "${quarantine_dir}"
        echo "restored replaced or refreshed upload entry: ${candidate}"
        continue
      fi
      echo "upload entry changed during cleanup and is preserved for manual recovery: ${quarantine_entry}" >&2
      exit 1
    fi

    rm -f -- "${quarantine_entry}"
    rmdir -- "${quarantine_dir}"
    deleted_uploads=$((deleted_uploads + 1))
done < "${candidate_list}"

unexpected_upload_entry=$(find "${resolved_upload_root}" -xdev -mindepth 1 \
  -maxdepth 1 \( ! -type f -o ! -uid "${expected_upload_uid}" -o ! -links 1 \) \
  -print -quit) || exit 1
if [[ -n "${unexpected_upload_entry}" ]]; then
  echo "unexpected non-file entry remains in upload root: ${unexpected_upload_entry}" >&2
  exit 1
fi

printf 'retention cleanup completed: releases=%d uploads=%d current=%s\n' \
  "${deleted_releases}" "${deleted_uploads}" "${current_release}"
