#!/usr/bin/env bash
set -Eeuo pipefail

# Black-box boundary checks for the running, non-demo TTP API. The script only
# creates resources inside its own temporary spaces and removes those rows on
# exit. It never prints response bodies or credentials.

base_url="${CICD_BASE_URL:-http://127.0.0.1:8790}"
username="${CICD_REGRESSION_USERNAME:-admin}"
password="${CICD_REGRESSION_PASSWORD:-ttp}"
repository_url="${CICD_EDGE_REPOSITORY_URL:-https://github.com/octocat/Hello-World.git}"
kubeconfig_path="${CICD_EDGE_KUBECONFIG:-${CICD_KUBECONFIG:-$HOME/.kube/config}}"
kube_context="${CICD_EDGE_KUBE_CONTEXT:-${CICD_KUBE_CONTEXT:-}}"
requested_branch="${CICD_EDGE_BRANCH:-master}"
cluster_id="${CICD_KUBE_CLUSTER_ID:-local}"
compose_project="${COMPOSE_PROJECT_NAME:-cicd-platform}"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/ttp-api-edge.XXXXXX")"

token=""
admin_token=""
viewer_token=""
developer_token=""
space_id=""
second_space_id=""
cluster_id_for_cleanup=""
project_id=""
dev_target_id=""
uat_target_id=""
pre_target_id=""
prod_target_id=""
release_id=""
published_release_id=""
edge_username=""
developer_username=""
last_status=""
last_body=""
cleanup_started=0

cleanup_test_data() {
  local exit_code=$?
  if ((cleanup_started == 1)); then
    local mysql_container
    mysql_container="$(docker ps \
      --filter "label=com.docker.compose.project=${compose_project}" \
      --filter 'label=com.docker.compose.service=mysql' \
      --format '{{.ID}}' | head -n 1 || true)"
    if [[ -z "$mysql_container" ]]; then
      printf 'WARN cleanup skipped: MySQL container is unavailable\n' >&2
      exit_code=1
    elif [[ "$space_id" =~ ^[A-Za-z0-9_-]{1,64}$ && "$edge_username" =~ ^[A-Za-z0-9_-]{1,100}$ && "$developer_username" =~ ^[A-Za-z0-9_-]{1,100}$ ]]; then
      if ! docker exec -i "$mysql_container" sh -c \
        'export MYSQL_PWD="$MYSQL_PASSWORD"; exec mysql --protocol=socket -u"$MYSQL_USER" "$MYSQL_DATABASE"' <<SQL >/dev/null 2>&1
DELETE FROM release_execution_logs WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM release_runtime_targets WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM release_commits WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM project_releases WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM ab_experiments WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM project_deployment_configs WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM project_git_credentials WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM project_deployment_targets WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM audit_logs WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM projects WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM image_registry_connections WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM clusters WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM space_members WHERE space_id IN ('$space_id', '$second_space_id');
DELETE FROM spaces WHERE id IN ('$space_id', '$second_space_id');
DELETE FROM users WHERE username IN ('$edge_username', '$developer_username');
SQL
      then
        printf 'WARN cleanup failed for the temporary spaces\n' >&2
        exit_code=1
      fi
    else
      printf 'WARN cleanup skipped: temporary identifiers were not complete\n' >&2
      exit_code=1
    fi
  fi
  rm -rf "$tmp_dir"
  unset token admin_token viewer_token developer_token
  exit "$exit_code"
}
trap cleanup_test_data EXIT

fail() {
  printf 'FAIL %s\n' "$1" >&2
  exit 1
}

pass() { printf 'PASS %s\n' "$1"; }
warn() { printf 'WARN %s\n' "$1"; }

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

api_call() {
  local method="$1"
  local path="$2"
  local payload="${3:-}"
  local response_file="$tmp_dir/response"
  local -a args=(--silent --show-error --max-time "${CICD_EDGE_TIMEOUT_SECONDS:-30}" -X "$method" -H 'Accept: application/json')
  if [[ -n "$token" ]]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  if [[ -n "$payload" ]]; then
    args+=(-H 'Content-Type: application/json' --data-binary @-)
    last_status="$(printf '%s' "$payload" | curl "${args[@]}" -o "$response_file" -w '%{http_code}' "${base_url%/}$path")" || fail "$method $path could not reach the API"
  else
    last_status="$(curl "${args[@]}" -o "$response_file" -w '%{http_code}' "${base_url%/}$path")" || fail "$method $path could not reach the API"
  fi
  last_body="$(<"$response_file")"
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
    if [[ "$last_status" == "$expected" ]]; then
      pass "$label (HTTP $last_status)"
      return
    fi
  done
  fail "$label: unexpected HTTP $last_status: $(safe_error)"
}

expect_json() {
  local expression="$1"
  local label="$2"
  shift 2
  jq -e "$@" "$expression" <<<"$last_body" >/dev/null || fail "$label: JSON assertion failed: $(safe_error)"
  pass "$label"
}

json_value() { jq -r "$1" <<<"$last_body"; }

