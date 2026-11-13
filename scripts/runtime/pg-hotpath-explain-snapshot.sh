#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CATALOG_SQL_LIB="${SCRIPT_DIR}/lib/pg-hotpath-catalog-sql.sh"
CLAIM_WINDOW_LIB="${SCRIPT_DIR}/lib/pg-hotpath-claim-window.sh"

if [[ ! -r "${CATALOG_SQL_LIB}" || ! -r "${CLAIM_WINDOW_LIB}" ]]; then
  echo "required pg hotpath runtime library is missing or unreadable" >&2
  exit 2
fi

# shellcheck source=scripts/runtime/lib/pg-hotpath-catalog-sql.sh
source "${CATALOG_SQL_LIB}"
# shellcheck source=scripts/runtime/lib/pg-hotpath-claim-window.sh
source "${CLAIM_WINDOW_LIB}"

required_functions=(
  invalid_indexes_sql
  target_indexes_sql
  mvcc_database_state_sql
  dead_tuples_sql
  claim_statement_window_sql
  alarm_claim_sql
  youtube_outbox_claim_sql
  query_matches_all_patterns
  claim_query_matches_target
)
for required_function in "${required_functions[@]}"; do
  if ! declare -F "${required_function}" >/dev/null; then
    echo "required pg hotpath runtime function is missing: ${required_function}" >&2
    exit 2
  fi
done

usage() {
  cat <<'EOF'
usage: pg-hotpath-explain-snapshot.sh [--output-dir DIR] [--stats-window-seconds N] [--print-sql] [--no-index-check] [--analyze]

Captures PostgreSQL EXPLAIN snapshots and MVCC evidence for Hololive alarm/youtube hot paths.

Connection:
  Prefer DATABASE_URL, or standard PG* environment variables accepted by psql.
  DATABASE_URL must be a postgres:// URI without a password, with query keys on the
  script's libpq allowlist; use PGPASSWORD or a password file.
  The script does not print secrets.
  Requires PostgreSQL 18 pg_stat_statements fields: toplevel and stats_since.

Claim plans:
  By default the claim UPDATEs are planned with EXPLAIN (SETTINGS) and not executed, so
  the script writes nothing; the stats window statement still holds a snapshot for the
  whole window, so keep the window short on production. --analyze runs them with EXPLAIN (ANALYZE, BUFFERS)
  inside a rolled-back transaction and checks Rows Removed by Filter. Use it only on a
  local fixture DB: ROLLBACK does not undo row locks held meanwhile, WAL, or dead tuples.

Outputs:
  invalid-indexes.txt
  target-indexes.txt
  mvcc-database-state.txt
  dead-tuples-autovacuum.txt
  claim-statement-window.txt
  alarm-dispatch-claim-explain.txt
  youtube-outbox-claim-explain.txt

Required catalog indexes:
  alarm_dispatch_deliveries: idx_alarm_dispatch_deliveries_due, idx_alarm_dispatch_deliveries_send_unit, idx_alarm_dispatch_deliveries_send_unit_due
  youtube_notification_outbox: idx_yno_pending_due_created_id
EOF
}

output_dir=""
print_sql=false
index_check=true
analyze=false
stats_window_seconds=60

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir)
      output_dir="${2:-}"
      shift 2
      ;;
    --print-sql)
      print_sql=true
      shift
      ;;
    --stats-window-seconds)
      stats_window_seconds="${2:-}"
      shift 2
      ;;
    --no-index-check)
      index_check=false
      shift
      ;;
    --analyze)
      analyze=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! "${stats_window_seconds}" =~ ^[0-9]+$ ]] \
  || (( stats_window_seconds < 1 || stats_window_seconds > 3600 )); then
  echo "--stats-window-seconds must be an integer between 1 and 3600" >&2
  exit 2
fi

