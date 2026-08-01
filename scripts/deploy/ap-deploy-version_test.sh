#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT_DIR/scripts/deploy/lib/ap-compose-version.sh"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

expect_failure() {
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    fail "$label"
  fi
}

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT
mkdir -p "$fixture_root/hololive/hololive-api"

printf '2.0.46\n' > "$fixture_root/VERSION"
printf '2.0.46\n' > "$fixture_root/hololive/hololive-api/VERSION"
[[ "$(ap_compose_release_version "$fixture_root")" == "2.0.46" ]] \
  || fail "matching release versions must resolve to HOLO_API_VERSION"

printf '9.8.7\n' > "$fixture_root/VERSION"
[[ "$(ap_compose_release_version "$fixture_root")" == "2.0.46" ]] \
  || fail "independent root/runtime releases must use hololive-api VERSION"

printf 'release-2.0.46\n' > "$fixture_root/hololive/hololive-api/VERSION"
expect_failure "invalid runtime VERSION must fail closed" ap_compose_release_version "$fixture_root"

deploy_script="$ROOT_DIR/scripts/deploy/ap-deploy.sh"
[[ "$(grep -Fc "sudo -n env HOLO_API_VERSION='\$HOLO_API_VERSION'" "$deploy_script")" -eq 4 ]] \
  || fail "every remote sudo Compose config/build/up invocation must propagate HOLO_API_VERSION"
grep -Fq "if [[ \"\$AP_RUNTIME_MODE\" == \"compose\" ]]; then" "$deploy_script" \
  || fail "release version resolution must remain scoped to Compose AP hosts"

echo "all AP Compose release version checks passed"
