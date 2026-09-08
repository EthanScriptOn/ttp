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
has_resource "ingresses" "networking.k8s.io" && pass "networking.k8s.io/ingresses API" || fail "networking.k8s.io/ingresses API is unavailable"
has_resource "horizontalpodautoscalers" "autoscaling" && pass "autoscaling/horizontalpodautoscalers API" || fail "autoscaling/horizontalpodautoscalers API is unavailable"

check_can_i() {
  local verb="$1"
  local resource="$2"
  if k_with_subject auth can-i "$verb" "$resource" --namespace "$namespace" --quiet >/dev/null 2>&1; then
    pass "can-i $verb $resource in $namespace"
  else
    fail "can-i $verb $resource in $namespace"
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
  'get services' 'list services' 'watch services' \
  'create services' 'update services' 'patch services' 'delete services' \
  'get configmaps' 'list configmaps' 'watch configmaps' \
  'create configmaps' 'update configmaps' 'patch configmaps' 'delete configmaps' \
  'get secrets' 'list secrets' 'watch secrets' \
  'create secrets' 'update secrets' 'patch secrets' 'delete secrets' \
  'get ingresses.networking.k8s.io' 'list ingresses.networking.k8s.io' \
  'create ingresses.networking.k8s.io' 'update ingresses.networking.k8s.io' 'patch ingresses.networking.k8s.io' 'delete ingresses.networking.k8s.io' \
  'get horizontalpodautoscalers.autoscaling' 'list horizontalpodautoscalers.autoscaling' \
  'create horizontalpodautoscalers.autoscaling' 'update horizontalpodautoscalers.autoscaling' 'patch horizontalpodautoscalers.autoscaling' 'delete horizontalpodautoscalers.autoscaling'; do
  read -r verb resource <<<"$permission"
  check_can_i "$verb" "$resource"
done

if k_with_subject auth can-i list nodes --all-namespaces --quiet >/dev/null 2>&1; then
  pass 'can-i list nodes'
else
  warn 'cannot list nodes; cluster-level node status will be unavailable'
fi

if k_with_subject get --raw /apis/metrics.k8s.io/v1beta1 >/dev/null 2>&1; then
  pass 'metrics.k8s.io API (metrics-server)'
else
  warn 'metrics.k8s.io API is unavailable; current CPU/memory metrics will be unavailable'
fi

if k_with_subject get --raw /apis/custom.metrics.k8s.io/v1beta1 >/dev/null 2>&1; then
  pass 'custom.metrics.k8s.io API'
else
  warn 'custom.metrics.k8s.io API is unavailable; application metrics still require Prometheus integration'
fi

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