expect_redacted() {
  local label="$1"
  local secret="${2:-}"
  [[ "$last_body" != *'"password"'* && "$last_body" != *'"password_hash"'* && "$last_body" != *'"token_ciphertext"'* ]] || fail "$label: sensitive field was returned"
  if [[ -n "$secret" && "$last_body" == *"$secret"* ]]; then
    fail "$label: credential value was returned"
  fi
  pass "$label"
}

require_command curl
require_command jq
require_command docker
require_command git

printf 'TTP API edge regression target: %s\n' "${base_url%/}"

api_call GET /api/health
expect_status 200 'health endpoint'
expect_json '.status == "ok" and (.demo | not)' 'production runtime has no demo flag'

api_call GET /api/projects
expect_status 401 'anonymous request is rejected'

token="not-a-valid-session"
api_call GET /api/projects
expect_status 401 'malformed bearer token is rejected'
token=""

api_call POST /api/auth/login "$(jq -nc --arg u "$username" '{username:$u,password:"wrong-password"}')"
expect_status 401 'wrong password is rejected'

api_call POST /api/auth/login "$(jq -nc --arg u "$username" --arg p "$password" '{username:$u,password:$p}')"
expect_status 200 'default administrator login'
expect_json '.user.username == $u and .current_space_id == "" and (.spaces | length == 0)' 'empty database has no selected space' --arg u "$username"
expect_redacted 'login response has no password fields'
token="$(json_value '.token')"
[[ -n "$token" && "$token" != "null" ]] || fail 'login did not return a session token'

api_call GET /api/auth/me
expect_status 200 'current user endpoint'
expect_json '.user.username == $u and .current_space_id == ""' 'current user has no selected space' --arg u "$username"
expect_redacted 'current user response is redacted'

api_call GET /api/spaces
expect_status 200 'empty space list endpoint'
expect_json '.total == 0 and (.items | length) == 0' 'empty database has no spaces'

api_call GET /api/projects
expect_status 403 'resource access requires a selected space'
expect_json '.error.code == "space_required"' 'missing space has a stable error code'

suffix="$(date +%s)-$$"
edge_username="ttp-edge-${suffix}"
viewer_password="ttp-viewer-${suffix}"
developer_password="ttp-developer-${suffix}"
developer_username="ttp-edge-developer-${suffix}"
space_slug="ttp-edge-${suffix}"

api_call POST /api/spaces "$(jq -nc --arg name "TTP edge $suffix" --arg slug "$space_slug" '{name:$name,slug:$slug,description:"temporary API boundary test"}')"
expect_status 201 'create primary temporary space'
space_id="$(json_value '.id')"
[[ "$space_id" =~ ^[A-Za-z0-9_-]{1,64}$ ]] || fail 'primary temporary space has an invalid ID'
cleanup_started=1

api_call POST /api/spaces "$(jq -nc --arg name "Duplicate edge $suffix" --arg slug "$space_slug" '{name:$name,slug:$slug}')"
expect_status 409 'duplicate space slug is rejected'

api_call POST /api/spaces "$(jq -nc --arg name "Second edge $suffix" --arg slug "${space_slug}-second" '{name:$name,slug:$slug}')"
expect_status 201 'create second temporary space'
second_space_id="$(json_value '.id')"
[[ "$second_space_id" =~ ^[A-Za-z0-9_-]{1,64}$ ]] || fail 'second temporary space has an invalid ID'

api_call POST /api/auth/select-space "$(jq -nc --arg id "$space_id" '{space_id:$id}')"
expect_status 200 'select primary temporary space'
admin_token="$(json_value '.token')"
token="$admin_token"

api_call POST /api/auth/select-space '{"space_id":"space-that-does-not-exist"}'
expect_status 404 'unknown space cannot be selected'

api_call GET /api/spaces
expect_status 200 'selected administrator lists spaces'
expect_json '.total == 2 and any(.items[]; .id == $id)' 'administrator sees both owned spaces' --arg id "$second_space_id"

api_call GET /api/space/settings
expect_status 200 'space settings endpoint'
expect_json '.space.id == $id and .role == "admin"' 'administrator role is explicit for the selected space' --arg id "$space_id"

api_call GET /api/space/permissions
expect_status 200 'space permission catalog endpoint'
expect_json 'any(.roles[]; .key == "viewer") and any(.permissions[]; .key == "release:publish")' 'permission catalog contains release controls'

cluster_id_for_cleanup="$cluster_id"
api_call POST /api/clusters "$(jq -nc --arg id "$cluster_id" --arg name "TTP edge cluster $suffix" --arg path "$kubeconfig_path" --arg context "$kube_context" '{id:$id,name:$name,provider:"kubernetes",connection_mode:"kubeconfig",kubeconfig_path:$path,kube_context:$context}')"
expect_status 201 'register temporary Kubernetes cluster'
expect_json '.cluster.id == $id and (.cluster | has("kubeconfig_path") | not)' 'cluster connection material is private' --arg id "$cluster_id"
expect_json '.connected == true' 'temporary cluster connects to the configured Kubernetes client'

