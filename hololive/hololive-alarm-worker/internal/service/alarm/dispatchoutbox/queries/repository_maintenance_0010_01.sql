-- 만료 판정과 기록 시각은 문장 시작에 한 번 채취한 clock을 쓴다. 갱신 시점의 만료 재검사는 소유자가 그 사이
-- 정산했거나 다른 회수가 먼저 가져간 행을 건너뛴다.
WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
), expired_window AS MATERIALIZED (
    SELECT d.send_unit_id, d.lock_expires_at, d.id AS delivery_id
    FROM alarm_dispatch_deliveries d
    WHERE d.status = 'leased' AND d.lock_expires_at < (SELECT clock.now FROM clock)
      AND d.send_unit_id IS NOT NULL
    ORDER BY d.lock_expires_at ASC, d.id ASC
    LIMIT $1::INT * $2::INT
), unit_candidates AS (
    SELECT DISTINCT ON (send_unit_id)
        send_unit_id AS id, lock_expires_at, delivery_id
    FROM expired_window
    ORDER BY send_unit_id, lock_expires_at ASC, delivery_id ASC
), locked_units AS (
    SELECT u.id, candidate.lock_expires_at, candidate.delivery_id,
        (
            SELECT count(d.id)
            FROM alarm_dispatch_deliveries d
            WHERE d.send_unit_id = u.id AND d.status = 'leased'
              AND d.lock_expires_at < clock.now
        ) AS delivery_count
    FROM unit_candidates candidate
    JOIN alarm_dispatch_send_units u ON u.id = candidate.id
    CROSS JOIN clock
    WHERE NOT EXISTS (
        SELECT 1 FROM alarm_dispatch_deliveries active
        WHERE active.send_unit_id = u.id AND active.status = 'leased'
          AND active.lock_expires_at >= clock.now
    )
    ORDER BY candidate.lock_expires_at ASC, candidate.delivery_id ASC
    LIMIT $1::INT
    FOR UPDATE OF u SKIP LOCKED
), ranked_units AS (
    SELECT id,
        row_number() OVER (ORDER BY lock_expires_at ASC, delivery_id ASC) AS ordinal,
        sum(delivery_count) OVER (ORDER BY lock_expires_at ASC, delivery_id ASC ROWS UNBOUNDED PRECEDING) AS cumulative_deliveries
    FROM locked_units
), picked AS (
    SELECT id FROM ranked_units
    WHERE cumulative_deliveries <= $1::INT OR ordinal = 1
)
UPDATE alarm_dispatch_deliveries d
SET status = 'retry',
    next_attempt_at = clock.now,
    locked_by = NULL,
    locked_at = NULL,
    lock_expires_at = NULL,
    last_error = 'lease expired before external send',
    last_error_code = 'lease_expired',
    updated_at = clock.now
FROM picked CROSS JOIN clock
WHERE d.send_unit_id = picked.id
  AND d.status = 'leased' AND d.lock_expires_at < clock.now
