SELECT event_key
FROM alarm_dispatch_events
WHERE event_key LIKE $1
