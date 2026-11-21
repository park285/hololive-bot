UPDATE alarm_dispatch_deliveries SET status='quarantined', quarantined_at=statement_timestamp() WHERE room_id=$1