api_call POST /api/clusters "$(jq -nc --arg id "$cluster_id" --arg name "Duplicate cluster $suffix" '{id:$id,name:$name,provider:"kubernetes",connection_mode:"kubeconfig"}')"
expect_status 409 'duplicate cluster ID is rejected'

api_call PATCH "/api/clusters/$cluster_id" '{"status":"invalid"}'
expect_status 400 'invalid cluster status is rejected'

api_call PATCH "/api/clusters/$cluster_id" "$(jq -nc --arg name "TTP edge cluster updated $suffix" '{name:$name}')"
expect_status 200 'cluster name update is persisted'

api_call POST "/api/clusters/$cluster_id/test"
expect_status 200 'cluster connection test endpoint'

api_call GET /api/clusters
expect_status 200 'list clusters in the selected space'
expect_json '.total == 1 and .items[0].id == $id' 'cluster list is space scoped' --arg id "$cluster_id"

project_name="TTP edge project $suffix"
api_call POST /api/projects "$(jq -nc --arg name "$project_name" --arg url "$repository_url" --arg cluster "$cluster_id" --arg branch "$requested_branch" '{name:$name,description:"temporary API boundary test",repository_id:"client-value-must-be-ignored",repository_url:$url,default_branch:$branch,cluster_id:$cluster,namespace:"edge-base",deploy_strategy:"rolling",replicas:1,container_port:8080}')"
expect_status 201 'create temporary project'
project_id="$(json_value '.id')"
[[ "$project_id" =~ ^[A-Za-z0-9_-]{1,64}$ ]] || fail 'temporary project has an invalid ID'
expect_json '.repository_id != "client-value-must-be-ignored"' 'repository ID is server-owned'

api_call POST /api/projects "$(jq -nc --arg name "$project_name" --arg cluster "$cluster_id" '{name:$name,repository_url:"https://github.com/octocat/another-edge-repository",default_branch:"master",cluster_id:$cluster,namespace:"edge-base"}')"
expect_status 409 'duplicate project name is rejected'

api_call GET "/api/projects/$project_id"
expect_status 200 'read temporary project'
expect_json '.id == $id and .namespace == "edge-base"' 'project settings are readable' --arg id "$project_id"

invalid_project_cases=(
  '{"name":"bad-strategy","repository_url":"https://github.com/octocat/bad-strategy","cluster_id":"CLUSTER","deploy_strategy":"bogus"}'
  '{"name":"bad-namespace","repository_url":"https://github.com/octocat/bad-namespace","cluster_id":"CLUSTER","namespace":"Bad_Ns"}'
  '{"name":"bad-replicas","repository_url":"https://github.com/octocat/bad-replicas","cluster_id":"CLUSTER","replicas":101}'
  '{"name":"bad-port","repository_url":"https://github.com/octocat/bad-port","cluster_id":"CLUSTER","container_port":70000}'
  '{"name":"bad-branch","repository_url":"https://github.com/octocat/bad-branch","cluster_id":"CLUSTER","default_branch":"bad..branch"}'
  '{"name":"bad-url","repository_url":"ftp://github.com/octocat/bad-url","cluster_id":"CLUSTER"}'
)
for invalid_project in "${invalid_project_cases[@]}"; do
  payload="${invalid_project/CLUSTER/$cluster_id}"
  api_call POST /api/projects "$payload"
  expect_status 400 'invalid project input is rejected before persistence'
done

api_call PATCH "/api/projects/$project_id" '{"container_port":0}'
expect_status 400 'explicit zero project port is rejected'

api_call PATCH "/api/projects/$project_id" '{"replicas":101}'
expect_status 400 'project replica upper bound is enforced'

api_call PATCH "/api/projects/$project_id" "$(jq -nc --arg description "updated edge description" '{description:$description}')"
expect_status 200 'valid project update is persisted'

api_call GET "/api/projects/$project_id/git/credential"
expect_status 200 'unconfigured project credential endpoint'
expect_json '.credential.configured == false' 'project starts without a Git credential'
expect_redacted 'unconfigured credential response is safe'

api_call PUT "/api/projects/$project_id/git/credential" '{"provider":"github","username":"release-bot","token":""}'
expect_status 400 'empty Git credential is rejected'

# Use a locally configured Git credential when available. It is sent only to
# the local TTP API, retained only for this temporary project, and never shown.
git_token="${CICD_EDGE_GIT_TOKEN:-}"
git_username="${CICD_EDGE_GIT_USERNAME:-}"
if [[ -z "$git_token" && -z "$git_username" ]]; then
  credential_blob="$(GIT_TERMINAL_PROMPT=0 git credential fill <<EOF 2>/dev/null || true
protocol=https
host=github.com

EOF
)"
  git_token="$(printf '%s\n' "$credential_blob" | awk -F= '$1 == "password" {sub(/^password=/, ""); print; exit}')"
  git_username="$(printf '%s\n' "$credential_blob" | awk -F= '$1 == "username" {sub(/^username=/, ""); print; exit}')"
  unset credential_blob
