#!/usr/bin/env bash
set -Eeuo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="$root_dir/deploy/docker-compose.yml"
project_name="${COMPOSE_PROJECT_NAME:-cicd-platform}"
compose_args=(-p "$project_name" -f "$compose_file")

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

if [[ "${CONFIRM_RESET:-}" != "YES" ]]; then
  printf 'This clears TTP business data from the local MySQL database.\n' >&2
  printf 'The MySQL volume, schema, migration files, and database container are preserved.\n' >&2
  printf 'Re-run with CONFIRM_RESET=YES after confirming the data is disposable.\n' >&2
  exit 2
fi
command -v docker >/dev/null 2>&1 || { printf 'docker is required\n' >&2; exit 127; }
if ! docker_info_ready; then
  raw_file="$HOME/Library/Containers/com.docker.docker/Data/vms/0/data/Docker.raw"
  if [[ -f "$raw_file" && ! -w "$raw_file" ]]; then
    printf 'Docker daemon is unavailable because Docker.raw is not writable by %s.\n' "$(id -un)" >&2
    printf 'Fix it with: sudo chown %s:staff %s\n' "$(id -un)" "$raw_file" >&2
  else
    printf 'Docker daemon is unavailable; start Docker Desktop before resetting the database.\n' >&2
  fi
  exit 1
fi

find_mysql_container() {
  docker ps -aq \
    --filter "label=com.docker.compose.project=$project_name" \
    --filter 'label=com.docker.compose.service=mysql' \
    | head -n 1
}

wait_for_mysql() {
  local container="$1"
  local state=""
  local health=""
  for _ in {1..30}; do
    state="$(docker inspect --format '{{.State.Status}}' "$container" 2>/dev/null || true)"
    if [[ "$state" != "running" ]]; then
      printf 'MySQL container is not running (state: %s)\n' "${state:-unknown}" >&2
      return 1
    fi
    health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container" 2>/dev/null || true)"
    case "$health" in
      healthy|none) return 0 ;;
      unhealthy)
        printf 'MySQL container healthcheck failed\n' >&2
        return 1
        ;;
    esac
    sleep 1
  done
  printf 'MySQL container did not become healthy in time\n' >&2
  return 1
}

mysql_container="$(find_mysql_container)"
if [[ -z "$mysql_container" ]]; then
  for name in MYSQL_PASSWORD MYSQL_ROOT_PASSWORD; do
    if [[ -z "${!name:-}" ]]; then
      printf '%s must be set when the MySQL container does not exist\n' "$name" >&2
      exit 2
    fi
  done
  docker compose "${compose_args[@]}" up -d --wait mysql
  mysql_container="$(find_mysql_container)"
fi
[[ -n "$mysql_container" ]] || { printf 'could not find the TTP MySQL container\n' >&2; exit 1; }
if [[ "$(docker inspect --format '{{.State.Status}}' "$mysql_container")" != "running" ]]; then
  docker start "$mysql_container" >/dev/null
fi
wait_for_mysql "$mysql_container"

docker exec -i "$mysql_container" sh -c \
  'export MYSQL_PWD="$MYSQL_PASSWORD"; exec mysql --protocol=socket -u"$MYSQL_USER" "$MYSQL_DATABASE"' <<'SQL'
SET FOREIGN_KEY_CHECKS = 0;
TRUNCATE TABLE release_execution_logs;
TRUNCATE TABLE release_runtime_targets;
TRUNCATE TABLE release_commits;
TRUNCATE TABLE project_releases;
TRUNCATE TABLE ab_experiments;
TRUNCATE TABLE project_deployment_configs;
TRUNCATE TABLE project_deployment_targets;
TRUNCATE TABLE audit_logs;
TRUNCATE TABLE project_git_credentials;
TRUNCATE TABLE projects;
TRUNCATE TABLE clusters;
TRUNCATE TABLE space_members;
TRUNCATE TABLE spaces;
TRUNCATE TABLE users;
SET FOREIGN_KEY_CHECKS = 1;
SQL

printf 'Local TTP business data was cleared. MySQL schema and volume were preserved.\n'
printf 'Start the backend to initialize the single default administrator.\n'
