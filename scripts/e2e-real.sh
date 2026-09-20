#!/usr/bin/env bash
set -Eeuo pipefail

# Runs the TTP lifecycle against the live local API, GitHub, MySQL and the
# configured Kubernetes cluster. The temporary space is removed on exit.

base_url="${CICD_BASE_URL:-http://127.0.0.1:8790}"
repo_url="${CICD_E2E_REPOSITORY_URL:-https://github.com/EthanScriptOn/control_server.git}"
login_user="${CICD_E2E_USERNAME:-admin}"
login_password="${CICD_E2E_PASSWORD:-ttp}"
cluster_id="${CICD_E2E_CLUSTER_ID:-${CICD_KUBE_CLUSTER_ID:-local}}"
kubeconfig_path="${CICD_E2E_KUBECONFIG:-${CICD_KUBECONFIG:-$HOME/.kube/config}}"
kube_context="${CICD_E2E_KUBE_CONTEXT:-${CICD_KUBE_CONTEXT:-}}"
compose_project="${COMPOSE_PROJECT_NAME:-cicd-platform}"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/ttp-e2e-real.XXXXXX")"

token=""
git_token=""
credential_blob=""
space_id=""
project_id=""
release_id=""
cleanup_done=0
last_status=""
last_body=""

cleanup_test_data() {
  local exit_code=$?
  if [[ "$cleanup_done" == "0" && "$space_id" =~ ^[A-Za-z0-9_-]{1,64}$ ]]; then
    local mysql_container
    mysql_container="$(docker ps --filter "label=com.docker.compose.project=${compose_project}" --filter 'label=com.docker.compose.service=mysql' --format '{{.ID}}' | head -n 1 || true)"
    if [[ -n "$mysql_container" ]]; then
      docker exec -i "$mysql_container" sh -c 'export MYSQL_PWD="$MYSQL_PASSWORD"; exec mysql --protocol=socket -u"$MYSQL_USER" "$MYSQL_DATABASE"' <<SQL >/dev/null
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
      cleanup_done=1
    else
      printf 'cleanup failed: MySQL container is unavailable\n' >&2
      exit_code=1
    fi
  fi
  find "$tmp_dir" -mindepth 1 -maxdepth 1 -type f -delete 2>/dev/null || true
  rmdir "$tmp_dir" 2>/dev/null || true
  unset token git_token credential_blob
  exit "$exit_code"
}
trap cleanup_test_data EXIT

api_call() {
  local method="$1"
  local path="$2"
  local payload="${3:-}"
  local response_file="$tmp_dir/response"
  local -a args=(--silent --show-error --max-time "${CICD_E2E_TIMEOUT_SECONDS:-30}" -X "$method" -H 'Accept: application/json')
  if [[ -n "$token" ]]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  if [[ -n "$payload" ]]; then
    args+=(-H 'Content-Type: application/json' --data-binary @-)
    last_status="$(printf '%s' "$payload" | curl "${args[@]}" -o "$response_file" -w '%{http_code}' "${base_url%/}$path")"
  else
    last_status="$(curl "${args[@]}" -o "$response_file" -w '%{http_code}' "${base_url%/}$path")"
  fi
  last_body="$(<"$response_file")"
}

safe_error() {
	if printf '%s' "$last_body" | jq -e 'type == "object" and (.error | type == "object")' >/dev/null 2>&1; then
		printf '%s' "$last_body" | jq -c '{code: (.error.code // "unknown")}'
	else
		printf '%s' '{"code":"non_json_response"}'
	fi
}

expect_status() {
  local expected="$1"
  local label="$2"
  if [[ "$last_status" != "$expected" ]]; then
    printf 'FAIL %-46s HTTP %s expected %s %s\n' "$label" "$last_status" "$expected" "$(safe_error)" >&2
    exit 1
  fi
  printf 'PASS %-46s HTTP %s\n' "$label" "$last_status"
}

expect_json() {
  local expression="$1"
  local label="$2"
  shift 2
  if ! jq -e "$@" "$expression" <<<"$last_body" >/dev/null; then
    printf 'FAIL %-46s %s\n' "$label" "$(safe_error)" >&2
    exit 1
  fi
  printf 'PASS %-46s\n' "$label"
}

response_items_filter='if type == "array" then . else (.items // []) end'

printf 'TTP real end-to-end test: %s\n' "${base_url%/}"

api_call GET /api/health
expect_status 200 'health endpoint'
expect_json '.status == "ok" and (.demo | not)' 'demo runtime is disabled'

api_call POST /api/auth/login "$(jq -nc --arg u "$login_user" --arg p "$login_password" '{username:$u,password:$p}')"
expect_status 200 'administrator login'
token="$(jq -r '.token' <<<"$last_body")"
[[ -n "$token" && "$token" != "null" ]] || { printf 'FAIL login token is empty\n' >&2; exit 1; }
space_id="$(jq -r '.current_space_id // ""' <<<"$last_body")"
[[ -z "$space_id" ]] || { printf 'FAIL clean database unexpectedly selected a space\n' >&2; exit 1; }
printf 'PASS clean database starts without a selected space\n'

