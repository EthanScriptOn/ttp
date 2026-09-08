#!/usr/bin/env bash
set -Eeuo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="$root_dir/deploy/docker-compose.yml"
compose_project="${COMPOSE_PROJECT_NAME:-cicd-platform}"
compose_args=(-p "$compose_project" -f "$compose_file")

docker_info_ready() {
  docker info >/dev/null 2>&1 &
  local docker_pid=$!
  for _ in {1..15}; do
    if ! kill -0 "$docker_pid" 2>/dev/null; then
      if wait "$docker_pid"; then
        return 0
      fi
      return $?
    fi
    sleep 1
  done
  kill "$docker_pid" 2>/dev/null || true
  wait "$docker_pid" 2>/dev/null || true
  return 124
}

is_enabled() {
  case "${1:-}" in
    true|TRUE|1|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

loopback_builder_url() {
  local address="$1"
  case "$address" in
    127.0.0.1:[0-9]*|localhost:[0-9]*|'[::1]':[0-9]*) printf 'http://%s\n' "$address" ;;
    *) return 1 ;;
  esac
}

wait_for_builder() {
  local url="$1"
  local pid="$2"
  command -v curl >/dev/null 2>&1 || { printf 'curl is required when TTP_BUILDER_ENABLED=true\n' >&2; return 127; }
  for _ in {1..45}; do
    if curl --fail --silent --show-error --max-time 1 "${url%/}/health" >/dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      return 1
    fi
    sleep 1
  done
  return 1
}

required=(MYSQL_PASSWORD MYSQL_ROOT_PASSWORD CICD_JWT_SECRET CICD_GIT_CREDENTIAL_KEY CICD_KUBE_CLUSTER_ID)
for name in "${required[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    printf '%s must be set before starting TTP\n' "$name" >&2
    exit 2
  fi
done
command -v docker >/dev/null 2>&1 || { printf 'docker is required\n' >&2; exit 127; }
if [[ -n "${GO_BIN:-}" ]]; then
  go_bin="$GO_BIN"
elif command -v go >/dev/null 2>&1; then
  go_bin="$(command -v go)"
elif [[ -x /opt/homebrew/opt/go/libexec/bin/go ]]; then
  go_bin=/opt/homebrew/opt/go/libexec/bin/go
else
  printf 'go is required\n' >&2
  exit 127
fi
command -v pnpm >/dev/null 2>&1 || { printf 'pnpm is required\n' >&2; exit 127; }
if ! docker_info_ready; then
  raw_file="$HOME/Library/Containers/com.docker.docker/Data/vms/0/data/Docker.raw"
  if [[ -f "$raw_file" && ! -w "$raw_file" ]]; then
    printf 'Docker daemon is unavailable because Docker.raw is not writable by %s.\n' "$(id -un)" >&2
    printf 'Fix it with: sudo chown %s:staff %s\n' "$(id -un)" "$raw_file" >&2
  else
    printf 'Docker daemon is unavailable; start Docker Desktop before starting TTP.\n' >&2
  fi
  exit 1
fi

export MYSQL_DATABASE="${MYSQL_DATABASE:-cicd_platform}"
export MYSQL_USER="${MYSQL_USER:-cicd_app}"
export MYSQL_PORT="${MYSQL_PORT:-3306}"
export CICD_ADDR="${CICD_ADDR:-:8790}"
export CICD_RUNTIME_PROVIDER="${CICD_RUNTIME_PROVIDER:-kubernetes}"
export CICD_MYSQL_DSN="${CICD_MYSQL_DSN:-${MYSQL_USER}:${MYSQL_PASSWORD}@tcp(127.0.0.1:${MYSQL_PORT})/${MYSQL_DATABASE}?parseTime=true&charset=utf8mb4&loc=UTC}"

