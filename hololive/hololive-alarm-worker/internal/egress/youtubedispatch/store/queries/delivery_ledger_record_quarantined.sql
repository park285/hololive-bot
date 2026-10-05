WITH input AS (
    SELECT kind, logical_id, room_id, observed_at, source_delivery_id
    FROM unnest(
        $1::text[],
        $2::text[],
        $3::text[],
        $4::timestamptz[],
        $5::bigint[]
    ) AS values(kind, logical_id, room_id, observed_at, source_delivery_id)
)
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
    SET first_recorded_at = LEAST(current.first_recorded_at, EXCLUDED.first_recorded_at),
        updated_at = GREATEST(current.updated_at, EXCLUDED.updated_at),
        quarantined_at = LEAST(current.quarantined_at, EXCLUDED.quarantined_at),
        source_delivery_id = COALESCE(current.source_delivery_id, EXCLUDED.source_delivery_id)
    -- SENT는 우선하는 확정 증거이므로 늦은 quarantine으로 다시 쓰지 않는다.
    WHERE current.status = 'QUARANTINED'
      AND (EXCLUDED.first_recorded_at < current.first_recorded_at
           OR EXCLUDED.updated_at > current.updated_at
           OR EXCLUDED.quarantined_at < current.quarantined_at
           OR current.source_delivery_id IS NULL);