fi
if [[ -n "$git_token" && -n "$git_username" ]]; then
  api_call PUT "/api/projects/$project_id/git/credential" "$(jq -nc --arg provider github --arg user "$git_username" --arg secret "$git_token" '{provider:$provider,username:$user,token:$secret}')"
  if [[ "$last_status" == 200 ]]; then
    expect_json '.access.usable == true and (.credential | has("token") | not)' 'valid project Git credential is stored redacted'
    expect_redacted 'saved credential response does not expose the token' "$git_token"
  else
    warn "configured Git credential was not usable ($(safe_error)); read-only Git checks continue"
  fi
else
  warn 'no local Git credential was available; write-gated publish checks will report the expected permission boundary'
fi
unset git_token

api_call GET "/api/projects/$project_id/git/access"
expect_status 200 'Git access report endpoint'
expect_redacted 'Git access report is redacted'

api_call GET "/api/projects/$project_id/git/branches"
expect_status 200 'list real Git branches'
expect_json '.items | type == "array" and length > 0' 'real branch response is non-empty'
branch="$(jq -r --arg requested "$requested_branch" '.items[] | select(.name == $requested) | .name' <<<"$last_body" | head -n 1)"
if [[ -z "$branch" ]]; then
  branch="$(json_value '.items[0].name')"
fi
[[ -n "$branch" && "$branch" != "null" ]] || fail 'Git branch response has no usable branch'
pass "selected Git branch is available"

api_call GET "/api/projects/$project_id/git/commits?branch=$(printf '%s' "$branch" | jq -sRr @uri)&limit=0"
expect_status 400 'commit limit lower bound is enforced'

api_call GET "/api/projects/$project_id/git/commits?branch=$(printf '%s' "$branch" | jq -sRr @uri)&limit=201"
expect_status 400 'commit limit upper bound is enforced'

api_call GET "/api/projects/$project_id/git/commits?branch=$(printf '%s' "$branch" | jq -sRr @uri)&limit=5"
expect_status 200 'list real Git commits'
expect_json '.items | type == "array" and length > 0 and (.[0].sha | length >= 7)' 'commit response contains a real HEAD'
sha_one="$(json_value '.items[0].sha')"
sha_two="$(jq -r '.items[1].sha // empty' <<<"$last_body")"
[[ -n "$sha_one" && "$sha_one" != "null" ]] || fail 'first Git commit is empty'
if [[ -z "$sha_two" || "$sha_two" == "$sha_one" ]]; then
  fail 'at least two Git commits are required for commit lifecycle checks'
fi

api_call GET "/api/projects/$project_id/git/commits?branch=branch-that-does-not-exist&limit=1"
expect_status 404 'unknown Git branch is rejected'

api_call GET "/api/projects/$project_id/git/tags?limit=0"
expect_status 400 'tag limit lower bound is enforced'

api_call GET "/api/projects/$project_id/git/tags?limit=501"
expect_status 400 'tag limit upper bound is enforced'

api_call GET "/api/projects/$project_id/git/tags?limit=10"
expect_status 200 'list real Git tags'
expect_json '.items | type == "array"' 'tag response is a real provider response'

api_call POST "/api/projects/$project_id/release-preparation" "$(jq -nc --arg source "$branch" --arg sha "$sha_one" '{source_branch:$source,selected_sha:$sha}')"
expect_status 200 'read-only release preparation endpoint'
expect_json '.preparation | type == "object"' 'release preparation returns an analysis object'

api_call POST "/api/projects/$project_id/release-preparation" "$(jq -nc --arg source "$branch" --arg sha "$sha_one" '{action:"merge",source_branch:$source,base_branch:$source,selected_sha:$sha}')"
expect_status 200 'ready merge preparation does not require a Git write'
expect_json '.preparation.status == "ready" and .preparation.can_publish == true and .preparation.requires_merge == false' 'ready merge preparation remains read-only'

api_call GET "/api/projects/$project_id/deployment-config"
expect_status 200 'new project deployment config endpoint'
expect_json '.config.manifest == "" and .config.version == 0' 'new project has no fabricated manifest'

api_call PUT "/api/projects/$project_id/deployment-config" '{"manifest":"","format":"yaml"}'
expect_status 400 'empty deployment manifest is rejected'

api_call PUT "/api/projects/$project_id/deployment-config" '{"manifest":"kind: Deployment","format":"toml"}'
expect_status 400 'unknown deployment format is rejected'

api_call POST "/api/projects/$project_id/deployment-config/validate" '{"manifest":"kind: [","format":"yaml"}'
expect_status 400 'malformed deployment YAML is rejected'

manifest="$(cat <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ttp-edge-app
  namespace: edge-base
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ttp-edge-app
  template:
    metadata:
      labels:
        app: ttp-edge-app
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
  name: ttp-edge-app
  namespace: edge-base
spec:
  selector:
    app: ttp-edge-app
  ports:
    - port: 80
      targetPort: 80
EOF
)"
api_call PUT "/api/projects/$project_id/deployment-config" "$(jq -nc --arg manifest "$manifest" '{manifest:$manifest,format:"yaml"}')"
expect_status 200 'valid deployment manifest is saved'
expect_json '.config.version == 1 and .config.resource_count == 2' 'saved manifest has two resources'

