
		WITH alarm_channels AS (
			SELECT DISTINCT channel_id
			FROM alarms
			WHERE channel_id IS NOT NULL AND channel_id != ''
		),
		latest_alarm_names AS (
			SELECT DISTINCT ON (channel_id) channel_id, member_name
			FROM alarms
			WHERE channel_id IS NOT NULL AND channel_id != ''
			  AND member_name IS NOT NULL
			  AND member_name <> ''
			ORDER BY channel_id, created_at DESC
		),
		member_display_names AS (
			SELECT DISTINCT ON (channel_id)
			       channel_id,
			       COALESCE(NULLIF(short_korean_name, ''), NULLIF(korean_name, ''), '') AS member_name
			FROM members
			WHERE channel_id IS NOT NULL AND channel_id != ''
			ORDER BY channel_id, id ASC
		)
		SELECT c.channel_id,
		       COALESCE(NULLIF(m.member_name, ''), a.member_name, '') AS member_name
		FROM alarm_channels c
		LEFT JOIN latest_alarm_names a ON a.channel_id = c.channel_id
		LEFT JOIN member_display_names m ON m.channel_id = c.channel_id
		WHERE COALESCE(NULLIF(m.member_name, ''), a.member_name, '') != ''
		ORDER BY c.channel_id
	