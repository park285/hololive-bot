#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${REPO_ROOT}"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

require_file() {
  local path="$1"
  [[ -f "${path}" ]] || fail "missing file: ${path}"
}

require_executable() {
  local path="$1"
  require_file "${path}"
  [[ -x "${path}" ]] || fail "not executable: ${path}"
}

require_grep() {
  local pattern="$1"
  local path="$2"
  grep -Eq -- "${pattern}" "${path}" || fail "missing pattern in ${path}: ${pattern}"
}

require_executable "scripts/logs/remote-sync-main-logs.sh"
require_file "scripts/systemd/hololive-main-log-mirror@.service"
require_file "scripts/systemd/hololive-main-log-mirror@.timer"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

bash -n scripts/logs/remote-sync-main-logs.sh
if LOG_ROOT="${tmpdir}" scripts/logs/remote-sync-main-logs.sh query osaka '../bad' >"${tmpdir}/invalid-service.out" 2>&1; then
  fail "invalid service query unexpectedly succeeded"
fi
grep -q "unknown service for osaka" "${tmpdir}/invalid-service.out" \
  || fail "invalid service query did not fail at service validation"
if LOG_ROOT="${tmpdir}" scripts/logs/remote-sync-main-logs.sh docker-tail osaka youtube-scraper --tail nope >"${tmpdir}/invalid-tail.out" 2>&1; then
  fail "invalid docker-tail --tail unexpectedly succeeded"
fi
grep -q -- "--tail must be a positive integer" "${tmpdir}/invalid-tail.out" \
  || fail "invalid docker-tail --tail did not fail at validation"

sed "s#/home/kapu/gemini/hololive-bot#${REPO_ROOT}#g" \
  scripts/systemd/hololive-main-log-mirror@.service \
  > "${tmpdir}/hololive-main-log-mirror@.service"
cp scripts/systemd/hololive-main-log-mirror@.timer \
  "${tmpdir}/hololive-main-log-mirror@.timer"
if ! systemd-analyze verify \
  "${tmpdir}/hololive-main-log-mirror@.service" \
  "${tmpdir}/hololive-main-log-mirror@.timer" >"${tmpdir}/systemd-verify.out" 2>&1; then
  cat "${tmpdir}/systemd-verify.out" >&2
  fail "systemd unit verification failed"
fi

python3 - <<'PY'
import re
from pathlib import Path
text = Path("docker-compose.osaka.yml").read_text()
for service in ("stream-ingester", "youtube-scraper"):
    match = re.search(rf"(?ms)^  {re.escape(service)}:\n(?P<block>.*?)(?=^  [A-Za-z0-9_-]+:|\Z)", text)
    if not match:
        raise SystemExit(f"FAIL: service block missing: {service}")
    block = match.group("block")
    required = {
        'LOG_MAX_SIZE_MB: "20"',
        'LOG_MAX_BACKUPS: "10"',
        'LOG_MAX_AGE_DAYS: "14"',
        'max-size: "5m"',
        'max-file: "3"',
    }
    missing = sorted(item for item in required if item not in block)
    if missing:
        raise SystemExit(f"FAIL: {service} missing {', '.join(missing)}")
PY

require_grep '"entrypoint": "scripts/logs/remote-sync-main-logs.sh"' \
  docs/hololive-main-server-logs-mirror-v2/manifest.json
require_grep 'scripts/systemd/hololive-main-log-mirror@\.service' \
  docs/hololive-main-server-logs-mirror-v2/README.md
require_grep 'scripts/systemd/hololive-main-log-mirror@\.timer' \
  docs/hololive-main-server-logs-mirror-v2/README.md

echo "main log mirror v2 verification passed"
