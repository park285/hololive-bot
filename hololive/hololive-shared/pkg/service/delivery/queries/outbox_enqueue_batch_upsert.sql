WITH raw_input AS (
    SELECT
        item.kind,
        item.period_key,
        item.room_id,
        item.content_id,
        item.payload,
        source.ordinality
    FROM jsonb_array_elements($1::jsonb) WITH ORDINALITY AS source(value, ordinality)
    CROSS JOIN LATERAL jsonb_to_record(source.value) AS item(
        kind TEXT,
        period_key TEXT,
        room_id TEXT,
        content_id TEXT,
        payload JSONB
    )
), input AS (
    -- INSERT ... ON CONFLICT DO UPDATE cannot affect the same target row twice
    -- in one statement. Keep one row per conflict key and make duplicate input
    -- deterministic: the last occurrence in the caller-provided JSON array wins.
    SELECT DISTINCT ON (kind, content_id)
        kind,
        period_key,
        room_id,
        content_id,
        payload
    FROM raw_input
    ORDER BY kind, content_id, ordinality DESC
)
INSERT INTO notification_delivery_outbox (
    kind,
    period_key,
    room_id,
    content_id,
    payload,
    status,
    attempt_count,
    next_attempt_at
)
SELECT
    kind,
    period_key,
    room_id,
    content_id,
    payload,
    'PENDING',
    0,
    NOW()
FROM input
ON CONFLICT (kind, content_id) DO UPDATE
SET period_key = EXCLUDED.period_key,
    room_id = EXCLUDED.room_id,
    payload = EXCLUDED.payload,
    status = 'PENDING',
    attempt_count = 0,
    next_attempt_at = NOW(),
    locked_at = NULL,
    locked_by = NULL,
    lock_expires_at = NULL,
    sending_started_at = NULL,
    error = NULL
WHERE notification_delivery_outbox.status = 'FAILED'
