#!/usr/bin/env bash
set -Eeuo pipefail

# This is a black-box check against a running TTP API. It never supplies
# fabricated responses to the application and never prints credentials.
root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
base_url="${CICD_BASE_URL:-http://127.0.0.1:8790}"
username="${CICD_REGRESSION_USERNAME:-admin}"
password="${CICD_REGRESSION_PASSWORD:-ttp}"
run_mutating="${RUN_MUTATING_API_CHECK:-NO}"
cleanup="${CLEANUP_MUTATING_API_CHECK:-YES}"
expect_empty="${EXPECT_EMPTY_DB:-YES}"
repository_url="${CICD_REGRESSION_REPOSITORY_URL:-https://github.com/octocat/Hello-World.git}"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/ttp-api-regression.XXXXXX")"
last_status=""
last_body=""
token=""
space_id=""
cluster_id=""
project_id=""
release_id=""
target_id=""
cleanup_needed=0

cleanup() {
  local exit_code=$?
  if ((cleanup_needed == 1)) && [[ "$cleanup" == "YES" ]]; then
    if [[ -z "$space_id" || ! "$space_id" =~ ^[A-Za-z0-9_-]{1,64}$ ]]; then
      printf 'WARN  mutating API check could not identify its temporary space; cleanup was skipped\n' >&2
      exit_code=1
    else
      mysql_container="$(docker ps --filter "label=com.docker.compose.project=${COMPOSE_PROJECT_NAME:-cicd-platform}" --filter 'label=com.docker.compose.service=mysql' --format '{{.ID}}' | head -n 1)"
      if [[ -z "$mysql_container" ]]; then
        printf 'WARN  mutating API check created data but cleanup was skipped: MySQL container is not running\n' >&2
        exit_code=1
        rm -rf "$tmp_dir"
        exit "$exit_code"
      fi
      # Delete children explicitly before the space. Foreign-key checks are
      # kept enabled so this cannot leave orphaned regression rows behind.
      if ! docker exec -i "$mysql_container" sh -c \
        'export MYSQL_PWD="$MYSQL_PASSWORD"; exec mysql --protocol=socket -u"$MYSQL_USER" "$MYSQL_DATABASE"' <<SQL
DELETE FROM release_execution_logs WHERE space_id = '$space_id';
DELETE FROM release_runtime_targets WHERE space_id = '$space_id';
DELETE FROM release_commits WHERE space_id = '$space_id';
DELETE FROM project_releases WHERE space_id = '$space_id';
DELETE FROM ab_experiments WHERE space_id = '$space_id';
DELETE FROM project_deployment_configs WHERE space_id = '$space_id';
DELETE FROM project_git_credentials WHERE space_id = '$space_id';
DELETE FROM project_deployment_targets WHERE space_id = '$space_id';
DELETE FROM audit_logs WHERE space_id = '$space_id';
DELETE FROM projects WHERE space_id = '$space_id';
DELETE FROM clusters WHERE space_id = '$space_id';
DELETE FROM space_members WHERE space_id = '$space_id';
DELETE FROM spaces WHERE id = '$space_id';
SQL
      then
        printf 'WARN  mutating API check could not clean its temporary space\n' >&2
        exit_code=1
      fi
    fi
  fi
  rm -rf "$tmp_dir"
  exit "$exit_code"
}
trap cleanup EXIT

fail() {
  printf 'FAIL  %s\n' "$1" >&2
  exit 1
}

pass() { printf 'PASS  %s\n' "$1"; }

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

api_call() {
  local method="$1"
  local path="$2"
  local payload="${3:-}"
  local output="$tmp_dir/response"
  local -a args=(--silent --show-error --max-time "${CICD_API_TIMEOUT_SECONDS:-20}" -X "$method" -H 'Accept: application/json')
  if [[ -n "$token" ]]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  if [[ -n "$payload" ]]; then
    args+=(-H 'Content-Type: application/json' --data "$payload")
  fi
  last_status="$(curl "${args[@]}" -o "$output" -w '%{http_code}' "${base_url%/}$path")" || {
    fail "$method $path could not reach $base_url"
  }
	last_body="$(<"$output")"
}

