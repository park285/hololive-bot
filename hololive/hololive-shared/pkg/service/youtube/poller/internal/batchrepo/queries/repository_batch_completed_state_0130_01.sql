
		)
		SELECT i.identity_key, MIN(t.alarm_sent_at) AS sent_at
		FROM input i
		JOIN youtube_content_alarm_tracking t
		  ON t.kind = i.kind
		 AND (
		      t.canonical_content_id = i.canonical_content_id
		      OR t.content_id = i.requested_content_id
		      OR t.content_id = i.canonical_content_id
		      OR (i.raw_content_id <> '' AND t.content_id = i.raw_content_id)
		 )
		WHERE t.alarm_sent_at IS NOT NULL
		GROUP BY i.identity_key