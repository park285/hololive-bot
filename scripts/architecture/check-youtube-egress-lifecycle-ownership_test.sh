#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHECKER="${ROOT_DIR}/scripts/architecture/check-youtube-egress-lifecycle-ownership.sh"
TEST_TMP_DIR="$(mktemp -d)"
trap 'rm -rf -- "${TEST_TMP_DIR}"' EXIT

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

fixture="${TEST_TMP_DIR}/fixture"
shared_queries="${fixture}/hololive/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo/queries"
store_queries="${fixture}/hololive/hololive-alarm-worker/internal/egress/youtubedispatch/store/queries"
backfill_queries="${fixture}/hololive/hololive-alarm-worker/internal/egress/youtubedispatch/backfill/queries"
worker_other="${fixture}/hololive/hololive-alarm-worker/internal/other"
mkdir -p "${shared_queries}" "${store_queries}" "${backfill_queries}" "${worker_other}"

cat >"${shared_queries}/repository_batch_writes_0001_01.sql" <<'EOF'
INSERT INTO youtube_notification_outbox (kind, content_id)
VALUES
EOF
cat >"${shared_queries}/repository_batch_writes_0002_02.sql" <<'EOF'
ON CONFLICT (kind, content_id) DO NOTHING
EOF
cat >"${store_queries}/canonical.sql" <<'EOF'
UPDATE youtube_notification_delivery SET status = 'SENDING' WHERE id = $1;
EOF
cat >"${backfill_queries}/bounded.sql" <<'EOF'
INSERT INTO youtube_notification_delivery_ledger_state (singleton) VALUES (TRUE);
EOF

ROOT_DIR="${fixture}" "${CHECKER}" >"${TEST_TMP_DIR}/clean.out" 2>&1 \
  || {
    sed 's/^/  /' "${TEST_TMP_DIR}/clean.out" >&2
    fail "clean ownership fixture was rejected"
  }

cat >"${shared_queries}/forbidden_update.sql" <<'EOF'
UPDATE youtube_notification_delivery SET status = 'SENT' WHERE id = $1;
EOF
if ROOT_DIR="${fixture}" "${CHECKER}" >"${TEST_TMP_DIR}/shared.out" 2>&1; then
  fail "shared direct lifecycle update was not rejected"
fi
grep -Fq 'forbidden_update.sql' "${TEST_TMP_DIR}/shared.out" \
  || fail "shared lifecycle violation did not identify its source"
rm -- "${shared_queries}/forbidden_update.sql"

cat >"${shared_queries}/repository_batch_writes_0002_02.sql" <<'EOF'
ON CONFLICT (kind, content_id) DO UPDATE SET status = EXCLUDED.status
EOF
if ROOT_DIR="${fixture}" "${CHECKER}" >"${TEST_TMP_DIR}/conflict.out" 2>&1; then
  fail "poller outbox conflict rewrite was not rejected"
fi
grep -Fq 'repository_batch_writes_0002_02.sql' "${TEST_TMP_DIR}/conflict.out" \
  || fail "poller conflict violation did not identify its source"
cat >"${shared_queries}/repository_batch_writes_0002_02.sql" <<'EOF'
ON CONFLICT (kind, content_id) DO NOTHING
EOF

cat >"${worker_other}/forbidden.sql" <<'EOF'
DELETE FROM youtube_notification_outbox WHERE id = $1;
EOF
if ROOT_DIR="${fixture}" "${CHECKER}" >"${TEST_TMP_DIR}/worker.out" 2>&1; then
  fail "noncanonical worker lifecycle SQL was not rejected"
fi
grep -Fq 'forbidden.sql' "${TEST_TMP_DIR}/worker.out" \
  || fail "worker lifecycle violation did not identify its source"
rm -- "${worker_other}/forbidden.sql"

cat >"${worker_other}/legacy.go" <<'EOF'
package other

import _ "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/store"
EOF
if ROOT_DIR="${fixture}" "${CHECKER}" >"${TEST_TMP_DIR}/legacy.out" 2>&1; then
  fail "legacy shared store import was not rejected"
fi
grep -Fq 'legacy.go' "${TEST_TMP_DIR}/legacy.out" \
  || fail "legacy import violation did not identify its source"

echo "[PASS] YouTube egress lifecycle ownership fixtures"
