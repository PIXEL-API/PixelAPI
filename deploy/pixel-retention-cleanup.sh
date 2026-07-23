#!/usr/bin/env bash

set -euo pipefail

readonly retention_minutes=43200
readonly release_root="/opt/sub2api/releases"
readonly upload_root="/home/pixel/sub2api_release_uploads"
readonly lock_file="/run/pixel-release-maintenance.lock"

exec 9>"${lock_file}"
if ! flock -n 9; then
  echo "retention cleanup is already running" >&2
  exit 1
fi

resolved_release_root=$(readlink -f -- "${release_root}")
resolved_upload_root=$(readlink -f -- "${upload_root}")
current_release=$(readlink -f -- /opt/sub2api/current)

if [[ "${resolved_release_root}" != "${release_root}" ]]; then
  echo "unexpected release root: ${resolved_release_root}" >&2
  exit 1
fi
if [[ "${resolved_upload_root}" != "${upload_root}" ]]; then
  echo "unexpected upload root: ${resolved_upload_root}" >&2
  exit 1
fi
deleted_releases=0
deleted_uploads=0

while IFS= read -r -d '' candidate; do
  resolved_candidate=$(readlink -f -- "${candidate}")
  case "${resolved_candidate}" in
    "${resolved_release_root}"/*) ;;
    *)
      echo "unsafe release path: ${resolved_candidate}" >&2
      exit 1
      ;;
  esac

  # Re-read all protection pointers immediately before deletion. The deployment
  # switch uses the same flock so rollback/current cannot change inside this check.
  current_release=$(readlink -f -- /opt/sub2api/current)
  pixel_pid=$(systemctl show pixel.service -p MainPID --value)
  if [[ -z "${pixel_pid}" || "${pixel_pid}" == "0" ]]; then
    echo "pixel.service is not running; skipping release deletion"
    continue
  fi
  pixel_executable=$(readlink -f -- "/proc/${pixel_pid}/exe" || true)
  pixel_working_directory=$(readlink -f -- "/proc/${pixel_pid}/cwd" || true)

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
  if mountpoint -q -- "${resolved_candidate}"; then
    echo "skipping mounted release candidate: ${resolved_candidate}"
    continue
  fi

  rm -rf --one-file-system -- "${resolved_candidate}"
  deleted_releases=$((deleted_releases + 1))
done < <(
  find "${resolved_release_root}" -xdev -mindepth 1 -maxdepth 1 -type d \
    -mmin "+${retention_minutes}" -print0
)

while IFS= read -r -d '' candidate; do
  resolved_candidate=$(readlink -f -- "${candidate}")
  case "${resolved_candidate}" in
    "${resolved_upload_root}"/*) ;;
    *)
      echo "unsafe upload path: ${resolved_candidate}" >&2
      exit 1
      ;;
  esac

  rm -f -- "${resolved_candidate}"
  deleted_uploads=$((deleted_uploads + 1))
done < <(
  find "${resolved_upload_root}" -mindepth 1 -maxdepth 1 -type f \
    -mmin "+${retention_minutes}" -print0
)

find "${resolved_upload_root}" -mindepth 1 -maxdepth 1 -type d -empty \
  -mmin "+${retention_minutes}" -delete

printf 'retention cleanup completed: releases=%d uploads=%d current=%s\n' \
  "${deleted_releases}" "${deleted_uploads}" "${current_release}"
