
		WITH input AS (
			SELECT *
			FROM unnest($1::bigint[], $2::timestamptz[]) AS t(id, locked_at)
		)
		UPDATE youtube_notification_delivery d
		SET attempt_count = CASE WHEN attempt_count >= $3 THEN attempt_count ELSE $3 END,
		    error = $4,
		    status = $5,
		    locked_at = NULL
		FROM input i
		WHERE d.id = i.id
		  AND d.status = $6
		  AND d.locked_at = i.locked_at
	