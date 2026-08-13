#!/usr/bin/env sh
set -eu

suite="${1:-contract}"
case "$suite" in
  contract|live) ;;
  *)
    echo "usage: $0 [contract|live]" >&2
    exit 2
    ;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
backend_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)

run_live_smoke() {
  : "${BASE_URL:?BASE_URL is required for live provider smoke}"
  echo "Running live provider smoke against $BASE_URL"
  echo "Provider credentials may be omitted individually, but E2E_LIVE_MIN_ATTEMPTS must still be satisfied."
  (
    cd "$backend_dir"
    E2E_SUITE=live go test \
      -tags=e2e \
      -count=1 \
      -v \
      -timeout=30m \
      -run='^(TestClaude|TestGemini)' \
      ./internal/integration/...
  )
}

if [ "$suite" = "live" ]; then
  run_live_smoke
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required for provider-free contract E2E" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not available" >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required for provider-free contract E2E readiness checks" >&2
  exit 1
fi

run_id="${CI_JOB_ID:-local}-$$-$(date +%s)"
safe_id=$(printf '%s' "$run_id" | tr -c '[:alnum:]' '-')
network="sub2api-e2e-$safe_id"
postgres_container="sub2api-e2e-postgres-$safe_id"
redis_container="sub2api-e2e-redis-$safe_id"
app_container="sub2api-e2e-app-$safe_id"
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/sub2api-e2e.XXXXXX")
server_binary="$temp_dir/sub2api-server"
pricing_file="$backend_dir/resources/model-pricing/model_prices_and_context_window.json"
container_arch=$(go env GOARCH)
case "$container_arch" in
  amd64|arm64) ;;
  *)
    echo "unsupported Docker E2E architecture: $container_arch" >&2
    exit 1
    ;;
esac

cleanup() {
  docker rm -f "$app_container" "$redis_container" "$postgres_container" >/dev/null 2>&1 || true
  docker image rm -f "$app_container:contract" >/dev/null 2>&1 || true
  docker network rm "$network" >/dev/null 2>&1 || true
  rm -rf "$temp_dir"
}
trap cleanup EXIT INT TERM

echo "Building isolated linux contract-test server"
if [ ! -f "$pricing_file" ]; then
  echo "pricing fallback file is required for provider-free contract E2E: $pricing_file" >&2
  exit 1
fi
(
  cd "$backend_dir"
  CGO_ENABLED=0 GOOS=linux GOARCH="$container_arch" \
    go build -trimpath -o "$server_binary" ./cmd/server
)
cp "$pricing_file" "$temp_dir/model_pricing.json"
docker build \
  --platform "linux/$container_arch" \
  --tag "$app_container:contract" \
  --file - \
  "$temp_dir" >/dev/null <<DOCKERFILE
FROM scratch
COPY $(basename "$server_binary") /sub2api-server
COPY model_pricing.json /data/model_pricing.json
ENTRYPOINT ["/sub2api-server"]
DOCKERFILE

docker network create "$network" >/dev/null
docker run --detach --rm \
  --name "$postgres_container" \
  --network "$network" \
  --network-alias postgres \
  -e POSTGRES_USER=sub2api \
  -e POSTGRES_PASSWORD=contract-postgres-password \
  -e POSTGRES_DB=sub2api \
  postgres:18.1-alpine3.23 >/dev/null

docker run --detach --rm \
  --name "$redis_container" \
  --network "$network" \
  --network-alias redis \
  redis:8.4-alpine >/dev/null

wait_for_dependency() {
  name=$1
  check=$2
  attempts=0
  while [ "$attempts" -lt 60 ]; do
    if docker exec "$name" sh -c "$check" >/dev/null 2>&1; then
      return 0
    fi
    attempts=$((attempts + 1))
    sleep 1
  done
  echo "container $name did not become ready" >&2
  docker logs "$name" >&2 || true
  return 1
}

wait_for_dependency "$postgres_container" "pg_isready -U sub2api -d sub2api"
wait_for_dependency "$redis_container" "redis-cli ping"

MSYS_NO_PATHCONV=1 docker run --detach \
  --name "$app_container" \
  --network "$network" \
  -p 127.0.0.1::8080 \
  --tmpfs /tmp/sub2api-data:rw,nosuid,nodev,mode=0700 \
  -e DATA_DIR=/tmp/sub2api-data \
  -e AUTO_SETUP=true \
  -e SERVER_HOST=0.0.0.0 \
  -e SERVER_PORT=8080 \
  -e SERVER_MODE=release \
  -e RUN_MODE=standard \
  -e DATABASE_HOST=postgres \
  -e DATABASE_PORT=5432 \
  -e DATABASE_USER=sub2api \
  -e DATABASE_PASSWORD=contract-postgres-password \
  -e DATABASE_DBNAME=sub2api \
  -e DATABASE_SSLMODE=disable \
  -e REDIS_HOST=redis \
  -e REDIS_PORT=6379 \
  -e REDIS_DB=0 \
  -e PRICING_DATA_DIR=/data \
  -e PRICING_HASH_URL=disabled \
  -e ADMIN_EMAIL=contract-admin@test.local \
  -e ADMIN_PASSWORD=ContractAdminPassword12345 \
  -e JWT_SECRET=contract-jwt-secret-at-least-32-bytes \
  -e TOTP_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
  -e TZ=UTC \
  "$app_container:contract" >/dev/null

host_port=$(docker inspect --format='{{(index (index .NetworkSettings.Ports "8080/tcp") 0).HostPort}}' "$app_container")
if [ -z "$host_port" ]; then
  echo "failed to resolve contract-test server port" >&2
  exit 1
fi

attempts=0
while [ "$attempts" -lt 180 ]; do
  if curl --silent --show-error --fail --max-time 2 \
    "http://127.0.0.1:$host_port/health/ready" >/dev/null 2>&1; then
    break
  fi
  if [ "$(docker inspect --format='{{.State.Running}}' "$app_container" 2>/dev/null || printf false)" != "true" ]; then
    echo "contract-test server exited before readiness" >&2
    docker logs "$app_container" >&2 || true
    exit 1
  fi
  attempts=$((attempts + 1))
  sleep 1
done
if [ "$attempts" -ge 180 ]; then
  echo "contract-test server did not become ready" >&2
  docker logs "$app_container" >&2 || true
  exit 1
fi

echo "Running provider-free contract E2E against isolated PostgreSQL and Redis"
if ! (
  cd "$backend_dir"
  BASE_URL="http://127.0.0.1:$host_port" \
  ADMIN_EMAIL=contract-admin@test.local \
  ADMIN_PASSWORD=ContractAdminPassword12345 \
  E2E_SUITE=contract \
  E2E_ALLOW_MUTATION=true \
  go test \
      -tags=e2e \
      -count=1 \
      -v \
      -timeout=10m \
      -run='^TestContract' \
      ./internal/integration/...
); then
  echo "contract E2E failed; isolated server log follows" >&2
  docker logs "$app_container" >&2 || true
  exit 1
fi

exit 0
