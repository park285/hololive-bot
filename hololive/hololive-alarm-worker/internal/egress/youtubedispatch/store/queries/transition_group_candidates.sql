requested AS MATERIALIZED (
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
