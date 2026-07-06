
		WITH latest_alarm_name AS (
			SELECT member_name
			FROM alarms
			WHERE channel_id = $1
			  AND member_name IS NOT NULL
			  AND member_name <> ''
			ORDER BY created_at DESC
			LIMIT 1
		),
		member_display_name AS (
			SELECT COALESCE(NULLIF(short_korean_name, ''), NULLIF(korean_name, ''), '') AS member_name
			FROM members
			WHERE channel_id = $1
			ORDER BY id ASC
			LIMIT 1
		)
		SELECT COALESCE(
			NULLIF((SELECT member_name FROM member_display_name), ''),
			(SELECT member_name FROM latest_alarm_name),
			''
		)
	