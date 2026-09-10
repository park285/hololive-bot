#!/usr/bin/env bash
set -euo pipefail
export DOCKER_HOST=unix:///var/run/docker.sock
unset DOCKER_CONTEXT
ADMIN_LAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [[ $# -lt 1 || $# -gt 2 || ! "$1" =~ ^sha256:[0-9a-f]{64}$ || ( $# -eq 2 && "$2" != --quick ) ]]; then
  echo 'usage: test-admin-bigbang-resource-soak.sh sha256:<local-fixture-image-id> [--quick]' >&2
  exit 2
fi
ADMIN_LAB_TIMEOUT=3900
ADMIN_LAB_REPORT="${ADMIN_LAB_ROOT}/admin-dashboard/frontend/node_modules/.cache/admin-resource-soak.json"
if [[ "${2:-}" == --quick ]]; then
  ADMIN_LAB_TIMEOUT=900
  ADMIN_LAB_REPORT="${ADMIN_LAB_ROOT}/admin-dashboard/frontend/node_modules/.cache/admin-resource-soak-quick.json"
fi
ADMIN_LAB_ID="admin-lab-resource-soak-$$-$(date +%s)"
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
  "$(command -v node)" scripts/deploy/test-admin-bigbang-resource-soak.mjs "$@"
# 종료 코드만으로 중단되거나 이전 fixture가 남긴 관찰을 성공으로 간주하지 않습니다.
"$(command -v node)" --input-type=module - "${ADMIN_LAB_REPORT}" "$1" "$ADMIN_LAB_ID" "${2:-}" <<'JS'
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
const report = JSON.parse(await fs.readFile(process.argv[2], 'utf8'));
const quick = process.argv[5] === '--quick', duration = quick ? 600 : 3600;
assert.equal(report.image, process.argv[3]);
assert.equal(report.fixture_run_id, process.argv[4]);
assert.equal(report.diagnostic_only, quick);
assert.equal(report.status, quick ? 'PASS_DIAGNOSTIC' : 'PASS');
assert(report.elapsed_ms >= duration * 1000);
assert.equal(report.samples.length, duration / 5);
assert.equal(report.cycles.length, 100);
assert.equal(report.after_close.active_streams, 0);
if (quick) {
  assert.equal(report.warmup.status, 'COMPLETE');
  assert(report.warmup.elapsed_ms >= 60000);
  assert.equal(report.warmup.family_heartbeats, 4);
  assert.equal(report.warmup.reconnect_cycles.length, 16);
  assert(report.warmup.reconnect_cycles.every(cycle => cycle.validated_frames >= 30 && cycle.active_peers === 16));
  assert(Date.parse(report.started_at) >= Date.parse(report.warmup.started_at) + report.warmup.elapsed_ms);
}
JS
