#!/usr/bin/env bash
set -euo pipefail
export DOCKER_HOST=unix:///var/run/docker.sock
unset DOCKER_CONTEXT
ADMIN_LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [[ $# -lt 2 || $# -gt 3 || ! "$1" =~ ^sha256:[0-9a-f]{64}$ || ! "$2" =~ ^sha256:[0-9a-f]{64}$ || ( $# -eq 3 && "$3" != --quick ) ]]; then
  echo 'usage: test-admin-bigbang-native-performance.sh <native-baseline-image> <native-candidate-image> [--quick]' >&2
  exit 2
fi
ADMIN_LAB_TIMEOUT=4800
ADMIN_LAB_REPORT="${ADMIN_LAB_ROOT}/admin-dashboard/frontend/node_modules/.cache/admin-native-performance.json"
if [[ "${3:-}" == --quick ]]; then
  ADMIN_LAB_TIMEOUT=600
  ADMIN_LAB_REPORT="${ADMIN_LAB_ROOT}/admin-dashboard/frontend/node_modules/.cache/admin-native-performance-quick.json"
fi
ADMIN_LAB_ID="admin-lab-native-performance-$$-$(date +%s)"
cleanup() {
  local id
  while IFS= read -r id; do
    if [[ "$id" =~ ^[0-9a-f]+$ ]]; then
      docker stop --time 25 "$id" >/dev/null
      docker rm "$id" >/dev/null
    fi
  done < <(docker ps -aq --filter "label=io.hololive.admin-fixture=${ADMIN_LAB_ID}")
  while IFS= read -r id; do
    [[ "$id" =~ ^[0-9a-f]+$ ]] && docker network rm "$id" >/dev/null
  done < <(docker network ls -q --filter "label=io.hololive.admin-fixture=${ADMIN_LAB_ID}")
  if [[ -d "/tmp/${ADMIN_LAB_ID}" && ! -L "/tmp/${ADMIN_LAB_ID}" && -O "/tmp/${ADMIN_LAB_ID}" ]]; then
    sudo -n chown -R "$(id -u):$(id -g)" "/tmp/${ADMIN_LAB_ID}"
    rm -r -- "/tmp/${ADMIN_LAB_ID}"
  fi
}
trap cleanup EXIT
systemd-run --user --quiet --wait --pipe --collect --unit="${ADMIN_LAB_ID}" \
  --property="RuntimeMaxSec=${ADMIN_LAB_TIMEOUT}" --property=KillMode=control-group --property=TimeoutStopSec=5 \
  --working-directory="${ADMIN_LAB_ROOT}" --setenv="ADMIN_FIXTURE_RUN_ID=${ADMIN_LAB_ID}" --setenv="PATH=${PATH}" \
  "$(command -v node)" scripts/deploy/test-admin-bigbang-native-performance.mjs "$@"
# 수동 unit 중단도 0으로 반환할 수 있으므로 이번 fixture의 실제 완료 기록을 확인합니다.
"$(command -v node)" --input-type=module - "${ADMIN_LAB_REPORT}" "$1" "$2" "$ADMIN_LAB_ID" "${3:-}" <<'JS'
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
const report = JSON.parse(await fs.readFile(process.argv[2], 'utf8'));
assert.equal(report.baseline_image, process.argv[3]);
assert.equal(report.candidate_image, process.argv[4]);
assert.equal(report.fixture_run_id, process.argv[5], 'evidence from another fixture cannot pass');
const quick = process.argv[6] === '--quick';
assert.equal(report.diagnostic_only, quick);
assert.equal(report.status, quick ? 'PASS_DIAGNOSTIC' : 'PASS', 'interrupted or failed measurement cannot pass');
assert.equal(report.runs.length, quick ? 4 : 12);
assert(report.runs.every(run => run.status === 'PASS_SAMPLE' && run.exit_code === 0));
JS