api_call PUT "/api/projects/$project_id/deployment-config" "$(jq -nc --arg manifest "$manifest" '{manifest:$manifest,format:"yaml"}')"
expect_status 200 'deployment manifest can be revised'
expect_json '.config.version == 2' 'manifest revisions increment the version'

api_call POST "/api/projects/$project_id/deployment-config/validate" "$(jq -nc --arg manifest "$manifest" '{manifest:$manifest,format:"yaml"}')"
expect_status 200 'valid manifest validation endpoint'
expect_json '.config.resource_count == 2' 'manifest validation counts resources'

api_call POST "/api/projects/$project_id/deployment-config/validate" "$(jq -nc --arg manifest "${manifest/namespace: edge-base/namespace: other-space}" '{manifest:$manifest,format:"yaml"}')"
expect_status 400 'manifest namespace boundary is enforced'

duplicate_manifest='apiVersion: v1
kind: Service
metadata:
  name: duplicate
  namespace: edge-base
---
apiVersion: v1
kind: Service
metadata:
  name: duplicate
  namespace: edge-base'
api_call POST "/api/projects/$project_id/deployment-config/validate" "$(jq -nc --arg manifest "$duplicate_manifest" '{manifest:$manifest,format:"yaml"}')"
expect_status 400 'duplicate manifest resources are rejected'

api_call POST "/api/projects/$project_id/deployment-config/convert" "$(jq -nc --arg manifest "$manifest" '{manifest:$manifest,format:"json"}')"
expect_status 200 'YAML to JSON conversion endpoint'
expect_json '.config.format == "json" and (.config.manifest | startswith("["))' 'multi-document conversion keeps every resource'
json_manifest="$(json_value '.config.manifest')"

api_call POST "/api/projects/$project_id/deployment-config/convert" "$(jq -nc --arg manifest "$json_manifest" '{manifest:$manifest,format:"yaml"}')"
expect_status 200 'JSON to YAML conversion endpoint'
expect_json '.config.format == "yaml" and (.config.resource_count == 2)' 'reverse conversion preserves resources'

api_call POST /api/projects "$manifest"
expect_status 400 'unrelated malformed project request is rejected'

api_call GET "/api/projects/$project_id/deployment-targets"
expect_status 200 'read default DEV environment'
dev_target_id="$(jq -r '.items[] | select(.stage == "dev") | .id' <<<"$last_body" | head -n 1)"
[[ -n "$dev_target_id" ]] || fail 'default DEV environment is missing'

api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster "$cluster_id" '{name:"Invalid first stage",environment:"invalid-stage",stage:"uat",sort_order:1,cluster_id:$cluster,namespace:"default",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 400 'only DEV may occupy order one'

api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster "$cluster_id" '{name:"Invalid stage",environment:"invalid-stage-2",stage:"not-a-stage",sort_order:2,cluster_id:$cluster,namespace:"default",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 400 'unknown environment stage is rejected'

api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster "$cluster_id" '{name:"Invalid namespace",environment:"invalid-namespace",stage:"uat",sort_order:2,cluster_id:$cluster,namespace:"Bad_Ns",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 400 'environment namespace is validated'

api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster "$cluster_id" '{name:"Outside cluster",environment:"outside-cluster",stage:"uat",sort_order:2,cluster_id:"missing-cluster",namespace:"default",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 404 'environment cannot reference another space cluster'

api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster "$cluster_id" '{name:"UAT",environment:"uat",stage:"uat",sort_order:2,cluster_id:$cluster,namespace:"default",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 201 'create UAT environment'
uat_target_id="$(json_value '.target.id')"

api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster "$cluster_id" '{name:"UAT duplicate",environment:"uat",stage:"uat",sort_order:2,cluster_id:$cluster,namespace:"default",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 409 'duplicate environment key is rejected'

api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster "$cluster_id" '{name:"PRE",environment:"pre",stage:"pre",sort_order:3,cluster_id:$cluster,namespace:"default",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 201 'create PRE environment'
pre_target_id="$(json_value '.target.id')"

api_call POST "/api/projects/$project_id/deployment-targets" "$(jq -nc --arg cluster "$cluster_id" '{name:"PROD",environment:"prod",stage:"prod",sort_order:4,cluster_id:$cluster,namespace:"default",replicas:1,container_port:80,deploy_strategy:"rolling"}')"
expect_status 201 'create PROD environment'
prod_target_id="$(json_value '.target.id')"

api_call GET "/api/projects/$project_id/deployment-targets"
expect_status 200 'list ordered deployment environments'
expect_json '[.items[].stage] | join(",") == "dev,uat,pre,prod"' 'environment order is explicit and stable'

api_call PATCH "/api/projects/$project_id/deployment-targets/$uat_target_id" '{"deploy_strategy":"canary"}'
expect_status 200 'environment owns its deployment strategy'
expect_json '.target.deploy_strategy == "canary"' 'environment strategy update is visible'

api_call PATCH "/api/projects/$project_id/deployment-targets/$uat_target_id" '{"deploy_strategy":"rolling"}'
expect_status 200 'environment strategy can be restored'

