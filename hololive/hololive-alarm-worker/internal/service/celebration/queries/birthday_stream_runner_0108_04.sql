SELECT event_key, payload_schema_version, payload
FROM alarm_dispatch_events
WHERE event_key = ANY($1)
  AND alarm_type = 'BIRTHDAY'
  AND category = 'celebration'
ORDER BY event_key
