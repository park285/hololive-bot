SELECT DISTINCT e.event_key, d.room_id
FROM alarm_dispatch_events e
JOIN alarm_dispatch_deliveries d ON d.event_id = e.id
WHERE e.event_key = ANY($1)
  AND e.alarm_type = 'BIRTHDAY'
  AND e.category = 'celebration'
  AND d.status = 'sent'
  AND d.sent_at IS NOT NULL
ORDER BY e.event_key, d.room_id