if [[ "${print_sql}" == "true" ]]; then
  printf '%s\n' '-- invalid-indexes.sql'
  invalid_indexes_sql
  printf '\n%s\n' '-- target-indexes.sql'
  target_indexes_sql
  printf '\n%s\n' '-- mvcc-database-state.sql'
  mvcc_database_state_sql
  printf '\n%s\n' '-- dead-tuples-autovacuum.sql'
  dead_tuples_sql
  printf '\n%s\n' '-- claim-statement-window.sql'
  claim_statement_window_sql
  printf '\n%s\n' '-- alarm-dispatch-claim-explain.sql'
  alarm_claim_sql
  printf '\n%s\n' '-- youtube-outbox-claim-explain.sql'
  youtube_outbox_claim_sql
  exit 0
fi

# database_url_without_secrets는 --dbname 인자(ps로 보임)로 넘길 URL에 비밀값이 들 수 없음을 확인한다. 대괄호 host 안의
# '?' 때문에 질의 시작을 정확히 집을 수 없고 libpq는 키 둘레 공백을 버리고 decode하므로, 키는 허용 목록과 그대로 비교한다.
database_url_without_secrets() {
  local LC_ALL=C
  local rest param
  local -a params

  [[ "$1" =~ ^postgres(ql)?:// && "$1" != *[[:cntrl:]]* ]] || return 1
  rest="${1#*://}"
  # 인코딩하지 않은 '/'가 든 비밀번호는 libpq가 user info로 읽지 않아도 인자에 남으므로 첫 '@' 앞의 ':'를 모두 막는다.
  [[ "${rest}" != *@* || "${rest%%@*}" != *:* ]] || return 1
  [[ "${rest%%/*}" != *@* ]] || rest="${rest#*@}"
  [[ "${rest}" == *\?* ]] || return 0
  IFS='?&' read -r -a params <<< "${rest#*\?}"
  for param in "${params[@]}"; do
    [[ "${param}" == *=* ]] || continue
    case "${param%%=*}" in
      host|hostaddr|port|dbname|user|passfile|service|connect_timeout|client_encoding|options \
        |application_name|fallback_application_name|keepalives|keepalives_idle|keepalives_interval \
        |keepalives_count|tcp_user_timeout|channel_binding|sslmode|sslnegotiation|sslcompression|sslcert \
        |sslkey|sslcertmode|sslrootcert|sslcrl|sslcrldir|sslsni|requirepeer|require_auth \
        |min_protocol_version|max_protocol_version|ssl_min_protocol_version|ssl_max_protocol_version \
        |gssencmode|krbsrvname|gsslib|gssdelegation|target_session_attrs|load_balance_hosts) ;;
      *) return 1 ;;
    esac
  done
}

if [[ -n "${DATABASE_URL:-}" ]] && ! database_url_without_secrets "${DATABASE_URL}"; then
  echo "DATABASE_URL must be a postgres:// or postgresql:// URI without a password or control characters, whose query keys are on the script's libpq allowlist; supply the password through PGPASSWORD or a password file" >&2
  exit 2
fi

if [[ -z "${output_dir}" ]]; then
  output_dir="artifacts/pg-hotpath-explain/$(date -u +%Y%m%dT%H%M%SZ)"
fi

mkdir -p "${output_dir}"

run_psql() {
  local name="$1"
  local out_file="${output_dir}/${name}"
  shift

  if [[ -n "${DATABASE_URL:-}" ]]; then
    # libpq는 PGDATABASE 환경변수의 URL을 접속 문자열로 풀지 않으므로 --dbname으로 넘긴다.
    PGSERVICEFILE=/dev/null psql -X -v ON_ERROR_STOP=1 --dbname="${DATABASE_URL}" -o "${out_file}" "$@"
  else
    psql -X -v ON_ERROR_STOP=1 -o "${out_file}" "$@"
  fi
  echo "[PASS] wrote ${out_file}"
}

invalid_indexes_sql | run_psql invalid-indexes.txt -qAt
target_indexes_sql | run_psql target-indexes.txt -qAt -F '|'
mvcc_database_state_sql | run_psql mvcc-database-state.txt
dead_tuples_sql | run_psql dead-tuples-autovacuum.txt
claim_statement_window_sql | run_psql claim-statement-window.txt -qAt -F '|' \
  -v stats_window_seconds="${stats_window_seconds}"
