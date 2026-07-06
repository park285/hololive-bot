
		WITH input AS (
			SELECT *
			FROM unnest($1::bigint[], $2::timestamptz[]) AS t(id, locked_at)
		)
		UPDATE youtube_notification_delivery d
		SET attempt_count = attempt_count + 1,
		    error = $3,
		    status = CASE WHEN attempt_count + 1 >= $4 THEN $5 ELSE $6 END,
		    next_attempt_at = CASE WHEN attempt_count + 1 >= $4 THEN next_attempt_at ELSE $7 END,
		    locked_at = NULL
		FROM input i
		WHERE d.id = i.id
		  AND d.status = $8
		  AND d.locked_at = i.locked_at
	