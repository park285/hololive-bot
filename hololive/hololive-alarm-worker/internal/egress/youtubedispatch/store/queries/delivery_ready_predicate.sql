delivery.status = $1 AND delivery.status = 'PENDING'
AND COALESCE(cardinality(request.member_ids), 1) <= $4
AND (delivery.locked_at IS NULL OR delivery.locked_at < (SELECT lock_cutoff FROM clock))
AND delivery.next_attempt_at <= (SELECT at FROM clock) AND outbox.created_at >= (SELECT fresh_cutoff FROM clock)
AND (request.base_id IS NULL OR NOT EXISTS (
    SELECT 1 FROM unnest(request.member_ids) AS ids(id)
    LEFT JOIN youtube_notification_delivery member ON member.id = ids.id
    LEFT JOIN youtube_notification_outbox parent ON parent.id = member.outbox_id
    WHERE member.id IS NULL OR member.status <> 'PENDING'
       OR member.next_attempt_at > (SELECT at FROM clock) OR parent.created_at < (SELECT fresh_cutoff FROM clock)
       OR (member.locked_at IS NOT NULL AND member.locked_at >= (SELECT lock_cutoff FROM clock))
))
