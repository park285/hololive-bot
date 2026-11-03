		-- 결과 불명(stale SENDING)을 FAILED로 회수하면 rearm(outbox_enqueue_batch_upsert.sql의
		-- WHERE status='FAILED')이 재발송해 중복 노출 위험이 있어 별도 terminal 상태 QUARANTINED로 격리한다.
		-- 상태는 상수로 두어 generic plan에서도 idx_ndo_sending_stale 조건을 증명한다.
		WITH picked AS (
			SELECT id FROM notification_delivery_outbox
			WHERE status = 'SENDING'
			  AND sending_started_at < clock_timestamp() - ($1::double precision * INTERVAL '1 millisecond')
			ORDER BY sending_started_at ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE notification_delivery_outbox o
		SET status = 'QUARANTINED',
		    locked_at = NULL,
		    locked_by = NULL,
		    lock_expires_at = NULL,
		    sending_started_at = NULL,
		    error = $3
		FROM picked
		WHERE o.id = picked.id
