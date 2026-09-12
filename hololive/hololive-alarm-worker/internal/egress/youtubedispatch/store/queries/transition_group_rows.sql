-- 요청 관계를 한 번만 만들고 identity별 등가 join으로 후보를 찾는다.
-- correlated OR EXISTS는 전체 delivery마다 요청 배열을 다시 순회한다.
WITH requested AS MATERIALIZED (
    SELECT DISTINCT room_id, kind, candidate
    FROM unnest($2::text[], $3::text[], $4::text[]) AS requested(room_id, kind, candidate)
), candidate_ids AS (
    SELECT id
    FROM unnest($1::bigint[]) AS direct(id)
    UNION
    SELECT delivery.id
    FROM requested
    JOIN youtube_notification_outbox AS outbox
      ON outbox.kind = requested.kind
     AND btrim(outbox.content_id) = requested.candidate
    JOIN youtube_notification_delivery AS delivery
      ON delivery.outbox_id = outbox.id
     AND delivery.room_id = requested.room_id
    UNION
    SELECT delivery.id
    FROM requested
    JOIN youtube_notification_outbox AS outbox
      ON outbox.kind = requested.kind
     AND COALESCE(outbox.payload->>'canonical_post_id', '') = requested.candidate
    JOIN youtube_notification_delivery AS delivery
      ON delivery.outbox_id = outbox.id
     AND delivery.room_id = requested.room_id
)
SELECT delivery.id,
       delivery.outbox_id,
       delivery.room_id,
       delivery.status,
       delivery.attempt_count,
       delivery.next_attempt_at,
       delivery.created_at,
       delivery.locked_at,
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
