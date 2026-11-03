-- 자식이 하나 이상 있고 비종료(PENDING/SENDING) 자식이 없는 PENDING outbox만 aggregate 복구 후보다.
-- 상태는 고정값이라 파라미터 대신 리터럴로 둔다. 범용 실행계획에서도 상태 조건을 그대로 증명할 수 있다.
SELECT o.id
FROM youtube_notification_outbox AS o
WHERE o.status = 'PENDING'
  AND EXISTS (
      SELECT 1
      FROM youtube_notification_delivery AS d
      WHERE d.outbox_id = o.id
  )
  AND NOT EXISTS (
      SELECT 1
      FROM youtube_notification_delivery AS d
      WHERE d.outbox_id = o.id
        AND d.status IN ('PENDING', 'SENDING')
  )
ORDER BY o.id ASC
LIMIT $1
