-- 종료 상태를 상수로 두어 generic plan에서도 idx_ndo_terminal_cleanup 조건을 증명한다.
WITH picked AS MATERIALIZED (
	SELECT id FROM notification_delivery_outbox
	WHERE status IN ('SENT', 'FAILED', 'QUARANTINED') AND COALESCE(sent_at, created_at) < $1
	ORDER BY COALESCE(sent_at, created_at) ASC, id ASC
	LIMIT $2
), deleted AS (
	DELETE FROM notification_delivery_outbox o
	USING picked
	WHERE o.id = picked.id
		-- 행 잠금 대기 중 rearm·보존 시각 변경이 커밋되면 최신 행을 다시 검사한다.
		AND o.status IN ('SENT', 'FAILED', 'QUARANTINED')
		AND COALESCE(o.sent_at, o.created_at) < $1
	RETURNING o.id
)
-- 재검사로 삭제가 줄어도 가득 찬 후보 창 뒤에 남은 만료 행을 계속 처리한다.
SELECT (SELECT count(id) FROM picked), (SELECT count(id) FROM deleted)
