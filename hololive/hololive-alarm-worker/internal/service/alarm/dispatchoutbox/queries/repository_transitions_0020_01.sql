
		UPDATE alarm_dispatch_deliveries
		SET status='sending',
			sending_started_at=NOW(),
			lock_expires_at=NOW() + ($2::INT * INTERVAL '1 second'),
			updated_at=NOW()
		WHERE id = ANY($1)
		  AND status = 'leased'
		  AND locked_by = $3
		  AND lock_expires_at > NOW()