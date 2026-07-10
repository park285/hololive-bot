#!/usr/bin/env bash

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  echo "pg-hotpath-claim-window.sh must be sourced by pg-hotpath-explain-snapshot.sh" >&2
  exit 2
fi

claim_statement_window_sql() {
  cat <<'SQL'
/* hololive-pg-hotpath-stats-observer */
WITH start_info AS MATERIALIZED (
    SELECT
        clock_timestamp() AS collected_at,
        stats_reset,
        dealloc
    FROM pg_stat_statements_info
),
start_claims AS MATERIALIZED (
    SELECT
        statements.dbid,
        statements.userid,
        statements.queryid,
        statements.toplevel,
        statements.stats_since,
        statements.calls,
        statements.total_exec_time,
        statements.query,
        CASE
            WHEN fingerprint.is_alarm THEN 'alarm_dispatch'
            ELSE 'youtube_outbox'
        END AS claim_target
    FROM start_info
    CROSS JOIN pg_stat_statements statements
    CROSS JOIN LATERAL (
        SELECT
            statements.query ~* 'WITH[[:space:]]+picked[[:space:]]+AS[[:space:]]*[(]'
            AND statements.query ~* '[)][[:space:]]*,[[:space:]]*updated[[:space:]]+AS[[:space:]]*[(]'
            AND statements.query ~* 'UPDATE[[:space:]]+alarm_dispatch_deliveries[[:space:]]+d[[:space:]]+SET'
            AND statements.query ~* 'lock_expires_at[[:space:]]*=[[:space:]]*NOW[[:space:]]*[(][)][[:space:]]*[+]'
            AND statements.query ~* 'RETURNING[[:space:]]+d[.]id[[:space:]]*,[[:space:]]*d[.]event_id'
            AND statements.query ~* 'd[.]claim_keys'
            AND statements.query ~* 'd[.]delivery_context'
            AND statements.query ~* 'FROM[[:space:]]+updated' AS is_alarm,
            statements.query ~* 'WITH[[:space:]]+claim[[:space:]]+AS[[:space:]]*[(]'
            AND statements.query ~* '[)][[:space:]]*,[[:space:]]*updated[[:space:]]+AS[[:space:]]*[(]'
            AND statements.query ~* 'UPDATE[[:space:]]+youtube_notification_outbox[[:space:]]+o[[:space:]]+SET[[:space:]]+locked_at'
            AND statements.query ~* 'FROM[[:space:]]+claim[[:space:]]+WHERE[[:space:]]+o[.]id[[:space:]]*=[[:space:]]*claim[.]id'
            AND statements.query ~* 'RETURNING[[:space:]]+o[.]id[[:space:]]*,[[:space:]]*o[.]kind[[:space:]]*,[[:space:]]*o[.]channel_id[[:space:]]*,[[:space:]]*o[.]content_id'
            AND statements.query ~* 'o[.]payload::text[[:space:]]+AS[[:space:]]+payload'
            AND statements.query ~* 'FROM[[:space:]]+updated' AS is_youtube
    ) AS fingerprint
    WHERE start_info.stats_reset IS NOT NULL
      AND statements.dbid = (
        SELECT oid
        FROM pg_database
        WHERE datname = current_database()
    )
      AND statements.queryid IS NOT NULL
      AND statements.stats_since IS NOT NULL
      AND statements.query ILIKE '%FOR UPDATE SKIP LOCKED%'
      AND statements.query NOT ILIKE '%EXPLAIN%FOR UPDATE SKIP LOCKED%'
      AND statements.query NOT ILIKE '%pg_stat_statements%'
      AND statements.query NOT LIKE '%hololive-pg-hotpath-stats-observer%'
      AND fingerprint.is_alarm <> fingerprint.is_youtube
),
wait_for_window AS MATERIALIZED (
    SELECT
        pg_sleep(
            :'stats_window_seconds'::double precision
            + start_claim_count.claim_count * 0
        ) AS slept,
        start_claim_count.claim_count
    FROM start_info
    CROSS JOIN (
        SELECT count(*) AS claim_count
        FROM start_claims
    ) AS start_claim_count
),
finish_claims AS MATERIALIZED (
    SELECT
        statements.dbid,
        statements.userid,
        statements.queryid,
        statements.toplevel,
        statements.stats_since,
        statements.calls,
        statements.total_exec_time,
        statements.query,
        CASE
            WHEN fingerprint.is_alarm THEN 'alarm_dispatch'
            ELSE 'youtube_outbox'
        END AS claim_target
    FROM wait_for_window
    CROSS JOIN pg_stat_statements statements
    CROSS JOIN LATERAL (
        SELECT
            statements.query ~* 'WITH[[:space:]]+picked[[:space:]]+AS[[:space:]]*[(]'
            AND statements.query ~* '[)][[:space:]]*,[[:space:]]*updated[[:space:]]+AS[[:space:]]*[(]'
            AND statements.query ~* 'UPDATE[[:space:]]+alarm_dispatch_deliveries[[:space:]]+d[[:space:]]+SET'
            AND statements.query ~* 'lock_expires_at[[:space:]]*=[[:space:]]*NOW[[:space:]]*[(][)][[:space:]]*[+]'
            AND statements.query ~* 'RETURNING[[:space:]]+d[.]id[[:space:]]*,[[:space:]]*d[.]event_id'
            AND statements.query ~* 'd[.]claim_keys'
            AND statements.query ~* 'd[.]delivery_context'
            AND statements.query ~* 'FROM[[:space:]]+updated' AS is_alarm,
            statements.query ~* 'WITH[[:space:]]+claim[[:space:]]+AS[[:space:]]*[(]'
            AND statements.query ~* '[)][[:space:]]*,[[:space:]]*updated[[:space:]]+AS[[:space:]]*[(]'
            AND statements.query ~* 'UPDATE[[:space:]]+youtube_notification_outbox[[:space:]]+o[[:space:]]+SET[[:space:]]+locked_at'
            AND statements.query ~* 'FROM[[:space:]]+claim[[:space:]]+WHERE[[:space:]]+o[.]id[[:space:]]*=[[:space:]]*claim[.]id'
            AND statements.query ~* 'RETURNING[[:space:]]+o[.]id[[:space:]]*,[[:space:]]*o[.]kind[[:space:]]*,[[:space:]]*o[.]channel_id[[:space:]]*,[[:space:]]*o[.]content_id'
            AND statements.query ~* 'o[.]payload::text[[:space:]]+AS[[:space:]]+payload'
            AND statements.query ~* 'FROM[[:space:]]+updated' AS is_youtube
    ) AS fingerprint
    WHERE wait_for_window.claim_count >= 0
      AND statements.dbid = (
        SELECT oid
        FROM pg_database
        WHERE datname = current_database()
    )
      AND statements.queryid IS NOT NULL
      AND statements.stats_since IS NOT NULL
      AND statements.query ILIKE '%FOR UPDATE SKIP LOCKED%'
      AND statements.query NOT ILIKE '%EXPLAIN%FOR UPDATE SKIP LOCKED%'
      AND statements.query NOT ILIKE '%pg_stat_statements%'
      AND statements.query NOT LIKE '%hololive-pg-hotpath-stats-observer%'
      AND fingerprint.is_alarm <> fingerprint.is_youtube
),
finish_info AS MATERIALIZED (
    SELECT
        clock_timestamp()
            + make_interval(secs => finish_claim_count.claim_count * 0) AS collected_at,
        info.stats_reset,
        info.dealloc + finish_claim_count.claim_count * 0 AS dealloc
    FROM pg_stat_statements_info info
    CROSS JOIN (
        SELECT count(*) AS claim_count
        FROM finish_claims
    ) AS finish_claim_count
),
paired_claims AS (
    SELECT
        COALESCE(finish.dbid, start.dbid) AS dbid,
        COALESCE(finish.userid, start.userid) AS userid,
        COALESCE(finish.queryid, start.queryid) AS queryid,
        COALESCE(finish.toplevel, start.toplevel) AS toplevel,
        start.stats_since AS start_stats_since,
        finish.stats_since AS finish_stats_since,
        start.claim_target AS start_claim_target,
        finish.claim_target AS finish_claim_target,
        COALESCE(finish.claim_target, start.claim_target) AS claim_target,
        start.calls AS start_calls,
        finish.calls AS finish_calls,
        start.total_exec_time AS start_total_exec_time,
        finish.total_exec_time AS finish_total_exec_time,
        finish.query,
        finish.calls - COALESCE(start.calls, 0) AS delta_calls,
        finish.total_exec_time - COALESCE(start.total_exec_time, 0) AS delta_total_exec_time
    FROM start_claims start
    FULL OUTER JOIN finish_claims finish
      ON finish.dbid = start.dbid
     AND finish.userid = start.userid
     AND finish.queryid = start.queryid
     AND finish.toplevel = start.toplevel
),
window_state AS (
    SELECT
        start_info.collected_at AS started_at,
        finish_info.collected_at AS finished_at,
        start_info.dealloc AS start_dealloc,
        finish_info.dealloc AS finish_dealloc,
        CASE
            WHEN finish_info.collected_at - start_info.collected_at
                < make_interval(secs => :'stats_window_seconds'::double precision) THEN 'short_window'
            WHEN start_info.stats_reset IS DISTINCT FROM finish_info.stats_reset THEN 'stats_reset'
            WHEN start_info.dealloc IS DISTINCT FROM finish_info.dealloc THEN 'entry_deallocated'
            WHEN EXISTS (
                SELECT 1
                FROM paired_claims
                WHERE start_stats_since IS NOT NULL
                  AND finish_stats_since IS NOT NULL
                  AND start_stats_since IS DISTINCT FROM finish_stats_since
            ) THEN 'statement_stats_reset'
            WHEN EXISTS (
                SELECT 1
                FROM paired_claims
                WHERE start_claim_target IS DISTINCT FROM finish_claim_target
                  AND start_claim_target IS NOT NULL
                  AND finish_claim_target IS NOT NULL
            ) THEN 'fingerprint_changed'
            WHEN EXISTS (
                SELECT 1
                FROM paired_claims
                WHERE finish_calls IS NULL
                   OR finish_calls < COALESCE(start_calls, 0)
                   OR finish_total_exec_time < COALESCE(start_total_exec_time, 0)
            ) THEN 'counter_discontinuity'
            ELSE 'ok'
        END AS status
    FROM start_info
    CROSS JOIN finish_info
),
output_rows AS (
    SELECT
        0 AS sort_order,
        'window'::text AS record_type,
        window_state.status::text AS status,
        window_state.started_at::text AS started_at,
        window_state.finished_at::text AS finished_at,
        :'stats_window_seconds'::text AS requested_seconds,
        window_state.start_dealloc::text AS start_dealloc,
        window_state.finish_dealloc::text AS finish_dealloc,
        ''::text AS claim_target,
        ''::text AS dbid,
        ''::text AS userid,
        ''::text AS queryid,
        ''::text AS toplevel,
        ''::text AS start_stats_since,
        ''::text AS finish_stats_since,
        ''::text AS delta_calls,
        ''::text AS delta_total_ms,
        ''::text AS delta_mean_ms,
        ''::text AS query
    FROM window_state

    UNION ALL

    SELECT
        1 AS sort_order,
        'claim'::text AS record_type,
        ''::text AS status,
        ''::text AS started_at,
        ''::text AS finished_at,
        ''::text AS requested_seconds,
        ''::text AS start_dealloc,
        ''::text AS finish_dealloc,
        paired_claims.claim_target::text,
        paired_claims.dbid::text,
        paired_claims.userid::text,
        paired_claims.queryid::text,
        paired_claims.toplevel::text,
        COALESCE(paired_claims.start_stats_since::text, '') AS start_stats_since,
        paired_claims.finish_stats_since::text,
        paired_claims.delta_calls::text,
        round(paired_claims.delta_total_exec_time::numeric, 3)::text AS delta_total_ms,
        round(
            (paired_claims.delta_total_exec_time / paired_claims.delta_calls)::numeric,
            3
        )::text AS delta_mean_ms,
        regexp_replace(paired_claims.query, E'[\n\r\t]+', ' ', 'g') AS query
    FROM paired_claims
    CROSS JOIN window_state
    WHERE window_state.status = 'ok'
      AND paired_claims.delta_calls > 0
)
SELECT
    record_type,
    status,
    started_at,
    finished_at,
    requested_seconds,
    start_dealloc,
    finish_dealloc,
    claim_target,
    dbid,
    userid,
    queryid,
    toplevel,
    start_stats_since,
    finish_stats_since,
    delta_calls,
    delta_total_ms,
    delta_mean_ms,
    query
FROM output_rows
ORDER BY sort_order, delta_mean_ms DESC NULLS LAST, queryid, toplevel;
SQL
}

