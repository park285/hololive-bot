SELECT count(*) FROM alarm_dispatch_deliveries d JOIN alarm_dispatch_events e ON e.id=d.event_id
WHERE d.room_id=$1 AND e.category=$2
