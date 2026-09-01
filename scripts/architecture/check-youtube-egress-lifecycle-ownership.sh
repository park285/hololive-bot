#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${ROOT_DIR:-$(cd "${SCRIPT_DIR}/../.." && pwd)}"
cd "${ROOT_DIR}"

alarm_worker="hololive/hololive-alarm-worker"
hololive_root="hololive"
poller_queries="hololive/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo/queries"

fail=0

report_hits() {
  local label="$1"
  local hits="$2"

  if [[ -n "${hits}" ]]; then
    echo "[FAIL] ${label}" >&2
    echo "${hits}" >&2
    fail=1
  else
    echo "[PASS] ${label}"
  fi
}

mutation_pattern='\b(?:UPDATE|DELETE[[:space:]]+FROM)[[:space:]]+(?:public\.)?youtube_notification_(?:outbox|delivery)\b|\bINSERT[[:space:]]+INTO[[:space:]]+(?:public\.)?youtube_notification_(?:delivery|delivery_ledger|delivery_ledger_state)\b'

outside_worker_hits="$(
  rg -n -i -U "${mutation_pattern}" "${hololive_root}" \
    -g '*.go' -g '*.sql' -g '!*_test.go' \
    -g '!**/hololive-alarm-worker/**' \
    -g '!**/hololive-dbtest/**' \
    -g '!**/migrations/**' \
    || true
)"
report_hits \
  "only alarm-worker owns existing outbox and delivery lifecycle mutations" \
  "${outside_worker_hits}"

worker_noncanonical_hits="$(
  rg -n -i -U "${mutation_pattern}" "${alarm_worker}" \
    -g '*.go' -g '*.sql' -g '!*_test.go' \
    -g '!**/internal/egress/youtubedispatch/store/queries/**' \
    -g '!**/internal/egress/youtubedispatch/backfill/queries/**' \
    || true
)"
report_hits \
  "alarm-worker lifecycle SQL stays in the canonical store or bounded backfill" \
  "${worker_noncanonical_hits}"

legacy_import_hits="$(
  rg -n -F 'github.com/kapu/hololive-shared/pkg/service/youtube/outbox/store' "${hololive_root}" \
    -g '*.go' -g '!*_test.go' \
    || true
)"
report_hits "legacy shared lifecycle store has no production importer" "${legacy_import_hits}"

legacy_writer_hits="$(
  rg -n 'StatusUpdater|recoverSuccessfulCommunityShortsSentState|markRecoveredSentDeliveryRows' \
    "${alarm_worker}/internal/egress/youtubedispatch" \
    -g '*.go' -g '*.sql' -g '!*_test.go' \
    || true
)"
report_hits "retired lifecycle writers remain absent" "${legacy_writer_hits}"

poller_conflict_hits=""
if [[ -d "${poller_queries}" ]]; then
  poller_conflict_hits="$(
    awk '
      BEGIN { IGNORECASE = 1; in_outbox_insert = 0 }
      /INSERT[[:space:]]+INTO[[:space:]]+(public\.)?youtube_notification_outbox/ {
        in_outbox_insert = 1
        next
      }
      /INSERT[[:space:]]+INTO[[:space:]]+/ {
        in_outbox_insert = 0
      }
      in_outbox_insert && /DO[[:space:]]+UPDATE/ {
        print FILENAME ":" FNR ":" $0
      }
    ' "${poller_queries}"/repository_batch_writes_*.sql 2>/dev/null || true
  )"
fi
report_hits "poller outbox insert never rewrites an existing lifecycle row" "${poller_conflict_hits}"

if [[ "${fail}" -ne 0 ]]; then
  exit 1
fi

echo "[PASS] YouTube egress lifecycle ownership gate"
