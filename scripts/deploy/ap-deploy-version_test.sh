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

# 중앙 compose wrapper의 export는 두 runtime VERSION을 읽고, 호출자가 넘긴 다른 API 버전은 거부한다.
export_root="$fixture_root/export"
mkdir -p "$export_root/hololive/hololive-api" "$export_root/hololive/hololive-alarm-worker"
printf '2.1.3\n' > "$export_root/hololive/hololive-api/VERSION"
printf '3.2.1\n' > "$export_root/hololive/hololive-alarm-worker/VERSION"
unset HOLO_API_VERSION HOLO_ALARM_WORKER_VERSION
compose_export_release_versions "$export_root"
[[ "$HOLO_API_VERSION" == 2.1.3 ]] || fail "API version must be read from its VERSION file"
[[ "$HOLO_ALARM_WORKER_VERSION" == 3.2.1 ]] || fail "alarm worker version must be read from its VERSION file"
HOLO_API_VERSION=9.9.9
expect_failure "mismatched caller-provided API version must fail closed" compose_export_release_versions "$export_root"
unset HOLO_API_VERSION HOLO_ALARM_WORKER_VERSION

# 실제 버전 해석과 전송 경계 거부를 검증합니다. 스크립트 문자열·등장 횟수는 계약이 아닙니다.
preview_checker="$ROOT_DIR/scripts/deploy/check-ap-rsync-preview.sh"

allowed_preview="$fixture_root/allowed-preview.txt"
printf '%s\n' \
  '.d..t...... hololive-bot/hololive/hololive-alarm-worker/' \
  '<f.st...... hololive-bot/hololive/hololive-alarm-worker/VERSION' \
  >"$allowed_preview"
"$preview_checker" "$allowed_preview"

forbidden_preview="$fixture_root/forbidden-preview.txt"
printf '%s\n' \
  'cL+++++++++ hololive-bot/hololive/hololive-alarm-worker/VERSION -> ../shadow/hololive/hololive-alarm-worker/VERSION' \
  >"$forbidden_preview"
expect_failure "AP preview must reject a VERSION symlink record" "$preview_checker" "$forbidden_preview"

printf '%s\n' \
  '>f+++++++++ hololive-bot/shadow/hololive/hololive-alarm-worker/VERSION' \
  >"$forbidden_preview"
expect_failure "AP preview must reject a suffix-matching nested path" "$preview_checker" "$forbidden_preview"

printf '%s\n' \
  '>f+++++++++ hololive-bot/hololive/hololive-alarm-worker/secret.go' \
  >"$forbidden_preview"
expect_failure "AP preview must reject any other alarm worker child" "$preview_checker" "$forbidden_preview"


echo "all AP Compose release version checks passed"
