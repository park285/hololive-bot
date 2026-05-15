-- Set-based delivery insert template.
-- 목적: delivery별 row-by-row INSERT를 제거합니다.

WITH input AS (
    SELECT *
    FROM unnest(
        $1::text[],        -- event_key
        $2::varchar(64)[], -- room_id
        $3::text[],        -- dedupe_key
        $4::text[][],      -- claim_keys
        $5::jsonb[],       -- delivery_context
        $6::text[]         -- status
    ) AS t(event_key, room_id, dedupe_key, claim_keys, delivery_context, status)
), event_ids AS (
    SELECT e.id, e.event_key
    FROM alarm_dispatch_events e
    WHERE e.event_key = ANY($1::text[])
), inserted AS (
    INSERT INTO alarm_dispatch_deliveries (
        event_id,
        room_id,
        dedupe_key,
        claim_keys,
        delivery_context,
        status,
        next_attempt_at
    )
    SELECT
        e.id,
        i.room_id,
        i.dedupe_key,
        i.claim_keys,
        i.delivery_context,
        i.status,
        NOW()
    FROM input i
    JOIN event_ids e ON e.event_key = i.event_key
    ON CONFLICT (dedupe_key) DO NOTHING
    RETURNING dedupe_key
)
SELECT
    (SELECT count(*) FROM input) AS requested_deliveries,
    (SELECT count(*) FROM inserted) AS inserted_deliveries;

-- 선택적 bounded duplicate classification:
-- input dedupe_key 범위 안에서만 조회하므로 batch size에 의해 bounded입니다.
SELECT status, count(*)
FROM alarm_dispatch_deliveries
WHERE dedupe_key = ANY($1::text[])
GROUP BY status;
