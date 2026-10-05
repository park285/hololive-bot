WITH picked AS (
			SELECT id FROM notification_delivery_outbox
			WHERE status IN ($1, $2, $3) AND COALESCE(sent_at, created_at) < $4
			ORDER BY COALESCE(sent_at, created_at) ASC, id ASC
			LIMIT $5
		)
		DELETE FROM notification_delivery_outbox o
		USING picked
		WHERE o.id = picked.id
