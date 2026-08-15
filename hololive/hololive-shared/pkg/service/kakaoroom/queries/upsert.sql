INSERT INTO kakao_rooms (room_id, room_type, room_link_id)
VALUES ($1, $2, $3)
ON CONFLICT (room_id) DO UPDATE
SET room_type = EXCLUDED.room_type,
    room_link_id = EXCLUDED.room_link_id,
    updated_at = now()
WHERE (kakao_rooms.room_type, kakao_rooms.room_link_id)
    IS DISTINCT FROM (EXCLUDED.room_type, EXCLUDED.room_link_id)
