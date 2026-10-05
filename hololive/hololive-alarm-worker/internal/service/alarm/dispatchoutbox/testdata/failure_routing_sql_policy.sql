
		WITH input AS (
			SELECT id, attempt_count, next_attempt_at, error, error_code, target_status
			FROM jsonb_to_recordset($1::jsonb) AS x(
				id BIGINT,
				attempt_count INT,
				next_attempt_at TIMESTAMPTZ,
				error TEXT,
				error_code TEXT,
				target_status TEXT
			)
		)
		UPDATE alarm_dispatch_deliveries d
		SET status=input.target_status,
			attempt_count=input.attempt_count,
			next_attempt_at=CASE WHEN input.target_status='retry' THEN input.next_attempt_at ELSE d.next_attempt_at END,
			dlq_at=CASE WHEN input.target_status='dlq' THEN NOW() ELSE d.dlq_at END,
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error=input.error,
			last_error_code=input.error_code,
			updated_at=NOW()
		FROM input
		WHERE d.id=input.id
		  AND input.attempt_count = d.attempt_count + 1
		  AND d.status='leased'
		  AND d.locked_by=$2
		  AND d.lock_expires_at > NOW()
		RETURNING d.id
