-- 상태와 보존 시각 열을 상수로 두어 generic plan에서도 sent 보존 부분 인덱스를 쓴다. 바깥 DELETE는
-- 고른 id 배열로 PK만 찾아, generic plan이 만료 행 전체를 hash join으로 읽지 않게 한다.
WITH picked AS (
    SELECT id
    FROM alarm_dispatch_deliveries
    WHERE status = 'sent'
      AND sent_at < NOW() - ($1::int * INTERVAL '1 day')
    ORDER BY sent_at ASC, id ASC
    LIMIT $2
)
DELETE FROM alarm_dispatch_deliveries d
WHERE d.id = ANY(ARRAY(SELECT id FROM picked))
  AND d.status = 'sent'
  AND d.sent_at < NOW() - ($1::int * INTERVAL '1 day')
