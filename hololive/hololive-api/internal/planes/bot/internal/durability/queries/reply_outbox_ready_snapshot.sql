WITH ready AS (
    SELECT candidate.id, candidate.available_at AS eligible_at
    {{reply_ready_relation}}
      AND NOT ({{safety_expired}})
)
SELECT COUNT(id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (statement_timestamp() - MIN(eligible_at))), 0), 0)
FROM ready