builder_enabled="${TTP_BUILDER_ENABLED:-false}"
builder_pid=''
buildkit_pid=''
if is_enabled "$builder_enabled"; then
  : "${TTP_BUILDER_TOKEN:?TTP_BUILDER_TOKEN must be set when TTP_BUILDER_ENABLED=true}"
  : "${TTP_BUILDER_BUILDKIT_ADDR:?TTP_BUILDER_BUILDKIT_ADDR must be set when TTP_BUILDER_ENABLED=true}"
  : "${TTP_BUILDER_REGISTRY_CREDENTIALS_FILE:?TTP_BUILDER_REGISTRY_CREDENTIALS_FILE must be set when TTP_BUILDER_ENABLED=true}"
  : "${TTP_BUILDER_ALLOWED_GIT_HOSTS:?TTP_BUILDER_ALLOWED_GIT_HOSTS must be set when TTP_BUILDER_ENABLED=true}"
  : "${TTP_BUILDER_IMAGE_REPOSITORY_PREFIX:?TTP_BUILDER_IMAGE_REPOSITORY_PREFIX must be set when TTP_BUILDER_ENABLED=true}"
  : "${TTP_BUILDER_REGISTRY_CREDENTIAL_REF:?TTP_BUILDER_REGISTRY_CREDENTIAL_REF must be set when TTP_BUILDER_ENABLED=true}"
  : "${TTP_BUILDER_IMAGE_PLATFORMS:?TTP_BUILDER_IMAGE_PLATFORMS must be set when TTP_BUILDER_ENABLED=true}"
  export TTP_BUILDER_ADDR="${TTP_BUILDER_ADDR:-127.0.0.1:8791}"
  local_builder_url="$(loopback_builder_url "$TTP_BUILDER_ADDR" || true)"
  if [[ -z "$local_builder_url" ]]; then
    printf 'TTP_BUILDER_ADDR must bind to 127.0.0.1, localhost, or [::1] when TTP_BUILDER_ENABLED=true.\n' >&2
    exit 2
  fi
  if [[ -n "${CICD_IMAGE_BUILDER_URL:-}" && "${CICD_IMAGE_BUILDER_URL%/}" != "$local_builder_url" ]]; then
    printf 'CICD_IMAGE_BUILDER_URL must match TTP_BUILDER_ADDR when the local builder is enabled.\n' >&2
    printf 'For a remote HTTPS builder, leave TTP_BUILDER_ENABLED=false and configure CICD_IMAGE_BUILDER_URL directly.\n' >&2
    exit 2
  fi
  if [[ -n "${CICD_IMAGE_BUILDER_TOKEN:-}" && "$CICD_IMAGE_BUILDER_TOKEN" != "$TTP_BUILDER_TOKEN" ]]; then
    printf 'CICD_IMAGE_BUILDER_TOKEN must match TTP_BUILDER_TOKEN when the local builder is enabled.\n' >&2
    exit 2
  fi
  export CICD_IMAGE_BUILDER_URL="$local_builder_url"
  export CICD_IMAGE_BUILDER_TOKEN="$TTP_BUILDER_TOKEN"
  export TTP_BUILDER_WORKDIR="${TTP_BUILDER_WORKDIR:-$root_dir/.runtime/ttp-builder}"
  builder_health_url="$local_builder_url"
fi

backend_port="${CICD_ADDR##*:}"
if [[ -z "$backend_port" || "$backend_port" == "$CICD_ADDR" ]]; then
  backend_port=8790
fi

docker compose "${compose_args[@]}" up -d --wait mysql

backend_pid=''
frontend_pid=''
cleanup() {
  trap - INT TERM EXIT
  [[ -z "$frontend_pid" ]] || kill "$frontend_pid" 2>/dev/null || true
  [[ -z "$backend_pid" ]] || kill "$backend_pid" 2>/dev/null || true
  [[ -z "$builder_pid" ]] || kill "$builder_pid" 2>/dev/null || true
  [[ -z "$buildkit_pid" ]] || kill "$buildkit_pid" 2>/dev/null || true
}
trap cleanup INT TERM EXIT

if is_enabled "$builder_enabled"; then
  if is_enabled "${TTP_BUILDER_START_BUILDKIT:-false}"; then
    buildkitd_bin="${TTP_BUILDER_BUILDKITD:-buildkitd}"
    command -v "$buildkitd_bin" >/dev/null 2>&1 || { printf 'buildkitd is required when TTP_BUILDER_START_BUILDKIT=true\n' >&2; exit 127; }
    "$buildkitd_bin" --addr "$TTP_BUILDER_BUILDKIT_ADDR" &
    buildkit_pid=$!
  fi
  (cd "$root_dir/backend" && exec "$go_bin" run ./cmd/builder) &
  builder_pid=$!
  if ! wait_for_builder "$builder_health_url" "$builder_pid"; then
    printf 'ttp-builder did not become healthy; see its process output above.\n' >&2
    exit 1
  fi
fi

(cd "$root_dir/backend" && exec "$go_bin" run ./cmd/server) &
backend_pid=$!
(cd "$root_dir/frontend" && exec pnpm run dev -- --port "${FRONTEND_PORT:-5173}") &
frontend_pid=$!
printf 'TTP frontend: http://127.0.0.1:%s\n' "${FRONTEND_PORT:-5173}"
printf 'TTP backend: http://127.0.0.1:%s\n' "$backend_port"
if [[ -n "$builder_pid" ]]; then
  printf 'TTP builder: %s\n' "$CICD_IMAGE_BUILDER_URL"
fi

while true; do
  if ! kill -0 "$backend_pid" 2>/dev/null; then
    break
  fi
  if ! kill -0 "$frontend_pid" 2>/dev/null; then
    printf 'frontend process exited; stopping backend\n' >&2
    break
  fi
  if [[ -n "$builder_pid" ]] && ! kill -0 "$builder_pid" 2>/dev/null; then
    printf 'builder process exited; stopping TTP\n' >&2
    break
  fi
  if [[ -n "$buildkit_pid" ]] && ! kill -0 "$buildkit_pid" 2>/dev/null; then
    printf 'BuildKit daemon exited; stopping TTP\n' >&2
    break
  fi
  sleep 1
done
wait "$backend_pid" || true
