SELECT d.id::text, d.event_id::text, d.room_id, COALESCE(d.send_unit_id::text, ''),
       e.alarm_type::text, e.channel_id, e.stream_id, d.status, d.attempt_count,
       CASE WHEN d.last_error_code ~ '^[A-Za-z0-9_.:-]{1,128}$'
            THEN d.last_error_code WHEN d.last_error_code = '' THEN '' ELSE 'unclassified' END,
       d.next_attempt_at, d.created_at, d.updated_at, d.lock_expires_at,
       d.sending_started_at, d.sent_at, d.dlq_at, d.quarantined_at, d.cancelled_at
FROM alarm_dispatch_deliveries d
JOIN alarm_dispatch_events e ON e.id = d.event_id
JOIN alarm_dispatch_deliveries target ON target.id = $1
WHERE (target.send_unit_id IS NULL AND d.id = target.id)
   OR (target.send_unit_id IS NOT NULL AND d.send_unit_id = target.send_unit_id)
ORDER BY d.id
LIMIT $2
