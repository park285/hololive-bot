
		UPDATE alarm_dispatch_deliveries
		SET status='sent',
			sent_at=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			updated_at=NOW()
		WHERE id = ANY($1)
		  AND status = 'sending'
		  AND locked_by = $2
		  AND lock_expires_at > NOW()