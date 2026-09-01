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
WHERE delivery.id = ANY($1::bigint[])
   OR (
       delivery.room_id = ANY($2::text[])
       AND outbox.kind = ANY($3::text[])
       AND (
           btrim(outbox.content_id) = ANY($4::text[])
           OR COALESCE(outbox.payload->>'canonical_post_id', '') = ANY($4::text[])
       )
   )
ORDER BY delivery.created_at, delivery.id
LIMIT $5
FOR UPDATE OF delivery;
