#!/usr/bin/env bash
set -Eeuo pipefail

namespace="${KUBE_NAMESPACE:-}"
context="${KUBE_CONTEXT:-}"
service_account="${KUBE_SERVICE_ACCOUNT:-}"
service_account_namespace="${KUBE_SERVICE_ACCOUNT_NAMESPACE:-ttp-system}"

usage() {
  printf 'Usage: %s --namespace NAME [--context NAME] [--service-account NAME] [--service-account-namespace NAME]\n' "$0"
}

while (($# > 0)); do
  case "$1" in
    --namespace)
      (($# >= 2)) || { usage >&2; exit 2; }
      namespace="$2"
      shift 2
      ;;
    --context)
      (($# >= 2)) || { usage >&2; exit 2; }
      context="$2"
      shift 2
      ;;
    --service-account)
      (($# >= 2)) || { usage >&2; exit 2; }
      service_account="$2"
      shift 2
      ;;
    --service-account-namespace)
      (($# >= 2)) || { usage >&2; exit 2; }
      service_account_namespace="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$namespace" ]]; then
  printf 'KUBE_NAMESPACE or --namespace is required\n' >&2
  exit 2
fi
if [[ ! "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
  printf 'invalid Kubernetes namespace: %s\n' "$namespace" >&2
  exit 2
fi
if [[ ! "$service_account_namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
  printf 'invalid ServiceAccount namespace: %s\n' "$service_account_namespace" >&2
  exit 2
fi
if [[ -n "$service_account" && ! "$service_account" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
  printf 'invalid ServiceAccount name: %s\n' "$service_account" >&2
  exit 2
fi
command -v kubectl >/dev/null 2>&1 || { printf 'kubectl is required\n' >&2; exit 127; }

kubectl_args=()
if [[ -n "$context" ]]; then
  kubectl_args+=(--context "$context")
fi
k() {
  if ((${#kubectl_args[@]} > 0)); then
    kubectl "${kubectl_args[@]}" "$@"
  else
    kubectl "$@"
  fi
}

printf 'Kubernetes context: '
k config current-context
printf 'Namespace: %s\n' "$namespace"
auth_subject_args=()
if [[ -n "$service_account" ]]; then
  auth_subject_args=(--as "system:serviceaccount:${service_account_namespace}:${service_account}")
  printf 'Authorization subject: ServiceAccount %s/%s\n' "$service_account_namespace" "$service_account"
fi
k version --request-timeout=10s >/dev/null

failures=0
warnings=0

pass() { printf 'PASS  %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; failures=$((failures + 1)); }
warn() { printf 'WARN  %s\n' "$1"; warnings=$((warnings + 1)); }

has_resource() {
  local resource="$1"
  local group="$2"
  k api-resources --api-group "$group" --no-headers 2>/dev/null | awk -v wanted="$resource" '$1 == wanted { found=1 } END { exit found ? 0 : 1 }'
}

has_resource "deployments" "apps" && pass "apps/deployments API" || fail "apps/deployments API is unavailable"
has_resource "statefulsets" "apps" && pass "apps/statefulsets API" || fail "apps/statefulsets API is unavailable"
has_resource "daemonsets" "apps" && pass "apps/daemonsets API" || fail "apps/daemonsets API is unavailable"
has_resource "jobs" "batch" && pass "batch/jobs API" || fail "batch/jobs API is unavailable"
has_resource "cronjobs" "batch" && pass "batch/cronjobs API" || fail "batch/cronjobs API is unavailable"
has_resource "ingresses" "networking.k8s.io" && pass "networking.k8s.io/ingresses API" || fail "networking.k8s.io/ingresses API is unavailable"
has_resource "horizontalpodautoscalers" "autoscaling" && pass "autoscaling/horizontalpodautoscalers API" || fail "autoscaling/horizontalpodautoscalers API is unavailable"
has_resource "rolebindings" "rbac.authorization.k8s.io" && pass "rbac.authorization.k8s.io/rolebindings API" || fail "rbac.authorization.k8s.io/rolebindings API is unavailable"

check_can_i() {
  local verb="$1"
  local resource="$2"
  if k_with_subject auth can-i "$verb" "$resource" --namespace "$namespace" --quiet >/dev/null 2>&1; then
    pass "can-i $verb $resource in $namespace"
  else
    fail "can-i $verb $resource in $namespace"
  fi
}

check_cluster_can_i() {
  local verb="$1"
  local resource="$2"
  if k_with_subject auth can-i "$verb" "$resource" --all-namespaces --quiet >/dev/null 2>&1; then
    pass "can-i $verb $resource cluster-wide"
  else
    fail "can-i $verb $resource cluster-wide"
  fi
}

k_with_subject() {
  if ((${#auth_subject_args[@]} > 0)); then
    k "$@" "${auth_subject_args[@]}"
  else
    k "$@"
  fi
}

for permission in \
  'get pods' 'list pods' 'watch pods' \
  'get pods/log' 'create pods/exec' \
  'delete pods' \
  'get deployments.apps' 'list deployments.apps' 'watch deployments.apps' \
  'create deployments.apps' 'update deployments.apps' 'patch deployments.apps' 'delete deployments.apps' \
  'get replicasets.apps' 'list replicasets.apps' 'delete replicasets.apps' \
  'get statefulsets.apps' 'list statefulsets.apps' 'watch statefulsets.apps' \
  'create statefulsets.apps' 'update statefulsets.apps' 'patch statefulsets.apps' 'delete statefulsets.apps' \
  'get daemonsets.apps' 'list daemonsets.apps' 'watch daemonsets.apps' \
  'create daemonsets.apps' 'update daemonsets.apps' 'patch daemonsets.apps' 'delete daemonsets.apps' \
  'get jobs.batch' 'list jobs.batch' 'watch jobs.batch' \
  'create jobs.batch' 'update jobs.batch' 'patch jobs.batch' 'delete jobs.batch' \
  'get cronjobs.batch' 'list cronjobs.batch' 'watch cronjobs.batch' \
  'create cronjobs.batch' 'update cronjobs.batch' 'patch cronjobs.batch' 'delete cronjobs.batch' \
  'get services' 'list services' 'watch services' \
  'create services' 'update services' 'patch services' 'delete services' \
  'get configmaps' 'list configmaps' 'watch configmaps' \
  'create configmaps' 'update configmaps' 'patch configmaps' 'delete configmaps' \
  'get secrets' 'list secrets' 'watch secrets' \
  'create secrets' 'update secrets' 'patch secrets' 'delete secrets' \
  'get persistentvolumeclaims' 'list persistentvolumeclaims' 'watch persistentvolumeclaims' \
  'create persistentvolumeclaims' 'update persistentvolumeclaims' 'patch persistentvolumeclaims' 'delete persistentvolumeclaims' \
  'get resourcequotas' 'list resourcequotas' 'watch resourcequotas' \
  'create resourcequotas' 'update resourcequotas' 'patch resourcequotas' 'delete resourcequotas' \
  'get limitranges' 'list limitranges' 'watch limitranges' \
  'create limitranges' 'update limitranges' 'patch limitranges' 'delete limitranges' \
  'get rolebindings.rbac.authorization.k8s.io' 'list rolebindings.rbac.authorization.k8s.io' 'watch rolebindings.rbac.authorization.k8s.io' \
  'create rolebindings.rbac.authorization.k8s.io' 'update rolebindings.rbac.authorization.k8s.io' 'patch rolebindings.rbac.authorization.k8s.io' 'delete rolebindings.rbac.authorization.k8s.io' \
  'get ingresses.networking.k8s.io' 'list ingresses.networking.k8s.io' \
  'create ingresses.networking.k8s.io' 'update ingresses.networking.k8s.io' 'patch ingresses.networking.k8s.io' 'delete ingresses.networking.k8s.io' \
  'get horizontalpodautoscalers.autoscaling' 'list horizontalpodautoscalers.autoscaling' \
  'create horizontalpodautoscalers.autoscaling' 'update horizontalpodautoscalers.autoscaling' 'patch horizontalpodautoscalers.autoscaling' 'delete horizontalpodautoscalers.autoscaling'; do
  read -r verb resource <<<"$permission"
  check_can_i "$verb" "$resource"
done

for permission in \
  'get namespaces' 'list namespaces' 'watch namespaces' \
  'create namespaces' 'update namespaces' 'patch namespaces' \
  'bind clusterroles.rbac.authorization.k8s.io/ttp-runtime-namespace-access'; do
  read -r verb resource <<<"$permission"
  check_cluster_can_i "$verb" "$resource"
done

if k_with_subject auth can-i list nodes --all-namespaces --quiet >/dev/null 2>&1; then
  pass 'can-i list nodes'
else
  warn 'cannot list nodes; cluster-level node status will be unavailable'
fi

for monitoring_resource in \
  'deployment/prometheus' \
  'deployment/kube-state-metrics' \
  'daemonset/node-exporter'; do
  if k_with_subject get "$monitoring_resource" --namespace ttp-monitoring >/dev/null 2>&1; then
    pass "$monitoring_resource in ttp-monitoring"
  else
    warn "$monitoring_resource is unavailable; install Prometheus monitoring from TTP"
  fi
done

if [[ -n "${PROMETHEUS_URL:-}" ]]; then
  command -v curl >/dev/null 2>&1 || { printf 'curl is required when PROMETHEUS_URL is set\n' >&2; exit 127; }
  if curl --fail --silent --show-error --max-time 5 "$PROMETHEUS_URL/-/ready" >/dev/null; then
    pass 'Prometheus endpoint'
  else
    warn "Prometheus endpoint is not ready: $PROMETHEUS_URL"
  fi
else
  warn 'PROMETHEUS_URL is not set; historical metrics were not checked'
fi

printf '\nSummary: %d failure(s), %d warning(s)\n' "$failures" "$warnings"
if ((failures > 0)); then
  exit 1
fi
