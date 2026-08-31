#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

fixture_root="$TMP_DIR/repo"
mkdir -p "$fixture_root/hololive/hololive-api" "$fixture_root/hololive/hololive-alarm-worker"
printf '%s\n' 2.1.3 >"$fixture_root/hololive/hololive-api/VERSION"
printf '%s\n' 3.2.1 >"$fixture_root/hololive/hololive-alarm-worker/VERSION"

# shellcheck source=scripts/deploy/lib/ap-compose-version.sh
. "$ROOT_DIR/scripts/deploy/lib/ap-compose-version.sh"

unset HOLO_API_VERSION HOLO_ALARM_WORKER_VERSION
compose_export_release_versions "$fixture_root"
[[ "$HOLO_API_VERSION" == 2.1.3 ]] || fail "API version was not read from its VERSION file"
[[ "$HOLO_ALARM_WORKER_VERSION" == 3.2.1 ]] || fail "alarm worker version was not read from its VERSION file"

HOLO_API_VERSION=9.9.9
if compose_export_release_versions "$fixture_root" 2>/dev/null; then
  fail "mismatched caller-provided API version must fail closed"
fi

compose_file="$ROOT_DIR/deploy/compose/docker-compose.prod.yml"
[[ "$(grep -Fc 'HOLO_API_VERSION:?HOLO_API_VERSION must be injected by scripts/deploy/compose.sh' "$compose_file")" -eq 5 ]] \
  || fail "all API build/runtime metadata must require wrapper injection"
[[ "$(grep -Fc 'HOLO_ALARM_WORKER_VERSION:?HOLO_ALARM_WORKER_VERSION must be injected by scripts/deploy/compose.sh' "$compose_file")" -eq 1 ]] \
  || fail "alarm worker build metadata must require wrapper injection"
if grep -Fq 'HOLO_API_VERSION:-2.0.0' "$compose_file"; then
  fail "stale API version fallback remains in Compose"
fi

for wrapper in \
  "$ROOT_DIR/scripts/deploy/compose.sh" \
  "$ROOT_DIR/scripts/deploy/compose-redeploy-service.sh"; do
  expected_export="compose_export_release_versions \"\${ROOT_DIR}\""
  grep -Fq 'scripts/deploy/lib/ap-compose-version.sh' "$wrapper" \
    || fail "canonical wrapper does not source the version owner: $wrapper"
  grep -Fq "$expected_export" "$wrapper" \
    || fail "canonical wrapper does not inject repository release versions: $wrapper"
done

echo "ok: Compose versions are injected only from canonical VERSION files"
