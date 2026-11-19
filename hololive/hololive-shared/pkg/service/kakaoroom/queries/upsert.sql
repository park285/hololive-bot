WITH incoming (room_id, room_type, room_link_id) AS (
    VALUES ($1::text, $2::text, $3::text)
)
INSERT INTO kakao_rooms (room_id, room_type, room_link_id)
SELECT incoming.room_id, incoming.room_type, incoming.room_link_id
FROM incoming
WHERE NOT EXISTS (
    SELECT 1
    FROM kakao_rooms AS stored
    WHERE stored.room_id = incoming.room_id
      AND stored.room_type = incoming.room_type
      AND stored.room_link_id = incoming.room_link_id
)
ON CONFLICT (room_id) DO UPDATE
SET room_type = EXCLUDED.room_type,
    room_link_id = EXCLUDED.room_link_id,
    updated_at = now()
WHERE (kakao_rooms.room_type, kakao_rooms.room_link_id)
    IS DISTINCT FROM (EXCLUDED.room_type, EXCLUDED.room_link_id)
