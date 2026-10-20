-- 다음 시도 시각과 종결 시각은 대상 행을 잠근 뒤 한 번 채취한 DB 시계(clock)로 정한다. 잠금 대기가 길어도
-- 재시도·종결 시각이 대기 이전으로 앞당겨지지 않는다. 정확한 claim(status, attempt_count, locked_at)이 fence다.
-- $1 다음 상태, $2 다음 시도 횟수, $3 재시도 지연 ms, $4 종결 여부, $5 사유, $6 id, $7 PENDING, $8 시도 횟수,
-- $9 claim locked_at.
WITH target AS MATERIALIZED (
    SELECT id
    FROM youtube_notification_outbox
    WHERE id = $6
      AND status = $7
      AND attempt_count = $8
      AND locked_at = $9
    FOR UPDATE
), clock AS MATERIALIZED (
    SELECT clock_timestamp() AS at
    FROM (SELECT count(id) AS locked_count FROM target) AS acquired
)
UPDATE youtube_notification_outbox
SET status = $1,
    attempt_count = $2,
    next_attempt_at = (SELECT at FROM clock) + $3::bigint * INTERVAL '1 millisecond',
    locked_at = NULL,
    sent_at = NULL,
    terminal_at = CASE WHEN $4::boolean THEN (SELECT at FROM clock) END,
    error = $5
WHERE id IN (SELECT id FROM target)
RETURNING id;
