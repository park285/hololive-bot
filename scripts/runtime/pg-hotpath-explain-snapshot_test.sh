#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT="${ROOT_DIR}/scripts/runtime/pg-hotpath-explain-snapshot.sh"
CATALOG_SQL_LIB="${ROOT_DIR}/scripts/runtime/lib/pg-hotpath-catalog-sql.sh"
CLAIM_WINDOW_LIB="${ROOT_DIR}/scripts/runtime/lib/pg-hotpath-claim-window.sh"

sql="$("${SCRIPT}" --print-sql)"

require() {
  local token="$1"
  if [[ "${sql}" != *"${token}"* ]]; then
    echo "missing SQL token: ${token}" >&2
    exit 1
  fi
}

require "EXPLAIN (ANALYZE, BUFFERS)"
require "alarm_dispatch_deliveries"
require "youtube_notification_outbox"
require "idx_alarm_dispatch_deliveries_due"
require "idx_yno_pending_due_created_id"
require "definition_ok"
require "INTERVAL '5 minutes'"
require "INTERVAL '2 hours'"
require "FOR UPDATE SKIP LOCKED"
require "ROLLBACK;"
require "pg_stat_user_tables"
require "pg_stat_statements"
require "pg_stat_statements_info"
require "pg_sleep("
require ":'stats_window_seconds'::double precision"
require "FULL OUTER JOIN finish_claims"
require "finish.calls - COALESCE(start.calls, 0)"
require "finish.total_exec_time - COALESCE(start.total_exec_time, 0)"
require "statements.toplevel"
require "statements.stats_since"
require "AND finish.toplevel = start.toplevel"
require "paired_claims.toplevel::text"
require "start_stats_since"
require "finish_stats_since"
require "start_stats_since IS DISTINCT FROM finish_stats_since"
require "statement_stats_reset"
require "statements.query ILIKE '%FOR UPDATE SKIP LOCKED%'"
require "statements.query NOT ILIKE '%pg_stat_statements%'"
require "statements.query NOT LIKE '%hololive-pg-hotpath-stats-observer%'"
require "hololive-pg-hotpath-stats-observer"
require "fingerprint.is_alarm <> fingerprint.is_youtube"
require "UPDATE[[:space:]]+alarm_dispatch_deliveries[[:space:]]+d[[:space:]]+SET"
require "lock_expires_at[[:space:]]*=[[:space:]]*NOW"
require "RETURNING[[:space:]]+d[.]id[[:space:]]*,[[:space:]]*d[.]event_id"
require "d[.]claim_keys"
require "d[.]delivery_context"
require "UPDATE[[:space:]]+youtube_notification_outbox[[:space:]]+o[[:space:]]+SET[[:space:]]+locked_at"
require "FROM[[:space:]]+claim[[:space:]]+WHERE[[:space:]]+o[.]id"
require "RETURNING[[:space:]]+o[.]id[[:space:]]*,[[:space:]]*o[.]kind"
require "o[.]payload::text[[:space:]]+AS[[:space:]]+payload"

if [[ "${sql}" == *"mean_exec_time"* ]]; then
  echo "printed SQL must evaluate interval deltas, not the cumulative lifetime mean" >&2
  exit 1
fi
if [[ "${sql}" == *"left(regexp_replace"* ]]; then
  echo "claim query evidence must not truncate the normalized runtime SQL" >&2
  exit 1
fi

if [[ "${sql}" == *"INTERVAL '10 minutes'"* || "${sql}" == *"INTERVAL '30 days'"* ]]; then
  echo "printed SQL must match current runtime claim windows" >&2
  exit 1
fi

if [[ "${sql}" == *"PGPASSWORD"* || "${sql}" == *"DB_PASSWORD"* ]]; then
  echo "printed SQL must not reference password-bearing environment variables" >&2
  exit 1
fi

compact_sql_file() {
  tr '\n\t' '  ' < "$1" | sed -E 's/[[:space:]]+/ /g'
}

