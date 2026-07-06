
			ON CONFLICT (kind, content_id) DO UPDATE
			SET payload = EXCLUDED.payload, status = 'PENDING', attempt_count = 0, next_attempt_at = NOW(),
			    locked_at = NULL, locked_by = NULL, lock_expires_at = NULL, sending_started_at = NULL, error = NULL
			WHERE notification_delivery_outbox.status = 'FAILED'