api_call PATCH "/api/projects/$project_id/deployment-targets/$pre_target_id" '{"enabled":false}'
expect_status 200 'environment can be disabled'

api_call PATCH "/api/projects/$project_id/deployment-targets/$pre_target_id" '{"enabled":true}'
expect_status 200 'environment can be re-enabled'

api_call DELETE "/api/projects/$project_id/deployment-targets/target-that-does-not-exist"
expect_status 404 'unknown environment cannot be deleted'

api_call DELETE "/api/projects/$project_id/deployment-targets/$dev_target_id"
expect_status 409 'DEV environment cannot be deleted'

api_call GET "/api/projects/$project_id/pods?target_id=$uat_target_id"
expect_status 200 'Pod list is backed by the selected Kubernetes environment'
expect_json '(.items | type == "array") and (.total | type == "number")' 'Pod list has a real empty-or-live shape'

api_call GET "/api/projects/$project_id/metrics?target_id=$uat_target_id"
expect_status 200 'project metrics endpoint reaches the real runtime'
expect_json '.pod_count | type == "number"' 'project metrics reports Pod count without fabricated samples'

api_call GET "/api/clusters/$cluster_id/metrics"
expect_status 200 'cluster metrics endpoint reaches the real runtime'

api_call GET "/api/projects/$project_id/pods/missing-pod?target_id=$uat_target_id"
expect_status 404 'unknown Pod is not fabricated'

api_call GET "/api/projects/$project_id/pods/missing-pod/logs?target_id=$uat_target_id&tail_lines=-1"
expect_status 400 'Pod log tail lower bound is enforced'

api_call GET "/api/projects/$project_id/pods/missing-pod/logs?target_id=$uat_target_id&tail_lines=10001"
expect_status 400 'Pod log tail upper bound is enforced'

api_call GET "/api/projects/$project_id/pods/missing-pod/logs?target_id=$uat_target_id"
expect_status 404 'unknown Pod logs are not fabricated'

api_call POST "/api/projects/$project_id/pods/missing-pod/exec?target_id=$uat_target_id" '{"command":""}'
expect_status 400 'empty Pod terminal command is rejected'

api_call POST "/api/projects/$project_id/pods/missing-pod/exec?target_id=$uat_target_id" '{"command":"printf edge"}'
expect_status 404 'terminal does not target an unknown Pod'

api_call PATCH "/api/projects/$project_id/pods/missing-pod/config?target_id=$uat_target_id" '{"config":{"LOG_LEVEL":"debug"}}'
expect_status 404 'Pod config update does not fabricate an unknown Pod'

api_call GET "/api/projects/$project_id/pods?target_id=missing-target"
expect_status 404 'unknown environment is rejected by runtime routes'

api_call GET "/api/projects/$project_id/releases"
expect_status 200 'release list endpoint'
expect_json '.items | type == "array" and length == 0' 'new project has no release records'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" --arg a "$sha_one" --arg b "$sha_two" '{branch:$branch,commit_shas:[$a,$b],target_ids:[$d],strategy:"rolling",name:"edge draft"}')"
expect_status 201 'create release draft from branch snapshot'
release_id="$(json_value '.release.id')"
expect_json '.release.status == "draft" and .release.branch == $branch and (.release.commits | length == 2) and (.release.targets | length == 1)' 'release stores branch, commits and target snapshot' --arg branch "$branch"

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" --arg a "$sha_one" --arg b "$sha_two" '{branch:$branch,commit_shas:[$a,$b],target_ids:[$d],strategy:"rolling",name:"different name"}')"
expect_status 200 'identical release plan is idempotent'
expect_json '.duplicate == true and .release.id == $id' 'duplicate release points to the original record' --arg id "$release_id"

api_call DELETE "/api/projects/$project_id/releases/$release_id/commits/$sha_two"
expect_status 200 'draft commit can be removed'
expect_json '.commits | length == 1' 'removed commit is absent from active release commits'

api_call DELETE "/api/projects/$project_id/releases/$release_id/commits/$sha_one"
expect_status 409 'last active commit cannot be removed'

api_call DELETE "/api/projects/$project_id/releases/$release_id/commits/not-a-commit"
expect_status 404 'unknown release commit is rejected'

api_call PATCH "/api/projects/$project_id/releases/$release_id/progress" '{"progress":10,"stage":"draft","message":"should not update"}'
expect_status 409 'draft progress is immutable'

api_call POST "/api/projects/$project_id/releases/$release_id/cancel" '{"message":"edge draft cancellation"}'
expect_status 200 'draft release can be cancelled'
expect_json '.release.status == "cancelled"' 'cancelled release is terminal'

api_call POST "/api/projects/$project_id/releases/$release_id/cancel" '{}'
expect_status 409 'terminal release cannot be cancelled twice'

api_call POST "/api/projects/$project_id/releases/$release_id/fail" '{"error":"late failure"}'
expect_status 409 'terminal release cannot be failed'

