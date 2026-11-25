DELETE FROM kakao_room_delivery_pauses
WHERE room_id = $1
RETURNING room_id
