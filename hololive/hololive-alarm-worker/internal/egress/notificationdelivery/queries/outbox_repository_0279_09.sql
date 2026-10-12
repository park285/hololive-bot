-- 종료 상태를 상수로 두어 generic plan에서도 idx_ndo_terminal_cleanup 조건을 증명한다.
WITH picked AS (
			SELECT id FROM notification_delivery_outbox
			WHERE status IN ('SENT', 'FAILED', 'QUARANTINED') AND COALESCE(sent_at, created_at) < $1
			ORDER BY COALESCE(sent_at, created_at) ASC, id ASC
			LIMIT $2
		)
		DELETE FROM notification_delivery_outbox o
		USING picked
		WHERE o.id = picked.id