safe_error() {
	if jq -e 'type == "object" and (.error | type == "object")' >/dev/null 2>&1 <<<"$last_body"; then
		jq -c '{code: (.error.code // "unknown")}' <<<"$last_body"
	else
		printf '%s\n' '{"code":"non_json_response"}'
	fi
}

expect_status() {
	local expected="$1"
	local label="$2"
	[[ "$last_status" == "$expected" ]] || fail "$label: expected HTTP $expected, got $last_status: $(safe_error)"
	pass "$label (HTTP $last_status)"
}

expect_status_any() {
  local label="$1"
  shift
  local expected
  for expected in "$@"; do
    [[ "$last_status" == "$expected" ]] && { pass "$label (HTTP $last_status)"; return; }
  done
	fail "$label: unexpected HTTP $last_status: $(safe_error)"
}

expect_json() {
  local expression="$1"
  local label="$2"
  shift 2
	jq -e "$@" "$expression" >/dev/null <<<"$last_body" || fail "$label: JSON assertion failed: $(safe_error)"
  pass "$label"
}

require_command curl
require_command jq
if [[ "$run_mutating" == "YES" ]]; then
  require_command docker
fi
[[ -n "$username" ]] || fail 'CICD_REGRESSION_USERNAME is required'
[[ -n "$password" ]] || fail 'CICD_REGRESSION_PASSWORD is required'

printf 'TTP API regression target: %s\n' "${base_url%/}"

api_call GET /api/health
expect_status 200 'health endpoint'
expect_json '.status == "ok" and (.demo | not)' 'health reports a real runtime without demo flag'

api_call GET /api/projects
expect_status 401 'protected route rejects anonymous access'

api_call POST /api/auth/login "$(jq -nc --arg username "$username" --arg password "intentionally-wrong" '{username:$username,password:$password}')"
expect_status 401 'invalid credentials are rejected'

api_call POST /api/auth/login "$(jq -nc --arg username "$username" --arg password "$password" '{username:$username,password:$password}')"
expect_status 200 'default administrator login'
expect_json '.token | type == "string" and length > 20' 'login returns a session token'
token="$(jq -r '.token' <<<"$last_body")"
space_id="$(jq -r '.current_space_id // ""' <<<"$last_body")"

api_call GET /api/auth/me
expect_status 200 'current user endpoint'
expect_json '.user.username == $username and (.spaces | type == "array")' 'current user response is well formed' --arg username "$username"

api_call GET /api/spaces
expect_status 200 'space list endpoint'
if [[ "$expect_empty" == "YES" ]]; then
  expect_json '.total == 0 and (.items | length) == 0' 'clean database has no spaces'
fi

api_call GET /api/projects
if [[ -z "$space_id" ]]; then
  expect_status 403 'resource access without a selected space'
  expect_json '.error.code == "space_required"' 'missing space has a stable error code'
else
  expect_status 200 'resource access with the current space'
fi

if [[ "$run_mutating" != "YES" ]]; then
  pass 'read-only API regression completed (set RUN_MUTATING_API_CHECK=YES for lifecycle checks)'
  exit 0
fi

cleanup_needed=1
suffix="$(date +%s)-$$"

api_call POST /api/spaces "$(jq -nc --arg name "回归空间 $suffix" --arg slug "regression-$suffix" '{name:$name,slug:$slug,description:"API regression workspace"}')"
expect_status 201 'create temporary space'
space_id="$(jq -r '.id' <<<"$last_body")"
[[ -n "$space_id" && "$space_id" != "null" ]] || fail 'created space has no ID'

api_call POST /api/auth/select-space "$(jq -nc --arg space_id "$space_id" '{space_id:$space_id}')"
expect_status 200 'select temporary space'
token="$(jq -r '.token' <<<"$last_body")"
[[ -n "$token" && "$token" != "null" ]] || fail 'selected space did not return a token'

cluster_id="regression-$suffix"
api_call POST /api/clusters "$(jq -nc --arg id "$cluster_id" --arg name "回归集群 $suffix" '{id:$id,name:$name,provider:"kubernetes",connection_mode:"kubeconfig"}')"
expect_status 201 'register temporary cluster'
expect_json '.cluster.id == $cluster_id and (.cluster | has("kubeconfig_path") | not)' 'cluster response does not expose kubeconfig' --arg cluster_id "$cluster_id"