claim_explain_options='(SETTINGS)'
[[ "${analyze}" != true ]] || claim_explain_options='(ANALYZE, BUFFERS)'
alarm_claim_sql | run_psql alarm-dispatch-claim-explain.txt -v claim_explain_options="${claim_explain_options}"
youtube_outbox_claim_sql | run_psql youtube-outbox-claim-explain.txt -v claim_explain_options="${claim_explain_options}"

if [[ -s "${output_dir}/invalid-indexes.txt" ]]; then
  echo "[FAIL] invalid or unready indexes detected; see ${output_dir}/invalid-indexes.txt" >&2
  exit 1
fi

validate_target_indexes() {
  local count=0
  local invalid=0
  local index_name ready valid definition_ok

  while IFS='|' read -r index_name ready valid definition_ok; do
    [[ -z "${index_name}" ]] && continue
    count=$((count + 1))
    if [[ "${ready}" != "t" || "${valid}" != "t" || "${definition_ok}" != "t" ]]; then
      invalid=1
    fi
  done < "${output_dir}/target-indexes.txt"

  if [[ "${count}" -ne 4 || "${invalid}" -ne 0 ]]; then
    echo "[FAIL] required claim indexes are missing, invalid, unready, or structurally incorrect; see ${output_dir}/target-indexes.txt" >&2
    return 1
  fi
}

if [[ "${index_check}" == "true" ]]; then
  validate_target_indexes
fi

