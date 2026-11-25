INSERT INTO kakao_room_delivery_pauses (room_id, reason, paused_at)
SELECT DISTINCT target.room_id, 'room_not_joined', clock_timestamp()
FROM unnest($1::text[]) AS target(room_id)
ON CONFLICT (room_id) DO NOTHING
