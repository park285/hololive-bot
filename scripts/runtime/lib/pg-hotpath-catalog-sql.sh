#!/usr/bin/env bash

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  echo "pg-hotpath-catalog-sql.sh must be sourced by pg-hotpath-explain-snapshot.sh" >&2
  exit 2
fi

invalid_indexes_sql() {
  cat <<'SQL'
SELECT
    n.nspname AS schema_name,
    c.relname AS index_name,
    i.indisready,
    i.indisvalid
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_index i ON i.indexrelid = c.oid
WHERE NOT i.indisvalid OR NOT i.indisready
ORDER BY n.nspname, c.relname;
SQL
}

target_indexes_sql() {
  cat <<'SQL'
WITH required(index_name) AS (
    VALUES
        ('idx_alarm_dispatch_deliveries_due'),
        ('idx_alarm_dispatch_deliveries_send_unit'),
        ('idx_alarm_dispatch_deliveries_send_unit_due'),
        ('idx_yno_pending_due_created_id')
),
observed AS (
    SELECT
        index_class.relname AS index_name,
        table_namespace.nspname AS table_schema,
        table_class.relname AS table_name,
        access_method.amname AS access_method,
        index_meta.indisready,
        index_meta.indisvalid,
        index_meta.indisunique,
        index_meta.indisprimary,
        index_meta.indisexclusion,
        index_meta.indnkeyatts,
        index_meta.indnatts,
        ARRAY(
            SELECT pg_get_indexdef(index_meta.indexrelid, key_position, true)
            FROM generate_series(1, index_meta.indnkeyatts::integer) AS key_position
            ORDER BY key_position
        ) AS key_definitions,
        pg_get_expr(index_meta.indpred, index_meta.indrelid) AS predicate
    FROM pg_class index_class
    JOIN pg_namespace index_namespace ON index_namespace.oid = index_class.relnamespace
    JOIN pg_index index_meta ON index_meta.indexrelid = index_class.oid
    JOIN pg_class table_class ON table_class.oid = index_meta.indrelid
    JOIN pg_namespace table_namespace ON table_namespace.oid = table_class.relnamespace
    JOIN pg_am access_method ON access_method.oid = index_class.relam
    WHERE index_namespace.nspname = 'public'
      AND index_class.relname IN (
        'idx_alarm_dispatch_deliveries_due',
        'idx_alarm_dispatch_deliveries_send_unit',
        'idx_alarm_dispatch_deliveries_send_unit_due',
        'idx_yno_pending_due_created_id'
      )
),
checked AS (
    SELECT
        required.index_name,
        COALESCE(observed.indisready, false) AS indisready,
        COALESCE(observed.indisvalid, false) AS indisvalid,
        COALESCE(
            observed.table_schema = 'public'
            AND observed.access_method = 'btree'
            AND observed.indnatts = observed.indnkeyatts
            AND NOT observed.indisunique
            AND NOT observed.indisprimary
            AND NOT observed.indisexclusion
            AND CASE required.index_name
                WHEN 'idx_alarm_dispatch_deliveries_due' THEN
                    observed.table_name = 'alarm_dispatch_deliveries'
                    AND observed.indnkeyatts = 2
                    AND observed.key_definitions = ARRAY['next_attempt_at', 'id']::text[]
                    AND observed.predicate = '(status = ANY (ARRAY[''pending''::text, ''retry''::text]))'
                WHEN 'idx_alarm_dispatch_deliveries_send_unit' THEN
                    observed.table_name = 'alarm_dispatch_deliveries'
                    AND observed.indnkeyatts = 1
                    AND observed.key_definitions = ARRAY['send_unit_id']::text[]
                    AND observed.predicate = '(send_unit_id IS NOT NULL)'
                WHEN 'idx_alarm_dispatch_deliveries_send_unit_due' THEN
                    observed.table_name = 'alarm_dispatch_deliveries'
                    AND observed.indnkeyatts = 3
                    AND observed.key_definitions = ARRAY['send_unit_id', 'next_attempt_at', 'id']::text[]
                    AND observed.predicate = '((send_unit_id IS NOT NULL) AND (status = ANY (ARRAY[''pending''::text, ''retry''::text])))'
                WHEN 'idx_yno_pending_due_created_id' THEN
                    observed.table_name = 'youtube_notification_outbox'
                    AND observed.indnkeyatts = 3
                    AND observed.key_definitions = ARRAY['next_attempt_at', 'created_at', 'id']::text[]
                    AND observed.predicate = '(status = ''PENDING''::text)'
                ELSE false
            END,
            false
        ) AS definition_ok
    FROM required
    LEFT JOIN observed ON observed.index_name = required.index_name
)
SELECT index_name, indisready, indisvalid, definition_ok
FROM checked
ORDER BY index_name;
SQL
}

