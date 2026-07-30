
		)
		SELECT i.identity_key, MIN(s.alarm_sent_at) AS sent_at
		FROM input i
		JOIN youtube_community_shorts_alarm_states s
		  ON s.kind = i.kind AND s.post_id = i.post_id
		WHERE s.alarm_sent_at IS NOT NULL
		GROUP BY i.identity_key