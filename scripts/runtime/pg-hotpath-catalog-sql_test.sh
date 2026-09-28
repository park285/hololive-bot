#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CATALOG_SQL_LIB="${ROOT_DIR}/scripts/runtime/lib/pg-hotpath-catalog-sql.sh"

# shellcheck source=scripts/runtime/lib/pg-hotpath-catalog-sql.sh
source "${CATALOG_SQL_LIB}"
sql="$(dead_tuples_sql)"

for token in \
  "source_collection_checkpoints" \
  "source_observation_queue" \
  "source_observations" \
  "youtube_collection_job_leases" \
  "n_tup_newpage_upd" \
  "last_idx_scan" \
  "relation.reloptions" \
  "backend_xmin" \
  "xact_start IS NOT NULL OR backend_xmin IS NOT NULL" \
  "stats.schemaname = 'public'" \
  "indexes.schemaname = 'public'" \
  "'toast' AS section" \
  "JOIN pg_stat_all_tables AS toast ON toast.relid = relation.reltoastrelid" \
  "toast_relation.reloptions AS toast_reloptions"; do
  if [[ "${sql}" != *"${token}"* ]]; then
    echo "missing MVCC catalog SQL token: ${token}" >&2
    exit 1
  fi
done

# table·toast·index 절이 같은 대상을 봐야 한 테이블의 heap·TOAST·index 증거를 같은 snapshot에서
# 대조할 수 있다. 목록을 절마다 따로 적으므로 어느 한 절만 늘거나 줄면 여기서 실패한다.
expected_targets="$(printf '%s\n' \
  alarm_dispatch_deliveries \
  alarm_dispatch_send_units \
  youtube_notification_outbox \
  youtube_notification_delivery \
  source_collection_checkpoints \
  source_observation_queue \
  source_observations \
  youtube_collection_job_leases \
  source_observation_applications \
  youtube_collection_targets \
  youtube_collection_target_reasons \
  youtube_live_sessions \
  youtube_live_pending_ends \
  youtube_content_evidence_clocks \
  youtube_community_posts \
  youtube_content_alarm_tracking \
  bot_reply_outbox | sort)"

mapfile -t target_lists < <(awk -v quote="'" '
  /relname IN \($/ { collecting = 1; list = ""; next }
  collecting && /^\)$/ { print list; collecting = 0; next }
  collecting {
    gsub(/[[:space:],]/, "")
    gsub(quote, "")
    list = list (list == "" ? "" : " ") $0
  }
  END { if (collecting) print list }
' <<<"${sql}")

if (( ${#target_lists[@]} != 3 )); then
  echo "MVCC catalog SQL must have exactly three relname target lists (table, toast, index); found ${#target_lists[@]}" >&2
  exit 1
fi
for target_list in "${target_lists[@]}"; do
  observed_targets="$(tr ' ' '\n' <<<"${target_list}" | sort)"
  if [[ "${observed_targets}" != "${expected_targets}" ]]; then
    echo "MVCC catalog target list drifted: ${target_list}" >&2
    exit 1
  fi
done

database_state_sql="$(mvcc_database_state_sql)"

for token in \
  "idle_in_transaction_session_timeout" \
  "autovacuum_freeze_max_age" \
  "autovacuum_multixact_freeze_max_age" \
  "age(database_catalog.datfrozenxid)" \
  "mxid_age(database_catalog.datminmxid)" \
  "database_stats.stats_reset"; do
  if [[ "${database_state_sql}" != *"${token}"* ]]; then
    echo "missing MVCC database state SQL token: ${token}" >&2
    exit 1
  fi
done

for catalog_sql in "${sql}" "${database_state_sql}"; do
  if [[ "${catalog_sql}" == *"query"* ]]; then
    echo "MVCC catalog SQL must not expose active query text" >&2
    exit 1
  fi
done

echo "ok: pg hotpath catalog SQL preserves bounded secret-free MVCC evidence"
