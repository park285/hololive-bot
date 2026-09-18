SELECT id::text, delivery_id::text, action, operator_id, reason,
       from_status, to_status, duplicate_risk_ack, created_at
FROM alarm_dispatch_admin_actions
WHERE delivery_id = $1 AND ($2::bigint IS NULL OR id < $2)
ORDER BY alarm_dispatch_admin_actions.id DESC
LIMIT $3