alarm_claim_source="$(compact_sql_file "${ROOT_DIR}/hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_claim_0053_02.sql")"
youtube_claim_source="$(compact_sql_file "${ROOT_DIR}/hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch/queries/dispatcher_claim_0050_01.sql")"

if (( ${#alarm_claim_source} <= 500 || ${#youtube_claim_source} <= 500 )); then
  echo "runtime claim fixtures must retain their full post-500-character structure" >&2
  exit 1
fi
alarm_late_fragment_prefix="${alarm_claim_source%%FROM updated*}"
youtube_late_fragment_prefix="${youtube_claim_source%%FROM updated*}"
if [[ "${alarm_late_fragment_prefix}" == "${alarm_claim_source}" \
  || "${youtube_late_fragment_prefix}" == "${youtube_claim_source}" \
  || ${#alarm_late_fragment_prefix} -le 500 \
  || ${#youtube_late_fragment_prefix} -le 500 ]]; then
  echo "runtime claim fixtures must exercise a fingerprint fragment after character 500" >&2
  exit 1
fi

for fragment in \
  "WITH picked AS (" \
  "), updated AS (" \
  "UPDATE alarm_dispatch_deliveries d" \
  "lock_expires_at = NOW() +" \
  "RETURNING d.id, d.event_id" \
  "d.claim_keys" \
  "d.delivery_context"; do
  if [[ "${alarm_claim_source}" != *"${fragment}"* ]]; then
    echo "alarm runtime claim no longer matches fingerprint fragment: ${fragment}" >&2
    exit 1
  fi
done

for fragment in \
  "WITH claim AS (" \
  "), updated AS (" \
  "UPDATE youtube_notification_outbox o" \
  "SET locked_at" \
  "FROM claim WHERE o.id = claim.id" \
  "RETURNING o.id, o.kind, o.channel_id, o.content_id" \
  "o.payload::text AS payload" \
  "FROM updated"; do
  if [[ "${youtube_claim_source}" != *"${fragment}"* ]]; then
    echo "youtube runtime claim no longer matches fingerprint fragment: ${fragment}" >&2
    exit 1
  fi
done

for non_claim_source_path in \
  "${ROOT_DIR}/hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_maintenance_0010_01.sql" \
  "${ROOT_DIR}/hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_maintenance_0035_02.sql"; do
  non_claim_source="$(compact_sql_file "${non_claim_source_path}")"
  if [[ "${non_claim_source}" == *"), updated AS ("* \
    || "${non_claim_source}" == *"RETURNING d.id, d.event_id"* \
    || "${non_claim_source}" == *"d.claim_keys"* \
    || "${non_claim_source}" == *"d.delivery_context"* ]]; then
    echo "alarm maintenance SQL must not satisfy the runtime claim fingerprint: ${non_claim_source_path}" >&2
    exit 1
  fi
done

youtube_revive_source="$(compact_sql_file "${ROOT_DIR}/hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch/queries/dispatcher_claim_revive_0109_01.sql")"
if [[ "${youtube_revive_source}" == *"), updated AS ("* \
  || "${youtube_revive_source}" == *"UPDATE youtube_notification_outbox o"* \
  || "${youtube_revive_source}" == *"RETURNING o.id, o.kind, o.channel_id, o.content_id"* ]]; then
  echo "youtube revive SQL must not satisfy the runtime claim fingerprint" >&2
  exit 1
fi

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

for runtime_library in "${CATALOG_SQL_LIB}" "${CLAIM_WINDOW_LIB}"; do
  if output="$(bash "${runtime_library}" 2>&1)"; then
    echo "runtime library must reject direct execution: ${runtime_library}" >&2
    exit 1
  fi
  if [[ "${output}" != *"must be sourced by pg-hotpath-explain-snapshot.sh"* ]]; then
    echo "runtime library direct-execution failure is not actionable: ${output}" >&2
    exit 1
  fi
done

cp "${SCRIPT}" "${workdir}/detached-pg-hotpath-explain-snapshot.sh"
if output="$(bash "${workdir}/detached-pg-hotpath-explain-snapshot.sh" --print-sql 2>&1)"; then
  echo "detached runtime entrypoint must fail without its source libraries" >&2
  exit 1
fi
if [[ "${output}" != *"required pg hotpath runtime library is missing or unreadable"* ]]; then
  echo "detached runtime entrypoint failure is not actionable: ${output}" >&2
  exit 1
fi

fake_psql="${workdir}/psql"
cat >"${fake_psql}" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${FAKE_PSQL_ARGV_LOG}"
printf '%s\n' "${PGDATABASE:-}" >> "${FAKE_PSQL_DSN_LOG}"
out_file=""
stats_window_seconds=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o)
      out_file="$2"
      shift 2
      ;;
    stats_window_seconds=*)
      stats_window_seconds="${1#stats_window_seconds=}"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

append_record() {
  local destination="$1"
  shift
  local IFS='|'
  printf '%s\n' "$*" >> "${destination}"
}

if [[ -n "$out_file" ]]; then
  case "$out_file" in
    *invalid-indexes*)
      if [[ "${FAKE_INVALID_INDEX:-false}" == "true" ]]; then
        printf '%s\n' 'public|idx_broken|f|f' > "$out_file"
      else
        : > "$out_file"
      fi
      ;;
    *target-indexes*)
      if [[ "${FAKE_INVALID_TARGET_INDEX:-false}" == "true" ]]; then
        printf '%s\n' 'idx_alarm_dispatch_deliveries_due|t|t|t' > "$out_file"
        printf '%s\n' 'idx_yno_pending_due_created_id|t|t|f' >> "$out_file"
      else
        printf '%s\n' 'idx_alarm_dispatch_deliveries_due|t|t|t' > "$out_file"
        printf '%s\n' 'idx_yno_pending_due_created_id|t|t|t' >> "$out_file"
      fi
      ;;
    *claim-statement-window*)
      window_status="ok"
      start_stats_since="2026-07-01 00:00:00+00"
      finish_stats_since="${start_stats_since}"
      if [[ "${FAKE_STATS_RESET:-false}" == "true" ]]; then
        window_status="stats_reset"
      elif [[ "${FAKE_ENTRY_DEALLOCATED:-false}" == "true" ]]; then
        window_status="entry_deallocated"
      elif [[ "${FAKE_COUNTER_DISCONTINUITY:-false}" == "true" ]]; then
        window_status="counter_discontinuity"
      elif [[ "${FAKE_SHORT_WINDOW:-false}" == "true" ]]; then
        window_status="short_window"
      elif [[ "${FAKE_STATEMENT_STATS_RESET:-false}" == "true" ]]; then
        window_status="statement_stats_reset"
      fi
      if [[ "${FAKE_COUNTER_RECOVERED_AFTER_STATEMENT_RESET:-false}" == "true" ]]; then
        finish_stats_since="2026-07-10 00:00:30+00"
      fi
      : > "$out_file"
      append_record "$out_file" \
        window "${window_status}" "2026-07-10 00:00:00+00" "2026-07-10 00:01:00+00" \
        "${stats_window_seconds}" 0 0 "" "" "" "" "" "" "" "" "" "" ""
      if [[ "${FAKE_DUPLICATE_WINDOW:-false}" == "true" ]]; then
        append_record "$out_file" \
          window "${window_status}" "2026-07-10 00:00:00+00" "2026-07-10 00:01:00+00" \
          "${stats_window_seconds}" 0 0 "" "" "" "" "" "" "" "" "" "" ""
      fi
      if [[ "${FAKE_NO_FRESH_CLAIM:-false}" != "true" && "${window_status}" == "ok" ]]; then
        if [[ "${FAKE_ALARM_EXPIRED_LEASE_COLLISION:-false}" == "true" ]]; then
          append_record "$out_file" \
            claim "" "" "" "" "" "" alarm_dispatch 1 2 2101 true \
            "${start_stats_since}" "${finish_stats_since}" 3 6.000 2.000 \
            "WITH picked AS ( SELECT id FROM alarm_dispatch_deliveries WHERE lock_expires_at < NOW() FOR UPDATE SKIP LOCKED ) UPDATE alarm_dispatch_deliveries d SET status=retry FROM picked"
        elif [[ "${FAKE_ALARM_STALE_SEND_COLLISION:-false}" == "true" ]]; then
          append_record "$out_file" \
            claim "" "" "" "" "" "" alarm_dispatch 1 2 2102 true \
            "${start_stats_since}" "${finish_stats_since}" 3 6.000 2.000 \
            "WITH picked AS ( SELECT id FROM alarm_dispatch_deliveries WHERE sending_started_at < NOW() FOR UPDATE SKIP LOCKED ) UPDATE alarm_dispatch_deliveries d SET status=quarantined FROM picked"
        else
          append_record "$out_file" \
            claim "" "" "" "" "" "" alarm_dispatch 1 2 2001 true \
            "${start_stats_since}" "${finish_stats_since}" 3 6.000 2.000 \
            "${FAKE_ALARM_QUERY:?}"
        fi
        if [[ "${FAKE_MISSING_YOUTUBE_CLAIM:-false}" != "true" ]]; then
          if [[ "${FAKE_YOUTUBE_REVIVE_COLLISION:-false}" == "true" ]]; then
            append_record "$out_file" \
              claim "" "" "" "" "" "" youtube_outbox 1 2 3101 true \
              "${start_stats_since}" "${finish_stats_since}" 2 4.000 2.000 \
              "SELECT FROM youtube_notification_outbox EXISTS delivery FOR UPDATE SKIP LOCKED"
          elif [[ "${FAKE_SLOW_CLAIM:-false}" == "true" ]]; then
            append_record "$out_file" \
              claim "" "" "" "" "" "" youtube_outbox 1 2 3001 true \
              "${start_stats_since}" "${finish_stats_since}" 2 16.500 8.250 \
              "${FAKE_YOUTUBE_QUERY:?}"
          else
            append_record "$out_file" \
              claim "" "" "" "" "" "" youtube_outbox 1 2 3001 true \
              "${start_stats_since}" "${finish_stats_since}" 2 4.000 2.000 \
              "${FAKE_YOUTUBE_QUERY:?}"
          fi
        fi
      fi
      ;;
    *dead-tuples*) printf '%s\n' 'alarm_dispatch_deliveries|10|2' > "$out_file" ;;
    *alarm-dispatch*)
      printf '%s\n' 'Index Scan using idx_alarm_dispatch_deliveries_due' > "$out_file"
      if [[ "${FAKE_ROWS_REMOVED:-false}" == "true" ]]; then
        printf '%s\n' 'Rows Removed by Filter: 1001' >> "$out_file"
      fi
      ;;
    *youtube-outbox*)
      printf '%s\n' 'Index Scan using idx_yno_status_created' > "$out_file"
      ;;
    *) : > "$out_file" ;;
  esac
