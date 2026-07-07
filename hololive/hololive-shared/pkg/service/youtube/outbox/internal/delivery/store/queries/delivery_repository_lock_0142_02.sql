
		WITH input AS (
			SELECT id, locked_at
			FROM unnest($1::bigint[], $2::timestamptz[]) AS t(id, locked_at)
		), updated AS (
			UPDATE youtube_notification_delivery d
			SET status = $3, sent_at = $4, locked_at = NULL, error = ''
			FROM input i
			WHERE d.id = i.id
			  AND d.status = $5
			  AND d.locked_at = i.locked_at
			RETURNING d.id
		)
		SELECT id FROM updated
