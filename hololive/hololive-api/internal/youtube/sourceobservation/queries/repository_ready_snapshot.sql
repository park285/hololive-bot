WITH {{ready_relation}}
SELECT COUNT(claim_order.observation_id),
       COALESCE(GREATEST(EXTRACT(EPOCH FROM (NOW() - MIN(eligible_at))), 0), 0)
FROM claim_order
