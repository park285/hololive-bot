WITH input AS (
    SELECT kind, logical_id, room_id, observed_at, source_delivery_id
    FROM unnest(
        $1::text[],
        $2::text[],
        $3::text[],
        $4::timestamptz[],
        $5::bigint[]
    ) AS values(kind, logical_id, room_id, observed_at, source_delivery_id)
), recorded AS (
    INSERT INTO youtube_notification_delivery_ledger AS current (
        kind,
        logical_id,
        room_id,
        status,
        first_recorded_at,
        updated_at,
        sent_at,
        quarantined_at,
        source_delivery_id
    )
    SELECT
        kind,
        logical_id,
        room_id,
        'SENT',
        observed_at,
        observed_at,
        observed_at,
        NULL,
        source_delivery_id
    FROM input
    ON CONFLICT (kind, logical_id, room_id) DO UPDATE
    SET status = 'SENT',
        first_recorded_at = LEAST(current.first_recorded_at, EXCLUDED.first_recorded_at),
        updated_at = GREATEST(current.updated_at, EXCLUDED.updated_at),
        sent_at = CASE
            WHEN current.status = 'SENT' THEN LEAST(current.sent_at, EXCLUDED.sent_at)
            ELSE EXCLUDED.sent_at
        END,
        quarantined_at = current.quarantined_at,
        source_delivery_id = CASE
            WHEN current.status = 'QUARANTINED' THEN EXCLUDED.source_delivery_id
            WHEN EXCLUDED.sent_at < current.sent_at THEN EXCLUDED.source_delivery_id
            ELSE COALESCE(current.source_delivery_id, EXCLUDED.source_delivery_id)
        END
    RETURNING kind, logical_id, room_id, status, first_recorded_at, updated_at,
              sent_at, quarantined_at, source_delivery_id
)
SELECT kind, logical_id, room_id, status, first_recorded_at, updated_at,
       sent_at, quarantined_at, source_delivery_id
FROM recorded
ORDER BY kind, logical_id, room_id;
