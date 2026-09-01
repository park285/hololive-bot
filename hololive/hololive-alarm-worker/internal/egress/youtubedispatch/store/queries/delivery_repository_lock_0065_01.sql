
		WITH input AS (
			SELECT id, locked_at, ord
			FROM unnest($1::bigint[], $2::timestamptz[]) WITH ORDINALITY AS t(id, locked_at, ord)
		), updated AS (
			UPDATE youtube_notification_delivery d
			SET status = $3, locked_at = $4
			FROM input i
			WHERE d.id = i.id
			  AND d.status = $5
			  AND d.locked_at = i.locked_at
			RETURNING i.ord, d.id, d.outbox_id, d.room_id, d.status, d.attempt_count,
			          d.next_attempt_at, d.created_at, d.locked_at, d.sent_at, COALESCE(d.error, '') AS error
		)
		SELECT id, outbox_id, room_id, status, attempt_count,
		       next_attempt_at, created_at, locked_at, sent_at, error
		FROM updated
		ORDER BY ord ASC
