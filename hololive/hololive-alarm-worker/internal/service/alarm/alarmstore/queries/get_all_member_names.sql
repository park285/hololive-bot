		WITH alarm_channels AS (
			SELECT DISTINCT channel_id
			FROM alarms
			WHERE channel_id IS NOT NULL AND channel_id != ''
		),
		member_display_names AS (
			SELECT DISTINCT ON (channel_id)
			       channel_id,
			       COALESCE(NULLIF(short_korean_name, ''), NULLIF(korean_name, ''), '') AS member_name
			FROM members
			WHERE channel_id IS NOT NULL AND channel_id != ''
			ORDER BY channel_id, id ASC
		)
		SELECT c.channel_id, m.member_name
		FROM alarm_channels c
		JOIN member_display_names m ON m.channel_id = c.channel_id
		WHERE m.member_name != ''
