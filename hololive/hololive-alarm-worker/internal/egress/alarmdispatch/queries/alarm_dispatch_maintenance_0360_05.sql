WITH picked AS (
    SELECT u.id
    FROM alarm_dispatch_send_units u
    WHERE NOT EXISTS (
        SELECT 1
        FROM alarm_dispatch_deliveries d
        WHERE d.send_unit_id = u.id
    )
    ORDER BY u.id
    LIMIT $1
    FOR UPDATE OF u SKIP LOCKED
)
DELETE FROM alarm_dispatch_send_units u
USING picked
WHERE u.id = picked.id
