
		WITH input AS (
			SELECT *
			FROM jsonb_to_recordset($1::jsonb) AS x(
				id BIGINT,
				attempt_count INT,
				next_attempt_at TIMESTAMPTZ,
				error TEXT
			)
		)
			UPDATE alarm_dispatch_deliveries
			SET status='retry',
				attempt_count=input.attempt_count,
				next_attempt_at=input.next_attempt_at,
				locked_by=NULL,
				locked_at=NULL,
				lock_expires_at=NULL,
				last_error=input.error,
				updated_at=NOW()
			FROM input
			WHERE alarm_dispatch_deliveries.id=input.id
			  AND status='leased'
			  AND locked_by=$2
			  AND lock_expires_at > NOW()