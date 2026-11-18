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
