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

command -v docker >/dev/null 2>&1 || { printf 'docker is required\n' >&2; exit 127; }

if ! docker_info_ready; then
  printf 'Docker daemon is unavailable; start Docker Desktop before checking the database.\n' >&2
  exit 1
fi

mysql_container="$(docker ps -q \
  --filter "label=com.docker.compose.project=$project_name" \
  --filter 'label=com.docker.compose.service=mysql' \
  | head -n 1)"
if [[ -z "$mysql_container" ]]; then
  printf 'TTP MySQL container is not running; start it before checking the database\n' >&2
  exit 1
fi

for _ in {1..30}; do
  state="$(docker inspect --format '{{.State.Status}}' "$mysql_container" 2>/dev/null || true)"
  health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$mysql_container" 2>/dev/null || true)"
  if [[ "$state" != "running" ]]; then
    printf 'TTP MySQL container is not running (state: %s)\n' "${state:-unknown}" >&2
    exit 1
  fi
  if [[ "$health" == "healthy" || "$health" == "none" ]]; then
    break
  fi
  if [[ "$health" == "unhealthy" ]]; then
    printf 'TTP MySQL container healthcheck failed\n' >&2
    exit 1
  fi
  sleep 1
done

[[ "$health" == "healthy" || "$health" == "none" ]] || { printf 'TTP MySQL container did not become healthy in time\n' >&2; exit 1; }

counts="$(docker exec -i "$mysql_container" sh -c \
  'export MYSQL_PWD="$MYSQL_PASSWORD"; exec mysql --protocol=socket -u"$MYSQL_USER" --batch --skip-column-names "$MYSQL_DATABASE"' <<'SQL'
SELECT
  (SELECT COUNT(*) FROM users),
  (SELECT COUNT(*) FROM spaces),
  (SELECT COUNT(*) FROM space_members),
  (SELECT COUNT(*) FROM clusters),
  (SELECT COUNT(*) FROM image_registry_connections),
  (SELECT COUNT(*) FROM projects),
  (SELECT COUNT(*) FROM project_deployment_targets),
  (SELECT COUNT(*) FROM project_deployment_resource_files),
  (SELECT COUNT(*) FROM project_deployment_configs),
  (SELECT COUNT(*) FROM deployment_namespace_quotas),
  (SELECT COUNT(*) FROM project_git_credentials),
  (SELECT COUNT(*) FROM project_releases),
  (SELECT COUNT(*) FROM release_commits),
  (SELECT COUNT(*) FROM release_runtime_targets),
  (SELECT COUNT(*) FROM release_execution_logs),
  (SELECT COUNT(*) FROM ab_experiments),
  (SELECT COUNT(*) FROM audit_logs),
  (SELECT COUNT(*) FROM information_schema.tables
    WHERE table_schema = DATABASE() AND table_name = 'project_build_configs');
SQL
)"
read -r users spaces members clusters registry_connections projects targets resource_files configs namespace_quotas credentials releases commits runtime_targets execution_logs experiments audit_logs legacy_build_configs <<< "$counts"

if [[ "$users" != 1 || "$spaces" != 0 || "$members" != 0 || "$clusters" != 0 || "$registry_connections" != 0 || "$projects" != 0 || "$targets" != 0 || "$resource_files" != 0 || "$configs" != 0 || "$namespace_quotas" != 0 || "$credentials" != 0 || "$releases" != 0 || "$commits" != 0 || "$runtime_targets" != 0 || "$execution_logs" != 0 || "$experiments" != 0 || "$audit_logs" != 0 || "$legacy_build_configs" != 0 ]]; then
  printf 'Unexpected local TTP database counts:\n'
  printf 'users=%s spaces=%s members=%s clusters=%s registry_connections=%s projects=%s targets=%s resource_files=%s configs=%s namespace_quotas=%s credentials=%s releases=%s commits=%s runtime_targets=%s execution_logs=%s experiments=%s audit_logs=%s legacy_build_configs=%s\n' \
	    "$users" "$spaces" "$members" "$clusters" "$registry_connections" "$projects" "$targets" "$resource_files" "$configs" "$namespace_quotas" "$credentials" "$releases" "$commits" "$runtime_targets" "$execution_logs" "$experiments" "$audit_logs" "$legacy_build_configs"
  exit 1
fi

printf 'Local TTP database check passed: one default administrator and all business data tables are empty.\n'
