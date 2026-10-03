SELECT id, status, COALESCE(locked_by, ''), COALESCE(lock_expires_at > NOW(), false),
       sending_started_at IS NOT NULL
FROM alarm_dispatch_deliveries WHERE send_unit_id = $1 ORDER BY id FOR UPDATE
