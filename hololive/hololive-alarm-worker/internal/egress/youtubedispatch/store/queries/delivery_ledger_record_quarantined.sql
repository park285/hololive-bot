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
        'QUARANTINED',
        observed_at,
        observed_at,
        NULL,
        observed_at,
        source_delivery_id
    FROM input
    ON CONFLICT (kind, logical_id, room_id) DO UPDATE
    SET status = current.status,
        first_recorded_at = CASE
            WHEN current.status = 'QUARANTINED'
                THEN LEAST(current.first_recorded_at, EXCLUDED.first_recorded_at)
            ELSE current.first_recorded_at
        END,
        updated_at = CASE
            WHEN current.status = 'QUARANTINED'
                THEN GREATEST(current.updated_at, EXCLUDED.updated_at)
            ELSE current.updated_at
        END,
        sent_at = current.sent_at,
        quarantined_at = CASE
            WHEN current.status = 'QUARANTINED'
                THEN LEAST(current.quarantined_at, EXCLUDED.quarantined_at)
            ELSE current.quarantined_at
        END,
        source_delivery_id = CASE
            WHEN current.status = 'QUARANTINED'
                THEN COALESCE(current.source_delivery_id, EXCLUDED.source_delivery_id)
            ELSE current.source_delivery_id
        END
    RETURNING kind, logical_id, room_id, status, first_recorded_at, updated_at,
              sent_at, quarantined_at, source_delivery_id
)
SELECT kind, logical_id, room_id, status, first_recorded_at, updated_at,
       sent_at, quarantined_at, source_delivery_id
FROM recorded
ORDER BY kind, logical_id, room_id;
