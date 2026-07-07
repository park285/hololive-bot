
		WITH input AS (
			SELECT id, error
			FROM jsonb_to_recordset($1::jsonb) AS x(id BIGINT, error TEXT)
		)
		UPDATE alarm_dispatch_deliveries d
		SET status=$2,
			%s=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error=CASE WHEN input.error = '' THEN d.last_error ELSE input.error END,
			updated_at=NOW()
		FROM input
		WHERE d.id = input.id
		  AND d.locked_by = $3
		  AND %s
