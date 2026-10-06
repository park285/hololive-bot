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
    -- 한 INSERT의 ON CONFLICT DO UPDATE는 같은 행을 두 번 갱신할 수 없다.
    -- 충돌 키마다 한 행만 남기며 입력 JSON 배열의 마지막 항목을 선택한다.
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
    room_id = CASE WHEN notification_delivery_outbox.payload->'request' IS NOT NULL
                   THEN notification_delivery_outbox.room_id ELSE EXCLUDED.room_id END,
    payload = CASE WHEN notification_delivery_outbox.payload->'request' IS NOT NULL
                   THEN notification_delivery_outbox.payload ELSE EXCLUDED.payload END,
    status = 'PENDING',
    attempt_count = 0,
    next_attempt_at = NOW(),
    locked_at = NULL,
    locked_by = NULL,
    lock_expires_at = NULL,
    sending_started_at = NULL,
    error = NULL
WHERE notification_delivery_outbox.status = 'FAILED'
  AND COALESCE(notification_delivery_outbox.payload->'request'->>'exhausted', 'false') <> 'true'
  AND (notification_delivery_outbox.payload->'request' IS NOT NULL
       OR notification_delivery_outbox.attempt_count = 0
       OR notification_delivery_outbox.payload->>'known_unsent' = 'true')