api_call GET /api/clusters
expect_status 200 'list clusters in current space'
expect_json '.total == 1 and .items[0].id == $cluster_id' 'cluster is space scoped' --arg cluster_id "$cluster_id"

project_name="regression-$suffix"
api_call POST /api/projects "$(jq -nc --arg name "$project_name" --arg url "$repository_url" --arg cluster_id "$cluster_id" '{name:$name,description:"API regression project",repository_id:"client-supplied-id-must-be-ignored",repository_url:$url,default_branch:"main",cluster_id:$cluster_id,namespace:"regression",deploy_strategy:"rolling",replicas:1,container_port:8080}')"
expect_status 201 'create temporary project'
project_id="$(jq -r '.id' <<<"$last_body")"
[[ -n "$project_id" && "$project_id" != "null" ]] || fail 'created project has no ID'
expect_json '.repository_id != "client-supplied-id-must-be-ignored"' 'repository ID is derived by the server'

api_call GET "/api/projects/$project_id/git/branches"
expect_status_any 'read real repository branches' 200 404 502 504
if [[ "$last_status" == 200 ]]; then
  branch="$(jq -r '.items[0].name // "main"' <<<"$last_body")"
  [[ -n "$branch" && "$branch" != "null" ]] || fail 'branch response is empty'
  pass 'branch response contains a usable branch name'
else
  if [[ "$last_status" == 404 ]]; then
    printf 'WARN  remote Git repository was not found or is not authorized for this project; lifecycle release checks are skipped\n'
  else
    printf 'WARN  remote Git repository was unavailable; lifecycle release checks are skipped\n'
  fi
  exit 0
fi

api_call GET "/api/projects/$project_id/deployment-config"
expect_status 200 'empty deployment config endpoint'
expect_json '.config.manifest == ""' 'new project has no implicit deployment manifest'

invalid_manifest='{"manifest":"kind: Deployment\nmetadata: [","format":"yaml"}'
api_call PUT "/api/projects/$project_id/deployment-config" "$invalid_manifest"
expect_status 400 'invalid deployment manifest is rejected'

manifest="$(cat <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: regression-app
  namespace: regression
spec:
  replicas: 1
---
apiVersion: v1
kind: Service
metadata:
  name: regression-app
  namespace: regression
EOF
)"
api_call PUT "/api/projects/$project_id/deployment-config" "$(jq -nc --arg manifest "$manifest" '{manifest:$manifest,format:"yaml"}')"
expect_status 200 'valid deployment manifest is saved'
expect_json '.config.version == 1 and .config.resource_count == 2' 'saved manifest has a version and resource summary'

api_call GET "/api/projects/$project_id/deployment-targets"
expect_status 200 'project has an explicit development target'
target_id="$(jq -r '.items[0].id // ""' <<<"$last_body")"
[[ -n "$target_id" ]] || fail 'project has no deployment target'

api_call GET "/api/projects/$project_id/pods?target_id=$target_id"
expect_status_any 'read real Kubernetes Pod state' 200 404 502 503 504

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg target_id "$target_id" '{branch:$branch,target_ids:[$target_id],strategy:"invalid"}')"
expect_status 400 'invalid release strategy is rejected'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg target_id "$target_id" '{branch:$branch,target_ids:[$target_id],strategy:"rolling"}')"
expect_status_any 'create release from the selected branch' 201 502 504
if [[ "$last_status" == 201 ]]; then
  release_id="$(jq -r '.release.id // ""' <<<"$last_body")"
  [[ -n "$release_id" ]] || fail 'created release has no ID'
  api_call GET "/api/projects/$project_id/releases/$release_id"
  expect_status 200 'read release details'
  expect_json '.release.branch == $branch and (.release.targets | length) == 1' 'release keeps branch and target snapshots' --arg branch "$branch"

  api_call GET "/api/projects/$project_id/releases/$release_id/targets/$target_id/logs"
  expect_status 200 'read release execution logs'
  expect_json '.items | type == "array"' 'release logs are returned as a list'
fi

pass 'mutating API regression completed; cleanup will restore the empty database'
