-- sending_started_at이 $2 밀리초보다 오래된 sending을 결과 불명으로 격리한다. 판정·기록 시각은 clock 하나다.
WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
), picked AS (
    SELECT d.id
    FROM alarm_dispatch_deliveries d
    WHERE d.status = 'sending'
      AND d.sending_started_at < (SELECT clock.now FROM clock) - ($2::BIGINT * INTERVAL '1 millisecond')
    ORDER BY d.sending_started_at ASC, d.id ASC
    LIMIT $1
    FOR UPDATE OF d SKIP LOCKED
)
UPDATE alarm_dispatch_deliveries d
SET status = 'quarantined',
    quarantined_at = clock.now,
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error = 'stale sending; external send outcome unknown',
    last_error_code = 'stale_sending',
    updated_at = clock.now
FROM picked CROSS JOIN clock
WHERE d.id = picked.id
