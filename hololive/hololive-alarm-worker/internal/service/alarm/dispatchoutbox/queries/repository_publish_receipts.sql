WITH input AS (
 SELECT ordinal, event_key, payload_hash, dedupe_key
 FROM jsonb_to_recordset($1::jsonb) AS x(ordinal INT, event_key TEXT, payload_hash TEXT, dedupe_key TEXT)
)
SELECT i.ordinal, i.dedupe_key, e.payload_hash <> i.payload_hash,
 COALESCE(d.status, '')
FROM input i
JOIN alarm_dispatch_events e ON e.event_key = i.event_key
LEFT JOIN alarm_dispatch_deliveries d ON d.dedupe_key = i.dedupe_key AND d.event_id = e.id
WHERE e.payload_hash <> i.payload_hash OR d.id IS NOT NULL
ORDER BY i.ordinal
