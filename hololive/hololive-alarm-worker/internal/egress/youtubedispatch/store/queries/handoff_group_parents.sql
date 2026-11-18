WITH /* fragment:transition_group_candidates */
SELECT DISTINCT grouped.outbox_id
FROM (
    SELECT delivery.outbox_id
    FROM youtube_notification_delivery AS delivery
    JOIN candidate_ids ON candidate_ids.id = delivery.id
    ORDER BY delivery.created_at, delivery.id
    LIMIT $5
) AS grouped
ORDER BY grouped.outbox_id
