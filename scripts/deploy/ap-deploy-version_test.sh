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
literal_dollar='$'
[[ "$(grep -Fc "sudo -n env HOLO_API_VERSION='\$HOLO_API_VERSION'" "$deploy_script")" -eq 4 ]] \
  || fail "every remote sudo Compose config/build/up invocation must propagate HOLO_API_VERSION"
[[ "$(grep -Fc "HOLO_API_VERSION='\$HOLO_API_VERSION' REVISION='\$REVISION'" "$deploy_script")" -eq 3 ]] \
  || fail "post-rsync Compose config/build/up must propagate the exact source revision"
grep -Fq "built_revision=\\${literal_dollar}(docker image inspect -f" "$deploy_script" \
  || fail "AP cutover must inspect the built image revision label"
grep -Fq 'built_revision\" == ' "$deploy_script" \
  || fail "AP cutover must require the exact built image revision"
build_line="$(grep -n "build \$services_list" "$deploy_script" | cut -d: -f1)"
built_verify_line="$(grep -nF "built_revision=\\${literal_dollar}(docker image inspect" "$deploy_script" | cut -d: -f1)"
up_line="$(grep -n "up -d --no-deps" "$deploy_script" | cut -d: -f1)"
[[ "${build_line}" -lt "${built_verify_line}" && "${built_verify_line}" -lt "${up_line}" ]] \
  || fail "AP built image revision verification must run after build and before up"
grep -Fq "actual_revision=\\${literal_dollar}(docker inspect -f" "$deploy_script" \
  || fail "AP completion must inspect the live image revision label"
grep -Fq 'org.opencontainers.image.revision' "$deploy_script" \
  || fail "AP completion must inspect the OCI revision label"
grep -Fq "[[ \\\"\\${literal_dollar}actual_revision\\\" == \\\"\\${literal_dollar}expected_revision\\\" ]]" "$deploy_script" \
  || fail "AP completion must require an exact live image revision match"
grep -Fq "if [[ \"\$AP_RUNTIME_MODE\" != \"compose\" ]]; then" "$deploy_script" \
  || fail "Compose AP deploy must reject non-Compose hosts before resolving the release version"

echo "all AP Compose release version checks passed"
