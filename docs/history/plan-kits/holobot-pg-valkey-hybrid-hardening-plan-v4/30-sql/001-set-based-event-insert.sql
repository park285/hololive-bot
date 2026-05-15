-- Set-based event insert template.
-- 목적: event별 row-by-row INSERT를 제거합니다.
-- 주의: 실제 pgx 타입 바인딩은 구현 시 조정하십시오.

WITH input AS (
    SELECT *
    FROM unnest(
        $1::text[],       -- event_key
        $2::char(64)[],   -- payload_hash
        $3::alarm_type[], -- alarm_type
        $4::varchar(64)[],-- channel_id
        $5::varchar(64)[],-- stream_id
        $6::text[],       -- category
        $7::jsonb[]       -- payload
    ) AS t(event_key, payload_hash, alarm_type, channel_id, stream_id, category, payload)
), conflict AS (
    SELECT e.event_key
    FROM alarm_dispatch_events e
    JOIN input i ON i.event_key = e.event_key
    WHERE e.payload_hash <> i.payload_hash
    LIMIT 1
), inserted AS (
    INSERT INTO alarm_dispatch_events (
        event_key,
        payload_hash,
        alarm_type,
        channel_id,
        stream_id,
        category,
        payload_schema_version,
        payload
    )
    SELECT
        i.event_key,
        i.payload_hash,
        i.alarm_type,
        i.channel_id,
        i.stream_id,
        i.category,
        1,
        i.payload
    FROM input i
    WHERE NOT EXISTS (SELECT 1 FROM conflict)
    ON CONFLICT (event_key) DO NOTHING
    RETURNING id, event_key, TRUE AS inserted
), all_events AS (
    SELECT id, event_key, inserted FROM inserted
    UNION ALL
    SELECT e.id, e.event_key, FALSE AS inserted
    FROM alarm_dispatch_events e
    JOIN input i ON i.event_key = e.event_key
    WHERE NOT EXISTS (
        SELECT 1 FROM inserted ins WHERE ins.event_key = e.event_key
    )
)
SELECT id, event_key, inserted
FROM all_events;

-- 구현 주의:
-- 1. conflict CTE가 row를 반환하면 Go 코드에서 명시적 error로 처리하십시오.
-- 2. 같은 input 안에 event_key 중복이 있으면 Go 전처리에서 hash conflict를 먼저 잡으십시오.
-- 3. payload 원문을 error/log에 남기지 마십시오.