fi
SH
chmod +x "${fake_psql}"

secret_dsn="postgres://user:secret@example.invalid/hololive"
FAKE_PSQL_ARGV_LOG="${workdir}/argv" \
FAKE_PSQL_DSN_LOG="${workdir}/dsn" \
FAKE_ALARM_QUERY="${alarm_claim_source}" \
FAKE_YOUTUBE_QUERY="${youtube_claim_source}" \
PATH="${workdir}:${PATH}" \
DATABASE_URL="${secret_dsn}" \
  "${SCRIPT}" --stats-window-seconds 1 --output-dir "${workdir}/out" >/dev/null

for artifact in \
  invalid-indexes.txt \
  target-indexes.txt \
  dead-tuples-autovacuum.txt \
  claim-statement-window.txt \
  alarm-dispatch-claim-explain.txt \
  youtube-outbox-claim-explain.txt; do
  if [[ ! -f "${workdir}/out/${artifact}" ]]; then
    echo "missing snapshot artifact: ${artifact}" >&2
    exit 1
  fi
done

if grep -q "secret" "${workdir}/argv"; then
  echo "DATABASE_URL must not be passed through psql argv" >&2
  exit 1
fi
if ! grep -qx "${secret_dsn}" "${workdir}/dsn"; then
  echo "DATABASE_URL must be passed through PGDATABASE" >&2
  exit 1