api_call DELETE "/api/projects/$project_id/releases/$release_id/commits/$sha_one"
expect_status 409 'terminal release commits remain immutable'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" --arg a "$sha_two" '{branch:$branch,commit_shas:[$a],target_ids:[$d],strategy:"rolling",name:"publish boundary"}')"
expect_status 201 'create publish-boundary release'
published_release_id="$(json_value '.release.id')"

api_call POST "/api/projects/$project_id/releases/$published_release_id/publish"
expect_status_any 'publish respects repository write and image builder boundaries' 202 403 501 502 504
if [[ "$last_status" == 202 ]]; then
  terminal_status=""
  for _ in {1..30}; do
    sleep 1
    api_call GET "/api/projects/$project_id/releases/$published_release_id"
    expect_status 200 'poll publish-boundary release'
    terminal_status="$(json_value '.release.status')"
    if [[ "$terminal_status" == "succeeded" || "$terminal_status" == "failed" || "$terminal_status" == "cancelled" ]]; then
      break
    fi
  done
  [[ "$terminal_status" == "succeeded" || "$terminal_status" == "failed" || "$terminal_status" == "cancelled" ]] || fail 'publish-boundary release did not reach a terminal state'
  pass "publish-boundary release reached $terminal_status"
  api_call GET "/api/projects/$project_id/releases/$published_release_id/targets/$dev_target_id/logs"
  expect_status 200 'read persisted environment execution logs'
  expect_json '.items | type == "array"' 'execution logs are returned as a list'
  if [[ "$terminal_status" == "failed" ]]; then
    expect_json 'any(.items[]; .source == "git") and any(.items[]; .source == "ttp")' 'raw Git and TTP execution lines are retained'
    api_call POST "/api/projects/$project_id/releases/$published_release_id/targets/$dev_target_id/retry"
    expect_status_any 'failed environment retry boundary' 202 403 501 502 504
  fi
else
  api_call GET "/api/projects/$project_id/releases/$published_release_id"
  expect_status 200 'read release after blocked publish'
  expect_json '.release.status == "draft"' 'blocked publish does not mutate the draft'
fi

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" --arg a "$sha_one" '{branch:$branch,commit_shas:[$a],target_ids:[$d],strategy:"canary",stable_percent:80,candidate_percent:20,name:"canary boundary"}')"
expect_status 201 'canary traffic split is accepted'
canary_release_id="$(json_value '.release.id')"
expect_json '.release.plan.traffic.stable_percent == 80 and .release.plan.traffic.candidate_percent == 20' 'canary percentages are persisted'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" --arg a "$sha_two" '{branch:$branch,commit_shas:[$a],target_ids:[$d],strategy:"canary",stable_percent:70,candidate_percent:20,name:"invalid canary"}')"
expect_status 400 'canary percentages must total one hundred'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" --arg a "$sha_two" '{branch:$branch,commit_shas:[$a],target_ids:[$d],strategy:"blue_green",blue_percent:50,green_percent:50,name:"blue green boundary"}')"
expect_status 201 'blue-green traffic split is accepted'
expect_json '.release.plan.traffic.stable_percent == 0 and .release.plan.traffic.candidate_percent == 0 and .release.plan.traffic.blue_percent == 50 and .release.plan.traffic.green_percent == 50' 'blue-green traffic fields remain distinct'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" --arg a "$sha_two" '{branch:$branch,commit_shas:[$a],target_ids:[$d],strategy:"rolling",stable_percent:90,candidate_percent:10,name:"invalid rolling"}')"
expect_status 400 'rolling release rejects non-stable traffic'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" '{branch:$branch,commit_shas:["missing-commit"],target_ids:[$d],strategy:"rolling"}')"
expect_status 404 'unknown release commit is rejected by Git'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg d "$dev_target_id" '{branch:"branch-that-does-not-exist",target_ids:[$d],strategy:"rolling"}')"
expect_status 404 'unknown release branch is rejected by Git'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg u "$uat_target_id" --arg a "$sha_one" '{branch:$branch,commit_shas:[$a],target_ids:[$u],strategy:"rolling"}')"
expect_status 400 'release selection must begin with DEV'

api_call PATCH "/api/projects/$project_id/deployment-targets/$pre_target_id" '{"enabled":false}'
expect_status 200 'disable environment for release boundary'
api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" --arg p "$pre_target_id" --arg a "$sha_one" '{branch:$branch,commit_shas:[$a],target_ids:[$d,$p],strategy:"rolling"}')"
expect_status 409 'disabled environment cannot be selected for release'
api_call PATCH "/api/projects/$project_id/deployment-targets/$pre_target_id" '{"enabled":true}'
expect_status 200 'restore disabled environment after boundary check'

api_call GET "/api/projects/$project_id/releases/not-a-release"
expect_status 404 'unknown release ID is rejected'

api_call GET "/api/projects/$project_id/releases/$canary_release_id/targets/not-a-target/logs"
expect_status 400 'unknown release target log binding is rejected'

api_call GET "/api/projects/$project_id/ab-experiments"
expect_status 200 'A/B experiment list endpoint'
expect_json '.total == 0 and (.items | length) == 0' 'new project has no A/B experiments'

