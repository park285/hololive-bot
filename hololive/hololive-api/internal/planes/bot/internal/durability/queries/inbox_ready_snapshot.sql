WITH ready AS (
    SELECT due.id, due.available_at AS eligible_at
    {{inbox_ready_relation}}
)
SELECT COUNT(id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (statement_timestamp() - MIN(eligible_at))), 0), 0)
FROM ready
