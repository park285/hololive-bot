SELECT room_id
FROM kakao_room_delivery_pauses
WHERE room_id = ANY($1::text[])
