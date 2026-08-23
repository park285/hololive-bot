		SELECT o.content_id, o.payload::text
		FROM youtube_notification_outbox o
		JOIN youtube_notification_delivery d ON d.outbox_id = o.id
		WHERE o.kind = $1
		  AND (o.content_id = $2 OR o.payload->>'canonical_post_id' = $3)
		  AND d.room_id = $4
		  AND d.status = $5
		  AND d.id <> $6
