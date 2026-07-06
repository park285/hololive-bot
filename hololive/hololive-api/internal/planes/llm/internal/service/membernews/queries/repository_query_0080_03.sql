
		SELECT DISTINCT
			COALESCE(
				NULLIF(a.member_name, ''),
				NULLIF(m.korean_name, ''),
				NULLIF(m.english_name, ''),
				NULLIF(m.japanese_name, '')
			) AS member_name
		FROM alarms a
		LEFT JOIN members m ON m.channel_id = a.channel_id
		WHERE a.room_id = $1
		  AND COALESCE(
				NULLIF(a.member_name, ''),
				NULLIF(m.korean_name, ''),
				NULLIF(m.english_name, ''),
				NULLIF(m.japanese_name, '')
			) IS NOT NULL
		ORDER BY member_name ASC
	