suffix="$(date +%s)-$$"
api_call POST /api/spaces "$(jq -nc --arg name "TTP E2E $suffix" --arg slug "ttp-e2e-$suffix" '{name:$name,slug:$slug,description:"temporary real integration test"}')"
expect_status 201 'create temporary space'
space_id="$(jq -r '.id' <<<"$last_body")"
[[ "$space_id" =~ ^[A-Za-z0-9_-]{1,64}$ ]] || { printf 'FAIL invalid temporary space id\n' >&2; exit 1; }

api_call POST /api/auth/select-space "$(jq -nc --arg id "$space_id" '{space_id:$id}')"
expect_status 200 'select temporary space'
token="$(jq -r '.token' <<<"$last_body")"

api_call POST /api/clusters "$(jq -nc --arg id "$cluster_id" --arg path "$kubeconfig_path" --arg context "$kube_context" '{id:$id,name:"E2E Kubernetes",provider:"kubernetes",connection_mode:"kubeconfig",kubeconfig_path:$path,kube_context:$context}')"
expect_status 201 'create cluster registration'
expect_json '.connected == true and (.connection.version | type == "string") and (.cluster | has("kubeconfig_path") | not)' 'connect to real Kubernetes'
printf '      Kubernetes version: %s\n' "$(jq -r '.connection.version' <<<"$last_body")"

api_call POST /api/projects "$(jq -nc --arg name "TTP E2E control_server" --arg url "$repo_url" --arg cluster "$cluster_id" '{name:$name,description:"temporary real integration test",repository_url:$url,default_branch:"main",cluster_id:$cluster,namespace:"dev",deploy_strategy:"rolling",replicas:1,container_port:80}')"
expect_status 201 'create project from real repository'
project_id="$(jq -r '.id' <<<"$last_body")"
[[ "$project_id" =~ ^[A-Za-z0-9_-]{1,64}$ ]] || { printf 'FAIL invalid temporary project id\n' >&2; exit 1; }

if [[ -n "${CICD_E2E_GIT_TOKEN:-}" ]]; then
  git_token="$CICD_E2E_GIT_TOKEN"
  git_user="${CICD_E2E_GIT_USERNAME:-EthanScriptOn}"
else
  credential_blob="$(printf 'protocol=https\nhost=github.com\n\n' | git credential fill 2>/dev/null || true)"
  git_token="$(printf '%s\n' "$credential_blob" | awk -F= '$1=="password"{sub(/^password=/,""); print; exit}')"
  git_user="$(printf '%s\n' "$credential_blob" | awk -F= '$1=="username"{sub(/^username=/,""); print; exit}')"
fi
[[ -n "$git_token" && -n "$git_user" ]] || { printf 'FAIL no GitHub credential is available\n' >&2; exit 1; }

api_call PUT "/api/projects/$project_id/git/credential" "$(jq -nc --arg provider github --arg user "$git_user" --arg secret "$git_token" '{provider:$provider,username:$user,token:$secret}')"
expect_status 200 'save and validate Git credential'
expect_json '.access.usable == true and .access.authenticated == true and .access.account_matches == true and .access.repository_found == true and .access.can_write == true and (.credential | has("token") | not)' 'Git credential is valid and redacted'
printf '      Git account: %s; can_write: %s\n' "$(jq -r '.access.account' <<<"$last_body")" "$(jq -r '.access.can_write' <<<"$last_body")"

api_call GET "/api/projects/$project_id/git/credential"
expect_status 200 'read Git credential metadata'
expect_json '.credential.configured == true and (.credential | has("token") | not)' 'stored credential stays redacted'

api_call GET "/api/projects/$project_id/git/access"
expect_status 200 'recheck Git access'
expect_json '.access.usable == true and .access.repository_found == true and .access.can_write == true' 'TTP can access the private repository'

api_call GET "/api/projects/$project_id/git/branches"
expect_status 200 'list real branches'
expect_json "($response_items_filter | length > 0 and any(.[]; .name == \"main\"))" 'main branch is available'

api_call GET "/api/projects/$project_id/git/commits?branch=main&limit=5"
expect_status 200 'list real commits'
expect_json "($response_items_filter | length > 0 and (.[0].sha | length >= 7))" 'main has a commit HEAD'
printf '      main HEAD: %s\n' "$(jq -r "$response_items_filter | .[0].short_sha" <<<"$last_body")"

api_call GET "/api/projects/$project_id/git/tags?limit=20"
expect_status 200 'list real tags'
expect_json '.supported == true and (.items | type == "array")' 'tag response is not mock data'

api_call GET "/api/projects/$project_id/deployment-targets"
expect_status 200 'read default DEV environment'
dev_target_id="$(jq -r '.items[] | select(.stage == "dev") | .id' <<<"$last_body" | head -1)"
[[ -n "$dev_target_id" ]] || { printf 'FAIL default DEV environment is missing\n' >&2; exit 1; }

