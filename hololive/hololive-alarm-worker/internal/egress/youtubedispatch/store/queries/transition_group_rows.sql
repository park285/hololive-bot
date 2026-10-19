-- 논리 그룹 delivery를 (created_at, id) 순서로 잠근다. 후보 조건은 transition_group_candidates.sql 조각이다.
-- $5는 잠글 행 한도다.
WITH /* transition_group_candidates */
SELECT delivery.id,
       delivery.outbox_id,
       delivery.room_id,
       delivery.status,
       delivery.attempt_count,
       delivery.next_attempt_at,
       delivery.created_at,
       delivery.locked_at,
       delivery.locked_by,
       delivery.sent_at,
       COALESCE(delivery.error, '') AS error,
       delivery.row_version,
       outbox.kind,
       outbox.channel_id,
       outbox.content_id,
       outbox.payload::text AS payload,
       outbox.created_at AS outbox_created_at,
       outbox.sent_at AS outbox_sent_at
FROM youtube_notification_delivery AS delivery
JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
JOIN candidate_ids ON candidate_ids.id = delivery.id
ORDER BY delivery.created_at, delivery.id
LIMIT $5
FOR UPDATE OF delivery;
