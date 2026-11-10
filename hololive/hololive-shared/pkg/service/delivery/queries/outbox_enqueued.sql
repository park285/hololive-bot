-- outbox_enqueue_batch_upsert.sql이 rearm하는 행(exhausted가 아닌 FAILED)만 제외한다.
SELECT EXISTS (
    SELECT 1
    FROM notification_delivery_outbox
    WHERE kind = $1
      AND content_id = $2
      AND NOT (
          status = 'FAILED'
          AND COALESCE(payload->'request'->>'exhausted', 'false') <> 'true'
      )
)
