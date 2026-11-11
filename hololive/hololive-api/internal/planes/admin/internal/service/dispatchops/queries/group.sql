{{listProjection}}
JOIN alarm_dispatch_deliveries target ON target.id = $1
WHERE d.send_unit_id = target.send_unit_id
ORDER BY d.id
LIMIT $2
