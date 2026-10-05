
			SELECT id, kind, channel_id, content_id, payload::text AS payload, status, attempt_count, next_attempt_at, created_at, locked_at, sent_at, COALESCE(error, '') AS error
		FROM youtube_notification_outbox
		WHERE id = ANY($1::bigint[])
		  AND kind = ANY($2::text[])
		  AND status = ANY($3::text[])
		ORDER BY id ASC
