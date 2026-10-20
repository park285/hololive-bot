-- 만료·due·신선도 판정과 새 locked_at은 문장 첫머리에서 한 번 채취한 DB 시계(clock)로 정한다. 스칼라 하위
-- 질의로 참조해 InitPlan 값이 되므로 PENDING 부분 인덱스 조건으로 쓰인다. clock은 행을 잠그기 전에 채취하므로
-- 판정은 실제 잠금 시점보다 보수적이고, 새 lease도 그만큼 일찍 끝난다.
-- $1 상태, $2 lease ms, $3 신선도 ms, $4 batch 크기.
WITH clock AS MATERIALIZED (
    SELECT sampled.at,
           sampled.at - $2::bigint * INTERVAL '1 millisecond' AS lock_cutoff,
           sampled.at - $3::bigint * INTERVAL '1 millisecond' AS fresh_cutoff
    FROM (SELECT clock_timestamp() AS at) AS sampled
), claim AS (
    SELECT outbox.id
    FROM youtube_notification_outbox AS outbox
    WHERE outbox.status = $1
      -- 범용 실행계획에서도 PENDING 전용 부분 인덱스 조건을 증명할 수 있게 한다.
      AND outbox.status = 'PENDING'
      AND (outbox.locked_at IS NULL OR outbox.locked_at < (SELECT lock_cutoff FROM clock))
      AND outbox.next_attempt_at <= (SELECT at FROM clock)
      AND outbox.created_at >= (SELECT fresh_cutoff FROM clock)
      AND NOT EXISTS (
          SELECT 1
          FROM youtube_notification_delivery AS delivery
          WHERE delivery.outbox_id = outbox.id
      )
    ORDER BY outbox.next_attempt_at, outbox.created_at, outbox.id
    LIMIT $4
    FOR UPDATE OF outbox SKIP LOCKED
), updated AS (
    UPDATE youtube_notification_outbox AS outbox
    SET locked_at = (SELECT at FROM clock)
    FROM claim
    WHERE outbox.id = claim.id
      AND outbox.status = $1
    RETURNING outbox.id,
              outbox.kind,
              outbox.channel_id,
              outbox.content_id,
              outbox.payload::text AS payload,
              outbox.status,
              outbox.attempt_count,
              outbox.next_attempt_at,
              outbox.created_at,
              outbox.locked_at,
              outbox.sent_at,
              COALESCE(outbox.error, '') AS error
)
SELECT id, kind, channel_id, content_id, payload, status, attempt_count,
       next_attempt_at, created_at, locked_at, sent_at, error
FROM updated
ORDER BY next_attempt_at, created_at, id;