mvcc_database_state_sql() {
  cat <<'SQL'
SELECT
    database_catalog.datname AS database_name,
    current_user AS session_user,
    current_setting('idle_in_transaction_session_timeout') AS idle_in_transaction_session_timeout,
    current_setting('transaction_timeout') AS transaction_timeout,
    current_setting('statement_timeout') AS statement_timeout,
    age(database_catalog.datfrozenxid) AS frozen_xid_age,
    mxid_age(database_catalog.datminmxid) AS frozen_multixact_age,
    current_setting('autovacuum_freeze_max_age')::bigint AS autovacuum_freeze_max_age,
    database_stats.stats_reset
FROM pg_database AS database_catalog
JOIN pg_stat_database AS database_stats
  ON database_stats.datid = database_catalog.oid
WHERE database_catalog.datname = current_database();
SQL
}

dead_tuples_sql() {
  cat <<'SQL'
SELECT
    relname,
    pg_size_pretty(pg_total_relation_size(relid)) AS total_size,
    n_live_tup,
    n_dead_tup,
    ROUND(
        100.0 * n_dead_tup::numeric
        / NULLIF(n_live_tup + n_dead_tup, 0),
        2
    ) AS dead_tuple_pct,
    n_tup_ins,
    n_tup_upd,
    n_tup_hot_upd,
    ROUND(
        100.0 * n_tup_hot_upd::numeric
        / NULLIF(n_tup_upd, 0),
        2
    ) AS hot_update_pct,
    n_tup_del,
    vacuum_count,
    autovacuum_count,
    analyze_count,
    autoanalyze_count,
    last_vacuum,
    last_autovacuum,
    last_analyze,
    last_autoanalyze
FROM pg_stat_user_tables
WHERE relname IN (
    'alarm_dispatch_deliveries',
    'alarm_dispatch_send_units',
    'notification_delivery_outbox',
    'youtube_notification_outbox',
    'youtube_notification_delivery',
    'youtube_notification_delivery_telemetry',
    'youtube_community_shorts_alarm_states',
    'source_observation_queue',
    'source_collection_checkpoints',
    'youtube_collection_job_leases',
    'source_observations'
)
ORDER BY dead_tuple_pct DESC NULLS LAST, n_dead_tup DESC, relname;
SQL
}

mvcc_index_activity_sql() {
  cat <<'SQL'
WITH database_stats AS (
    SELECT stats_reset
    FROM pg_stat_database
    WHERE datname = current_database()
)
SELECT
    clock_timestamp() AS captured_at,
    database_stats.stats_reset,
    index_stats.relname AS table_name,
    index_stats.indexrelname AS index_name,
    pg_size_pretty(pg_relation_size(index_stats.indexrelid)) AS index_size,
    index_stats.idx_scan,
    index_stats.idx_tup_read,
    index_stats.idx_tup_fetch
FROM pg_stat_user_indexes AS index_stats
CROSS JOIN database_stats
WHERE index_stats.relname IN (
    'source_observation_queue',
    'source_collection_checkpoints',
    'youtube_collection_job_leases',
    'source_observations'
)
ORDER BY index_stats.relname, index_stats.idx_scan, index_stats.indexrelname;
SQL
}

idle_transactions_sql() {
  cat <<'SQL'
SELECT
    pid,
    usename,
    application_name,
    client_addr,
    state,
    xact_start,
    state_change,
    clock_timestamp() - xact_start AS transaction_age,
    clock_timestamp() - state_change AS idle_age,
    backend_xmin::text AS backend_xmin,
    CASE
        WHEN backend_xmin IS NULL THEN NULL
        ELSE age(backend_xmin)
    END AS backend_xmin_age,
    wait_event_type,
    wait_event,
    LEFT(query, 500) AS query
FROM pg_stat_activity
WHERE datname = current_database()
  AND state IN ('idle in transaction', 'idle in transaction (aborted)')
ORDER BY xact_start, pid;
SQL
}