query_matches_all_patterns() {
  local query="$1"
  shift
  local pattern

  for pattern in "$@"; do
    [[ "${query}" =~ ${pattern} ]] || return 1
  done
}

claim_query_matches_target() {
  local claim_target="$1"
  local normalized_query="${2,,}"

  case "${claim_target}" in
    alarm_dispatch)
      query_matches_all_patterns "${normalized_query}" \
        'with[[:space:]]+picked[[:space:]]+as[[:space:]]*[(]' \
        '[)][[:space:]]*,[[:space:]]*updated[[:space:]]+as[[:space:]]*[(]' \
        'update[[:space:]]+alarm_dispatch_deliveries[[:space:]]+d[[:space:]]+set' \
        'lock_expires_at[[:space:]]*=[[:space:]]*now[[:space:]]*[(][)][[:space:]]*[+]' \
        'returning[[:space:]]+d[.]id[[:space:]]*,[[:space:]]*d[.]event_id' \
        'd[.]claim_keys' \
        'd[.]delivery_context' \
        'from[[:space:]]+updated' \
        'for[[:space:]]+update[[:space:]]+skip[[:space:]]+locked'
      ;;
    youtube_outbox)
      query_matches_all_patterns "${normalized_query}" \
        'with[[:space:]]+claim[[:space:]]+as[[:space:]]*[(]' \
        '[)][[:space:]]*,[[:space:]]*updated[[:space:]]+as[[:space:]]*[(]' \
        'update[[:space:]]+youtube_notification_outbox[[:space:]]+o[[:space:]]+set[[:space:]]+locked_at' \
        'from[[:space:]]+claim[[:space:]]+where[[:space:]]+o[.]id[[:space:]]*=[[:space:]]*claim[.]id' \
        'returning[[:space:]]+o[.]id[[:space:]]*,[[:space:]]*o[.]kind[[:space:]]*,[[:space:]]*o[.]channel_id[[:space:]]*,[[:space:]]*o[.]content_id' \
        'o[.]payload::text[[:space:]]+as[[:space:]]+payload' \
        'from[[:space:]]+updated' \
        'for[[:space:]]+update[[:space:]]+skip[[:space:]]+locked'
      ;;
    *)
      return 1
      ;;
  esac
}
