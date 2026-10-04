
-- 보존 격리 총량·경과는 그대로 두고, 검토 대상은 별도로 센다. closeout receipt는 기록한
-- 대상 행의 현재 revision·상태 메타데이터와 정확히 일치할 때만 그 행을 검토 완료로 본다.
-- receipt가 있어도 이후 재처리·재격리로 바뀐 행은 다시 검토 대상이다.
SELECT
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.next_attempt_at))) FILTER (WHERE d.status = 'pending'), 0),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.next_attempt_at))) FILTER (WHERE d.status = 'retry'), 0),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.sending_started_at))) FILTER (WHERE d.status = 'sending'), 0),
  COUNT(d.id) FILTER (WHERE d.status = 'quarantined'),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.quarantined_at))) FILTER (WHERE d.status = 'quarantined'), 0),
  COUNT(d.id) FILTER (WHERE d.status = 'quarantined' AND NOT review.closed),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.quarantined_at))) FILTER (WHERE d.status = 'quarantined' AND NOT review.closed), 0)
FROM alarm_dispatch_deliveries d
CROSS JOIN LATERAL (
  SELECT CASE WHEN d.status <> 'quarantined' THEN FALSE ELSE EXISTS (
    SELECT 1
    FROM alarm_dispatch_closeout_receipts r
    CROSS JOIN LATERAL jsonb_array_elements(r.status_metadata) AS reviewed(fact)
    WHERE r.send_unit_id = d.send_unit_id
      AND d.id = ANY (r.target_ids)
      AND reviewed.fact ->> 'id' = d.id::text
      AND reviewed.fact ->> 'status' = d.status
      AND (reviewed.fact ->> 'attemptCount')::integer = d.attempt_count
      AND (reviewed.fact ->> 'updatedAt')::timestamptz = d.updated_at
      AND (reviewed.fact ->> 'quarantinedAt')::timestamptz IS NOT DISTINCT FROM d.quarantined_at
      AND (reviewed.fact ->> 'sentAt')::timestamptz IS NOT DISTINCT FROM d.sent_at
      AND (reviewed.fact ->> 'cancelledAt')::timestamptz IS NOT DISTINCT FROM d.cancelled_at
  ) END AS closed
) review
WHERE d.status IN ('pending', 'retry', 'sending', 'quarantined')
