-- handoff 확인을 닫는다. 다음 확인 시각을 지우고 확인 시각을 남긴다. 호출자는 같은 트랜잭션에서 요청을 잠갔으므로
-- clock은 그 잠금 뒤의 DB 시계다.
WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
)
UPDATE youtube_notification_send_request r
SET handoff_next_check_at = NULL,
    handoff_checked_at = clock.now
FROM clock
WHERE r.base_id = $1
