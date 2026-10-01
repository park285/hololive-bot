WITH failures AS MATERIALIZED (
    SELECT d.id, {{errorCodeExpression}} AS error_code,
           e.alarm_type::text AS alarm_type, e.channel_id, d.room_id
    FROM alarm_dispatch_deliveries d
    JOIN alarm_dispatch_events e ON e.id = d.event_id
    WHERE d.status IN ('dlq', 'quarantined')
), counts AS (
    SELECT 'errorCodes' AS kind, error_code AS value, count(id) AS count FROM failures GROUP BY error_code
    UNION ALL
    SELECT 'alarmTypes', alarm_type, count(id) FROM failures GROUP BY alarm_type
    UNION ALL
    SELECT 'channels', channel_id, count(id) FROM failures GROUP BY channel_id
    UNION ALL
    SELECT 'rooms', room_id, count(id) FROM failures GROUP BY room_id
), ranked AS (
    SELECT kind, value, count,
           row_number() OVER (PARTITION BY kind ORDER BY count DESC, value ASC) AS rank
    FROM counts
), result AS (
    SELECT 'total' AS kind, '' AS value, count(id) AS count FROM failures
    UNION ALL
    SELECT kind, value, count FROM ranked WHERE rank <= $1
)
SELECT kind, value, count::text FROM result ORDER BY kind, result.count DESC, value ASC;
