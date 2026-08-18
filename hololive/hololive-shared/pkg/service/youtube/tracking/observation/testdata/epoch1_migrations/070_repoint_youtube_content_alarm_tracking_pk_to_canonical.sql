-- 070: repoint youtube_content_alarm_tracking PK to (kind, canonical_content_id) so it
-- matches the upsert arbiter and stops the 23505 PK leak (forward-only, idempotent).
WITH base_rows AS (
    SELECT
        t.ctid,
        t.kind,
        t.canonical_content_id,
        t.content_id,
        t.alarm_sent_at,
        t.detected_at,
        t.created_at,
        EXISTS (
            SELECT 1
            FROM youtube_notification_outbox AS o
            WHERE o.kind = t.kind
              AND o.content_id = t.content_id
        ) AS has_matching_outbox
    FROM youtube_content_alarm_tracking AS t
    WHERE t.canonical_content_id IS NOT NULL
), ranked_rows AS (
    SELECT
        b.ctid,
        ROW_NUMBER() OVER (
            PARTITION BY b.kind, b.canonical_content_id
            ORDER BY
                CASE WHEN b.content_id = b.canonical_content_id THEN 0 ELSE 1 END,
                CASE WHEN b.has_matching_outbox THEN 0 ELSE 1 END,
                CASE WHEN b.alarm_sent_at IS NULL THEN 1 ELSE 0 END,
                b.detected_at ASC,
                b.created_at ASC,
                b.content_id ASC
        ) AS survivor_rank
    FROM base_rows AS b
)
DELETE FROM youtube_content_alarm_tracking AS t
USING ranked_rows AS r
WHERE t.ctid = r.ctid
  AND r.survivor_rank > 1;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE table_name = 'youtube_content_alarm_tracking'
          AND constraint_type = 'PRIMARY KEY'
          AND constraint_name = 'youtube_content_alarm_tracking_pkey'
    ) AND NOT EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE t.relname = 'youtube_content_alarm_tracking'
          AND c.contype = 'p'
          AND c.conkey = ARRAY[
              (SELECT attnum FROM pg_attribute WHERE attrelid = t.oid AND attname = 'kind'),
              (SELECT attnum FROM pg_attribute WHERE attrelid = t.oid AND attname = 'canonical_content_id')
          ]::smallint[]
    ) THEN
        ALTER TABLE youtube_content_alarm_tracking
            DROP CONSTRAINT youtube_content_alarm_tracking_pkey;
        ALTER TABLE youtube_content_alarm_tracking
            ADD CONSTRAINT youtube_content_alarm_tracking_pkey
            PRIMARY KEY (kind, canonical_content_id);
    END IF;
END
$$;

DROP INDEX IF EXISTS idx_ycat_kind_canonical_content;
