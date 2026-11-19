
SELECT
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.next_attempt_at))) FILTER (WHERE d.status = 'pending'), 0),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.next_attempt_at))) FILTER (WHERE d.status = 'retry'), 0),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.sending_started_at))) FILTER (WHERE d.status = 'sending'), 0),
  COUNT(d.id) FILTER (WHERE d.status = 'quarantined'),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.quarantined_at))) FILTER (WHERE d.status = 'quarantined'), 0),
  COUNT(d.id) FILTER (WHERE d.status = 'quarantined' AND NOT review.closed),
  COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - d.quarantined_at))) FILTER (WHERE d.status = 'quarantined' AND NOT review.closed), 0),
  COUNT(d.id) FILTER (WHERE d.status = 'pending'),
  COUNT(d.id) FILTER (WHERE d.status = 'retry'),
  COUNT(d.id) FILTER (WHERE d.status = 'leased'),
  COUNT(d.id) FILTER (WHERE d.status = 'sending')
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
WHERE d.status IN ('pending', 'retry', 'leased', 'sending', 'quarantined')
