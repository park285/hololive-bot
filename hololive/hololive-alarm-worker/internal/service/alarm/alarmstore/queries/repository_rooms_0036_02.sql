WITH room_pairs AS (
	SELECT DISTINCT room_id, channel_id
	FROM alarms
),
kakao_room_names AS (
	SELECT DISTINCT ON (room_id) room_id, room_name
	FROM alarms
	WHERE room_name IS NOT NULL
	  AND btrim(room_name) <> ''
	  AND room_name <> room_id
	ORDER BY room_id, room_name_updated_at DESC, id DESC
)
SELECT p.room_id,
       p.channel_id,
       COALESCE(d.display_name, k.room_name, '') AS room_name
FROM room_pairs p
LEFT JOIN alarm_room_display_names d ON d.room_id = p.room_id
LEFT JOIN kakao_room_names k ON k.room_id = p.room_id
ORDER BY p.room_id, p.channel_id