api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster_id "$cluster_id" '{name:"UAT",environment:"uat",stage:"uat",sort_order:2,cluster_id:$cluster_id,namespace:"uat",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 201 'create UAT environment'
api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster_id "$cluster_id" '{name:"PRE",environment:"pre",stage:"pre",sort_order:3,cluster_id:$cluster_id,namespace:"pre",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 201 'create PRE environment'
api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster_id "$cluster_id" '{name:"PROD",environment:"prod",stage:"prod",sort_order:4,cluster_id:$cluster_id,namespace:"pro",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 201 'create PROD environment'
api_call GET "/api/projects/$project_id/deployment-targets"
expect_status 200 'list all environments'
expect_json '[.items[].stage] | join(",") == "dev,uat,pre,prod"' 'environment order is explicit'

manifest='apiVersion: apps/v1
kind: Deployment
metadata:
  name: ttp-e2e-control-server
  namespace: dev
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ttp-e2e-control-server
  template:
    metadata:
      labels:
        app: ttp-e2e-control-server
    spec:
      containers:
        - name: app
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: ttp-e2e-control-server
  namespace: dev
spec:
  selector:
    app: ttp-e2e-control-server
  ports:
    - port: 80
      targetPort: 80'
api_call PUT "/api/projects/$project_id/deployment-config" "$(jq -nc --arg manifest "$manifest" '{manifest:$manifest,format:"yaml"}')"
expect_status 200 'save Kubernetes manifest'
expect_json '.config.version == 1 and .config.resource_count == 2' 'manifest is persisted and summarized'

api_call POST "/api/projects/$project_id/deployment-config/validate" "$(jq -nc --arg manifest "$manifest" '{manifest:$manifest,format:"yaml"}')"
expect_status 200 'validate Kubernetes manifest'
expect_json '.config.resource_count == 2 and (.config.capabilities.supported_kinds | length > 0)' 'manifest validation is real'

api_call GET "/api/projects/$project_id/pods?target_id=$dev_target_id"
expect_status 200 'read project Pods from Kubernetes'
expect_json '.total == 0 and (.items | length) == 0' 'new project has no fabricated Pods'

api_call GET "/api/projects/$project_id/metrics?target_id=$dev_target_id"
expect_status 200 'read project metrics'
expect_json '.metrics_available == false and .pod_count == 0' 'metrics reports the current provider limitation'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg target "$dev_target_id" '{branch:"main",target_ids:[$target],strategy:"rolling",name:"TTP E2E release"}')"
expect_status 201 'create release from selected branch'
release_id="$(jq -r '.release.id' <<<"$last_body")"
expect_json '.release.branch == "main" and (.release.commits | length == 1) and .release.targets[0].id == $target' 'release snapshots branch, commit and environment' --arg target "$dev_target_id"

api_call GET "/api/projects/$project_id/releases/$release_id"
expect_status 200 'read persisted release detail'
expect_json '.release.status == "draft"' 'new release starts as draft'

api_call POST "/api/projects/$project_id/releases/$release_id/publish"
expect_status 202 'start publish workflow'
expect_json '.release.status == "running" or .release.status == "queued"' 'publish enters execution state'

terminal_status=""
for _ in {1..180}; do
  sleep 1
  api_call GET "/api/projects/$project_id/releases/$release_id"
  expect_status 200 'poll publish workflow'
  terminal_status="$(jq -r '.release.status' <<<"$last_body")"
  if [[ "$terminal_status" == "succeeded" || "$terminal_status" == "failed" || "$terminal_status" == "cancelled" ]]; then
    break
  fi
done
printf '      release terminal status: %s\n' "$terminal_status"
if [[ "$terminal_status" != "succeeded" && "$terminal_status" != "failed" ]]; then
  printf 'FAIL release did not reach a terminal status within the polling window\n' >&2
  exit 1
fi
if [[ "$terminal_status" == "failed" ]]; then
  expect_json '.release.error | type == "string" and length > 0' 'failed release exposes a concrete error'
else
  expect_json '.release.artifact.digest | type == "string" and startswith("sha256:")' 'successful release stores an immutable image digest'
fi

api_call GET "/api/projects/$project_id/releases/$release_id/targets/$dev_target_id/logs"
expect_status 200 'read persisted target logs'
expect_json '.total > 0 and any(.items[]; .source == "git") and any(.items[]; .source == "ttp")' 'raw Git and TTP logs are retained'
printf '      log sources: %s\n' "$(jq -r '[.items[].source] | unique | join(",")' <<<"$last_body")"

api_call POST "/api/projects/$project_id/releases/$release_id/cancel" '{"message":"terminal state check"}'
expect_status 409 'terminal release cannot be cancelled'

printf 'E2E reached the real Git/Builder/Kubernetes release boundary.\n'