alarm_claim_sql() {
  cat <<'SQL'
-- expected-index: idx_alarm_dispatch_deliveries_due
-- expected-index: idx_alarm_dispatch_deliveries_send_unit_due
BEGIN;
SET LOCAL statement_timeout = '5s';
EXPLAIN (ANALYZE, BUFFERS)
WITH legacy_head AS (
    SELECT d.id
    FROM alarm_dispatch_deliveries d
    WHERE d.send_unit_id IS NULL
      AND d.status IN ('pending', 'retry')
      AND d.next_attempt_at <= NOW()
    ORDER BY d.next_attempt_at ASC, d.id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
), due_window AS MATERIALIZED (
    SELECT d.send_unit_id, d.next_attempt_at, d.id AS delivery_id
    FROM alarm_dispatch_deliveries d
    WHERE d.send_unit_id IS NOT NULL
      AND d.status IN ('pending', 'retry')
      AND d.next_attempt_at <= NOW()
      AND NOT EXISTS (SELECT 1 FROM legacy_head)
    ORDER BY d.next_attempt_at ASC, d.id ASC
    LIMIT 500
), unit_candidates AS (
    SELECT DISTINCT ON (send_unit_id)
        send_unit_id AS id, next_attempt_at, delivery_id
    FROM due_window
    ORDER BY send_unit_id, next_attempt_at ASC, delivery_id ASC
), locked_units AS (
    SELECT u.id, candidate.next_attempt_at, candidate.delivery_id,
        (
            SELECT count(*)
            FROM alarm_dispatch_deliveries due
            WHERE due.send_unit_id = u.id
              AND due.status IN ('pending', 'retry')
              AND due.next_attempt_at <= NOW()
        ) AS delivery_count
    FROM unit_candidates candidate
    JOIN alarm_dispatch_send_units u ON u.id = candidate.id
    ORDER BY candidate.next_attempt_at ASC, candidate.delivery_id ASC
    LIMIT 50
    FOR UPDATE OF u SKIP LOCKED
), ranked_units AS (
    SELECT id,
        row_number() OVER (ORDER BY next_attempt_at ASC, delivery_id ASC) AS ordinal,
        sum(delivery_count) OVER (ORDER BY next_attempt_at ASC, delivery_id ASC ROWS UNBOUNDED PRECEDING) AS cumulative_deliveries
    FROM locked_units
), next_units AS (
    SELECT id
    FROM ranked_units
    WHERE cumulative_deliveries <= 50 OR ordinal = 1
), picked AS (
    SELECT d.id
    FROM alarm_dispatch_deliveries d
    WHERE d.send_unit_id IN (SELECT id FROM next_units)
      AND d.status IN ('pending', 'retry')
      AND d.next_attempt_at <= NOW()
    UNION ALL
    SELECT id FROM legacy_head
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'leased',
        locked_by = 'pg-hotpath-explain',
        locked_at = NOW(),
        lock_expires_at = NOW() + INTERVAL '60 seconds',
        updated_at = NOW()
    FROM picked
    WHERE d.id = picked.id
    RETURNING d.id, d.event_id, d.room_id, d.dedupe_key, d.claim_keys, d.delivery_context,
        d.dispatch_group_key, d.send_unit_id, d.status, d.attempt_count, d.next_attempt_at,
        d.locked_by, d.locked_at, d.lock_expires_at, d.sending_started_at, d.sent_at,
        d.dlq_at, d.quarantined_at, d.cancelled_at, d.last_error_code, d.last_error,
        d.created_at, d.updated_at
)
SELECT d.id, d.event_id, d.room_id, d.dedupe_key, d.claim_keys, d.delivery_context,
    d.dispatch_group_key, d.send_unit_id, COALESCE(u.client_request_id, ''), d.status,
    d.attempt_count, d.next_attempt_at, d.locked_by, d.locked_at, d.lock_expires_at,
    d.sending_started_at, d.sent_at, d.dlq_at, d.quarantined_at, d.cancelled_at,
    d.last_error_code, d.last_error, d.created_at, d.updated_at
FROM updated d
LEFT JOIN alarm_dispatch_send_units u ON u.id = d.send_unit_id
ORDER BY d.next_attempt_at ASC, d.id ASC;
ROLLBACK;
SQL
}

youtube_outbox_claim_sql() {
  cat <<'SQL'
-- expected-index: idx_yno_pending_due_created_id
BEGIN;
SET LOCAL statement_timeout = '5s';
EXPLAIN (ANALYZE, BUFFERS)
WITH claim AS (
    SELECT o.id
    FROM youtube_notification_outbox o
    WHERE o.status = 'PENDING'
      AND (o.locked_at IS NULL OR o.locked_at < NOW() - INTERVAL '5 minutes')
      AND o.next_attempt_at <= NOW()
      AND o.created_at >= NOW() - INTERVAL '2 hours'
      AND NOT EXISTS (
        SELECT 1
        FROM youtube_notification_delivery d
        WHERE d.outbox_id = o.id
      )
    ORDER BY o.next_attempt_at ASC, o.created_at ASC, o.id ASC
    LIMIT 50
    FOR UPDATE SKIP LOCKED
)
SELECT id
FROM claim
ORDER BY id;
ROLLBACK;
SQL
}
