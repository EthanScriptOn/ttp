#!/usr/bin/env bash
set -Eeuo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -n "${GO_BIN:-}" ]]; then
  go_bin="$GO_BIN"
elif command -v go >/dev/null 2>&1; then
  go_bin="$(command -v go)"
elif [[ -x /opt/homebrew/opt/go/libexec/bin/go ]]; then
  go_bin=/opt/homebrew/opt/go/libexec/bin/go
else
  go_bin=go
fi
if [[ -n "${PNPM_BIN:-}" ]]; then
  pnpm_bin="$PNPM_BIN"
else
  pnpm_bin="$(command -v pnpm || true)"
fi

command -v "$go_bin" >/dev/null 2>&1 || { printf '%s is required\n' "$go_bin" >&2; exit 127; }
command -v "$pnpm_bin" >/dev/null 2>&1 || { printf '%s is required\n' "$pnpm_bin" >&2; exit 127; }

printf '%s\n' '== Go unit and integration tests =='
(cd "$root_dir/backend" && "$go_bin" test ./...)

printf '%s\n' '== Go race tests =='
(cd "$root_dir/backend" && "$go_bin" test -race ./...)

printf '%s\n' '== Go vet =='
(cd "$root_dir/backend" && "$go_bin" vet ./...)

printf '%s\n' '== Frontend production build =='
(cd "$root_dir/frontend" && "$pnpm_bin" run build)

if [[ "${RUN_LIVE_CHECK:-NO}" == "YES" ]]; then
  command -v curl >/dev/null 2>&1 || { printf 'curl is required for RUN_LIVE_CHECK=YES\n' >&2; exit 127; }
  base_url="${CICD_BASE_URL:-http://127.0.0.1:8790}"
  printf '== Live health check: %s ==\n' "$base_url/api/health"
  curl --fail --silent --show-error --max-time 10 "$base_url/api/health"
  printf '\n'
fi

printf '%s\n' 'Regression checks completed.'
