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

dead_tuples_sql() {
  cat <<'SQL'
SELECT
    relname,
    n_live_tup,
    n_dead_tup,
    autovacuum_count,
    autoanalyze_count,
    last_autovacuum,
    last_autoanalyze
FROM pg_stat_user_tables
WHERE relname IN (
    'alarm_dispatch_deliveries',
    'youtube_notification_outbox',
    'youtube_notification_delivery'
)
ORDER BY n_dead_tup DESC, relname;
SQL
}

alarm_claim_sql() {
  cat <<'SQL'
-- expected-index: idx_alarm_dispatch_deliveries_due
BEGIN;
SET LOCAL statement_timeout = '5s';
EXPLAIN (ANALYZE, BUFFERS)
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status IN ('pending', 'retry')
      AND next_attempt_at <= NOW()
    ORDER BY next_attempt_at ASC, id ASC
    LIMIT 50
    FOR UPDATE SKIP LOCKED
)
SELECT id
FROM picked
ORDER BY id;
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
