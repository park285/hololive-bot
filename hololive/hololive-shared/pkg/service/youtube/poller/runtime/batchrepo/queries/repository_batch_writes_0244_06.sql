
		ON CONFLICT (kind, content_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    payload = EXCLUDED.payload,
		    status = 'PENDING',
		    attempt_count = 0,
		    next_attempt_at = EXCLUDED.next_attempt_at,
		    locked_at = NULL,
		    sent_at = NULL,
		    error = ''
		WHERE youtube_notification_outbox.status = 'FAILED'
		  AND youtube_notification_outbox.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	