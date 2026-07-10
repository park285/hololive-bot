UPDATE youtube_notification_outbox o
		SET status = agg.next_status,
		    locked_at = NULL,
		    sent_at = CASE WHEN agg.next_status = $6::text AND o.sent_at IS NULL THEN $7 ELSE o.sent_at END,
		    error = CASE WHEN agg.next_status = $4::text THEN $8::text ELSE '' END
		FROM (
		    SELECT ids.id,
		        CASE
		            WHEN COUNT(d.status) FILTER (WHERE d.status IN ($2::text, $3::text)) > 0 THEN $2::text
		            WHEN COUNT(d.status) FILTER (WHERE d.status IN ($4::text, $5::text)) > 0 THEN $4::text
		            WHEN COUNT(d.status) FILTER (WHERE d.status = $6::text) > 0 THEN $6::text
		            ELSE $2::text
		        END AS next_status
		    FROM unnest($1::bigint[]) AS ids(id)
		    LEFT JOIN youtube_notification_delivery d ON d.outbox_id = ids.id
		    GROUP BY ids.id
		) agg
		WHERE o.id = agg.id
		  AND (o.status IS DISTINCT FROM agg.next_status
		       OR (agg.next_status = $6::text AND o.sent_at IS NULL))
