-- 대상이 없는 fanout 완료 시각은 DB 시계(clock)로 기록한다.
-- $1 SENT, $2 id, $3 PENDING, $4 claim locked_at.
WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS at
)
UPDATE youtube_notification_outbox
SET status = $1,
    locked_at = NULL,
    sent_at = (SELECT at FROM clock),
    terminal_at = (SELECT at FROM clock),
    error = ''
WHERE id = $2
  AND status = $3
  AND locked_at = $4;
