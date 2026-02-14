#!/bin/bash
# 本地构建镜像的快速脚本，避免在命令行反复输入构建参数。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}"

VERSION_FILE="${REPO_ROOT}/backend/cmd/server/VERSION"
VERSION=""
if [ -f "${VERSION_FILE}" ]; then
  VERSION="$(tr -d '\r\n' < "${VERSION_FILE}")"
fi

if [ -z "${VERSION}" ]; then
  echo "未找到版本号（${VERSION_FILE} 为空或不存在），将仅构建 latest 镜像" >&2
  VERSION="docker"
fi

COMMIT="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo docker)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

docker build \
  -t "yangjianbo/aicodex2api:latest" \
  -t "yangjianbo/aicodex2api:${VERSION}" \
  --build-arg VERSION="${VERSION}" \
  --build-arg COMMIT="${COMMIT}" \
  --build-arg DATE="${DATE}" \
  --build-arg GOPROXY=https://goproxy.cn,direct \
  --build-arg GOSUMDB=sum.golang.google.cn \
  -f "${REPO_ROOT}/Dockerfile" \
  "${REPO_ROOT}"
