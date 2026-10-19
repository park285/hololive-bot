delivery.status = $1 AND delivery.status = 'PENDING'
AND COALESCE(cardinality(request.member_ids), 1) <= $5
AND (delivery.locked_at IS NULL OR delivery.locked_at < $2)
AND delivery.next_attempt_at <= $3 AND outbox.created_at >= $4
AND (request.base_id IS NULL OR NOT EXISTS (
    SELECT 1 FROM unnest(request.member_ids) AS ids(id)
    LEFT JOIN youtube_notification_delivery member ON member.id = ids.id
    LEFT JOIN youtube_notification_outbox parent ON parent.id = member.outbox_id
    WHERE member.id IS NULL OR member.status <> 'PENDING'
       OR member.next_attempt_at > $3 OR parent.created_at < $4
       OR (member.locked_at IS NOT NULL AND member.locked_at >= $2)
))