validate_claim_statement_window() {
  local window_count=0
  local window_status=""
  local window_requested_seconds=""
  local claim_count=0
  local alarm_claim_count=0
  local youtube_claim_count=0
  local slow_claim_count=0
  local unmatched_alarm_count=0
  local unmatched_youtube_count=0
  local record_type status started_at finished_at requested_seconds
  local start_dealloc finish_dealloc claim_target dbid userid queryid toplevel
  local start_stats_since finish_stats_since delta_calls
  local delta_total_ms delta_mean_ms query

  while IFS='|' read -r \
    record_type status started_at finished_at requested_seconds \
    start_dealloc finish_dealloc claim_target dbid userid queryid toplevel \
    start_stats_since finish_stats_since delta_calls \
    delta_total_ms delta_mean_ms query; do
    case "${record_type}" in
      window)
        if [[ -z "${started_at}" || -z "${finished_at}" \
          || ! "${requested_seconds}" =~ ^[0-9]+$ \
          || ! "${start_dealloc}" =~ ^[0-9]+$ \
          || ! "${finish_dealloc}" =~ ^[0-9]+$ ]]; then
          echo "[FAIL] malformed claim statement window metadata" >&2
          return 1
        fi
        window_count=$((window_count + 1))
        window_status="${status}"
        window_requested_seconds="${requested_seconds}"
        ;;
      claim)
        if [[ ! "${claim_target}" =~ ^(alarm_dispatch|youtube_outbox)$ \
          || ! "${dbid}" =~ ^[0-9]+$ \
          || ! "${userid}" =~ ^[0-9]+$ \
          || ! "${queryid}" =~ ^-?[0-9]+$ \
          || ! "${toplevel}" =~ ^(true|false)$ \
          || -z "${finish_stats_since}" \
          || ! "${delta_calls}" =~ ^[1-9][0-9]*$ \
          || ! "${delta_total_ms}" =~ ^[0-9]+([.][0-9]+)?$ \
          || ! "${delta_mean_ms}" =~ ^[0-9]+([.][0-9]+)?$ \
          || -z "${query}" ]]; then
          echo "[FAIL] malformed claim statement delta record" >&2
          return 1
        fi
        if [[ -n "${start_stats_since}" && "${start_stats_since}" != "${finish_stats_since}" ]]; then
          echo "[FAIL] claim statement stats_since changed within the observation window" >&2
          return 1
        fi
        if ! claim_query_matches_target "${claim_target}" "${query}"; then
          echo "[FAIL] claim statement fingerprint does not match its target" >&2
          return 1
        fi
        claim_count=$((claim_count + 1))
        case "${claim_target}" in
          alarm_dispatch) alarm_claim_count=$((alarm_claim_count + 1)) ;;
          youtube_outbox) youtube_claim_count=$((youtube_claim_count + 1)) ;;
        esac
        if awk -v mean_ms="${delta_mean_ms}" 'BEGIN { exit !((mean_ms + 0) > 5) }'; then
          slow_claim_count=$((slow_claim_count + 1))
        fi
        ;;
      unmatched)
        if [[ ! "${claim_target}" =~ ^(alarm_dispatch|youtube_outbox)$ \
          || ! "${delta_calls}" =~ ^[1-9][0-9]*$ \
          || -z "${query}" ]]; then
          echo "[FAIL] malformed unmatched claim-shaped statement record" >&2
          return 1
        fi
        case "${claim_target}" in
          alarm_dispatch) unmatched_alarm_count=$((unmatched_alarm_count + 1)) ;;
          youtube_outbox) unmatched_youtube_count=$((unmatched_youtube_count + 1)) ;;
        esac
        ;;
      "")
        ;;
      *)
        echo "[FAIL] malformed claim statement window record: ${record_type}" >&2
        return 1
        ;;
    esac
  done < "${output_dir}/claim-statement-window.txt"

  if [[ "${window_count}" -ne 1 ]]; then
    echo "[FAIL] claim statement window must contain exactly one window record; see ${output_dir}/claim-statement-window.txt" >&2
    return 1
  fi
  if [[ "${window_status}" != "ok" ]]; then
    echo "[FAIL] claim statement window is discontinuous (${window_status:-missing}); see ${output_dir}/claim-statement-window.txt" >&2
    return 1
  fi
  if [[ "${window_requested_seconds}" != "${stats_window_seconds}" ]]; then
    echo "[FAIL] claim statement window duration does not match the requested interval; see ${output_dir}/claim-statement-window.txt" >&2
    return 1
  fi
  local stale_hint=""
  if (( alarm_claim_count == 0 && unmatched_alarm_count > 0 )); then
    stale_hint+="; alarm_dispatch ran ${unmatched_alarm_count} unmatched UPDATE ... SKIP LOCKED statement(s)"
  fi
  if (( youtube_claim_count == 0 && unmatched_youtube_count > 0 )); then
    stale_hint+="; youtube_outbox ran ${unmatched_youtube_count} unmatched UPDATE ... SKIP LOCKED statement(s)"
  fi
  if [[ -n "${stale_hint}" ]]; then
    stale_hint+=" (if one is the current claim SQL, the claim fingerprint is stale)"
  fi
  if [[ "${claim_count}" -eq 0 ]]; then
    echo "[FAIL] no fresh claim executions observed in the bounded stats window${stale_hint}; see ${output_dir}/claim-statement-window.txt" >&2
    return 1
  fi
  if [[ "${alarm_claim_count}" -eq 0 || "${youtube_claim_count}" -eq 0 ]]; then
    echo "[FAIL] one or more required hot paths have no fresh claim execution${stale_hint}; see ${output_dir}/claim-statement-window.txt" >&2
    return 1
  fi
  if [[ "${slow_claim_count}" -ne 0 ]]; then
    echo "[FAIL] claim statements exceed 5ms delta mean execution time; see ${output_dir}/claim-statement-window.txt" >&2
    return 1
  fi
}

validate_claim_statement_window

rows_removed_exceeds_limit() {
  local plan_file="$1"
  awk '
    /Rows Removed by Filter:/ {
      value = $0
      sub(/^.*Rows Removed by Filter:[[:space:]]*/, "", value)
      if ((value + 0) > 1000) {
        exceeded = 1
      }
    }
    END { exit exceeded ? 0 : 1 }
  ' "${plan_file}"
}

if [[ "${analyze}" == true ]]; then
  for plan_file in \
    "${output_dir}/alarm-dispatch-claim-explain.txt" \
    "${output_dir}/youtube-outbox-claim-explain.txt"; do
    if rows_removed_exceeds_limit "${plan_file}"; then
      echo "[FAIL] Rows Removed by Filter exceeds 1000; see ${plan_file}" >&2
      exit 1
    fi
  done
else
  echo "[SKIP] Rows Removed by Filter needs --analyze on a local fixture DB"
fi

if [[ "${index_check}" == "true" ]]; then
  echo "[PASS] required claim indexes are ready, valid, and structurally correct"
fi
