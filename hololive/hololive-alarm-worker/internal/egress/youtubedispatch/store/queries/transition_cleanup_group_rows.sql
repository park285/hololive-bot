WITH requested AS MATERIALIZED (
    SELECT key_no, room_id, kind, candidate
    FROM unnest($2::text[], $3::text[], $4::text[], $5::bigint[]) AS requested(room_id, kind, candidate, key_no)
), matched AS (
    SELECT requested.key_no, delivery.id, delivery.created_at
    FROM requested
    JOIN youtube_notification_outbox AS outbox
      ON outbox.kind = requested.kind
     AND btrim(outbox.content_id) = requested.candidate
    JOIN youtube_notification_delivery AS delivery
      ON delivery.outbox_id = outbox.id
     AND delivery.room_id = requested.room_id
    UNION
    SELECT requested.key_no, delivery.id, delivery.created_at
    FROM requested
    JOIN youtube_notification_outbox AS outbox
      ON outbox.kind = requested.kind
     AND COALESCE(outbox.payload->>'canonical_post_id', '') = requested.candidate
    JOIN youtube_notification_delivery AS delivery
      ON delivery.outbox_id = outbox.id
     AND delivery.room_id = requested.room_id
), ranked AS (
    SELECT key_no,
           id,
           row_number() OVER (PARTITION BY key_no ORDER BY created_at, id) AS key_rank
    FROM matched
), scanned AS MATERIALIZED (
    SELECT observed.id,
           array_agg(observed.key_no) FILTER (WHERE observed.key_rank = $6) AS saturated_keys
    FROM (
        SELECT direct.id, NULL::bigint AS key_no, NULL::bigint AS key_rank
        FROM unnest($1::bigint[]) AS direct(id)
        UNION ALL
        SELECT ranked.id, ranked.key_no, ranked.key_rank
        FROM ranked
        WHERE ranked.key_rank <= $6
    ) AS observed
    GROUP BY observed.id
), locked_rows AS MATERIALIZED (
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
           outbox.sent_at AS outbox_sent_at,
           scanned.saturated_keys
    FROM youtube_notification_delivery AS delivery
    JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
    JOIN scanned ON scanned.id = delivery.id
    FOR UPDATE OF delivery SKIP LOCKED
)
SELECT locked_rows.id,
       locked_rows.outbox_id,
       locked_rows.room_id,
       locked_rows.status,
       locked_rows.attempt_count,
       locked_rows.next_attempt_at,
       locked_rows.created_at,
       locked_rows.locked_at,
       locked_rows.locked_by,
       locked_rows.sent_at,
       locked_rows.error,
       locked_rows.row_version,
       locked_rows.kind,
       locked_rows.channel_id,
       locked_rows.content_id,
       locked_rows.payload,
       locked_rows.outbox_created_at,
       locked_rows.outbox_sent_at,
       locked_rows.saturated_keys,
       TRUE AS locked
FROM locked_rows
UNION ALL
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
       outbox.sent_at AS outbox_sent_at,
       scanned.saturated_keys,
       FALSE AS locked
FROM scanned
JOIN youtube_notification_delivery AS delivery ON delivery.id = scanned.id
JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
WHERE NOT EXISTS (SELECT 1 FROM locked_rows WHERE locked_rows.id = scanned.id)
ORDER BY created_at, id;
