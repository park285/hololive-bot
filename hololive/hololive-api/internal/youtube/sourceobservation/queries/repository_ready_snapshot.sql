WITH {{ready_relation}}
SELECT COUNT(*),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (NOW() - MIN(eligible_at))), 0), 0)
FROM claim_order
