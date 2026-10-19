-- 새 delivery의 첫 시도 시각은 한 번 채취한 DB 시계(clock)라 모든 자식이 같은 값을 받는다.
-- $1 outbox id, $2 PENDING, $3 대상 room id 배열.
WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS at
)
INSERT INTO youtube_notification_delivery (
    outbox_id,
    room_id,
    status,
    attempt_count,
    next_attempt_at
)
SELECT $1, room_id, $2, 0, (SELECT at FROM clock)
FROM unnest($3::text[]) AS target(room_id)
ON CONFLICT (outbox_id, room_id) DO NOTHING;