api_call POST "/api/projects/$project_id/ab-experiments" "$(jq -nc --arg d "$dev_target_id" --arg a "$published_release_id" --arg b "$canary_release_id" '{name:"unsupported A/B",target_id:$d,a_release_id:$a,b_release_id:$b}')"
expect_status 501 'real Kubernetes runtime reports A/B routing as unsupported'
expect_json '.error.code == "ab_experiment_unsupported"' 'A/B unsupported boundary has a stable code'

api_call GET /api/audit-logs?limit=0
expect_status 400 'audit limit lower bound is enforced'

api_call GET /api/audit-logs?limit=201
expect_status 400 'audit limit upper bound is enforced'

api_call GET "/api/audit-logs?limit=200&space_id=$second_space_id"
expect_status 200 'audit query cannot widen the signed space scope'
expect_json 'all(.items[]; .space_id == $id)' 'audit records remain in the selected space' --arg id "$space_id"

api_call POST /api/space/members "$(jq -nc --arg user "$edge_username" --arg password "$viewer_password" '{username:$user,password:$password,role:"viewer"}')"
expect_status 201 'create viewer member'
viewer_id="$(json_value '.member.user_id')"

api_call POST /api/space/members "$(jq -nc --arg user "$edge_username" '{username:$user,role:"viewer"}')"
expect_status 409 'duplicate member is rejected'

api_call POST /api/space/members '{"username":"owner-attempt","password":"owner-pass","role":"owner"}'
expect_status 403 'normal member creation cannot create an owner'

api_call POST /api/space/members "$(jq -nc --arg user "$developer_username" --arg password "$developer_password" '{username:$user,password:$password,role:"developer"}')"
expect_status 201 'create developer member'
developer_id="$(json_value '.member.user_id')"

api_call GET /api/space/members
expect_status 200 'list space members'
expect_json 'any(.items[]; .user_id == ($viewer | tonumber) and .role == "viewer") and any(.items[]; .user_id == ($developer | tonumber) and .role == "developer")' 'member roles are persisted' --arg viewer "$viewer_id" --arg developer "$developer_id"
expect_redacted 'member list does not expose passwords'

token=""
api_call POST /api/auth/login "$(jq -nc --arg u "$edge_username" --arg p "$viewer_password" '{username:$u,password:$p}')"
expect_status 200 'viewer login'
viewer_token="$(json_value '.token')"
token="$viewer_token"
expect_json '.spaces | (length == 1 and .[0].id == $id)' 'viewer sees only the primary space' --arg id "$space_id"

api_call GET "/api/projects/$project_id"
expect_status 200 'viewer can read project'

api_call POST /api/clusters '{"name":"viewer-cluster"}'
expect_status 403 'viewer cannot manage clusters'

api_call POST /api/projects '{"name":"viewer-project","repository_url":"https://github.com/octocat/viewer-project","cluster_id":"local"}'
expect_status 403 'viewer cannot create projects'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" '{branch:$branch,target_ids:[$d],strategy:"rolling"}')"
expect_status 403 'viewer cannot create release drafts'

api_call POST "/api/auth/select-space" "$(jq -nc --arg id "$second_space_id" '{space_id:$id}')"
expect_status 403 'viewer cannot enter another space'

token="$admin_token"
api_call PATCH "/api/space/members/$viewer_id" '{"role":"developer"}'
expect_status 200 'owner can adjust a member role'
api_call PATCH "/api/space/members/$viewer_id" '{"role":"viewer"}'
expect_status 200 'owner can restore a member role'

token=""
api_call POST /api/auth/login "$(jq -nc --arg u "$developer_username" --arg p "$developer_password" '{username:$u,password:$p}')"
expect_status 200 'developer login'
developer_token="$(json_value '.token')"
token="$developer_token"

api_call GET "/api/projects/$project_id"
expect_status 200 'developer can read project'

api_call PATCH "/api/projects/$project_id" '{"description":"developer update boundary"}'
expect_status 200 'developer can update project configuration'

api_call POST /api/clusters '{"name":"developer-cluster"}'
expect_status 403 'developer cannot manage clusters'

api_call POST "/api/projects/$project_id/releases" "$(jq -nc --arg branch "$branch" --arg d "$dev_target_id" --arg a "$sha_two" '{branch:$branch,commit_shas:[$a],target_ids:[$d],strategy:"canary",stable_percent:90,candidate_percent:10,name:"developer draft"}')"
expect_status 201 'developer can create a release draft'

token="$admin_token"
api_call DELETE "/api/projects/$project_id/deployment-targets/$uat_target_id"
expect_status 200 'unused environment can be removed through the runtime cleanup boundary'

api_call GET "/api/projects/$project_id/deployment-targets"
expect_status 200 'environment list after deletion'
expect_json 'all(.items[]; .id != $id)' 'deleted environment is absent' --arg id "$uat_target_id"

api_call GET "/api/projects/$project_id/git/credential"
expect_status 200 'credential metadata remains readable after all checks'
expect_redacted 'final credential metadata remains redacted'

pass 'API edge regression completed; temporary resources will now be removed'