fi

assert_gate_rejects() {
  local env_name="$1"
  local want="$2"
  local output

  if output="$(
    FAKE_PSQL_ARGV_LOG="${workdir}/argv-${env_name}" \
    FAKE_PSQL_DSN_LOG="${workdir}/dsn-${env_name}" \
    FAKE_ALARM_QUERY="${alarm_claim_source}" \
    FAKE_YOUTUBE_QUERY="${youtube_claim_source}" \
    PATH="${workdir}:${PATH}" \
    DATABASE_URL="${secret_dsn}" \
    env "${env_name}=true" "${SCRIPT}" --output-dir "${workdir}/out-${env_name}" 2>&1
  )"; then
    echo "${env_name} must fail the release gate" >&2
    exit 1
  fi
  if [[ "${output}" != *"${want}"* ]]; then
    echo "${env_name} failure missing ${want}: ${output}" >&2
    exit 1
  fi
}

assert_gate_rejects FAKE_INVALID_INDEX "invalid or unready indexes"
assert_gate_rejects FAKE_INVALID_TARGET_INDEX "required claim indexes are missing, invalid, unready, or structurally incorrect"
assert_gate_rejects FAKE_SLOW_CLAIM "claim statements exceed 5ms delta mean"
assert_gate_rejects FAKE_NO_FRESH_CLAIM "no fresh claim executions observed"
assert_gate_rejects FAKE_MISSING_YOUTUBE_CLAIM "one or more required hot paths have no fresh claim execution"
assert_gate_rejects FAKE_STATS_RESET "claim statement window is discontinuous (stats_reset)"
assert_gate_rejects FAKE_ENTRY_DEALLOCATED "claim statement window is discontinuous (entry_deallocated)"
assert_gate_rejects FAKE_COUNTER_DISCONTINUITY "claim statement window is discontinuous (counter_discontinuity)"
assert_gate_rejects FAKE_SHORT_WINDOW "claim statement window is discontinuous (short_window)"
assert_gate_rejects FAKE_STATEMENT_STATS_RESET "claim statement window is discontinuous (statement_stats_reset)"
assert_gate_rejects FAKE_COUNTER_RECOVERED_AFTER_STATEMENT_RESET "claim statement stats_since changed"
assert_gate_rejects FAKE_ALARM_EXPIRED_LEASE_COLLISION "claim statement fingerprint does not match its target"
assert_gate_rejects FAKE_ALARM_STALE_SEND_COLLISION "claim statement fingerprint does not match its target"
assert_gate_rejects FAKE_YOUTUBE_REVIVE_COLLISION "claim statement fingerprint does not match its target"
assert_gate_rejects FAKE_DUPLICATE_WINDOW "claim statement window must contain exactly one window record"
assert_gate_rejects FAKE_ROWS_REMOVED "Rows Removed by Filter exceeds 1000"

if output="$("${SCRIPT}" --stats-window-seconds 0 --print-sql 2>&1)"; then
  echo "zero-length stats window must be rejected" >&2
  exit 1
fi
if [[ "${output}" != *"integer between 1 and 3600"* ]]; then
  echo "invalid stats window failure missing validation message: ${output}" >&2
  exit 1
fi

echo "ok: pg hotpath EXPLAIN script prints secret-free rollback-bounded plan SQL"
