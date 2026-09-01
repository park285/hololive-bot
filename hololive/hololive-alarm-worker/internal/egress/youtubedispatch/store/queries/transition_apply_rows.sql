WITH input AS (
    SELECT id,
           expected_status,
           expected_version,
           expected_attempt,
           expected_locked_at,
           next_status,
           next_version,
           next_attempt,
           next_attempt_at,
           next_locked_at,
           next_sent_at,
           next_error
    FROM unnest(
        $1::bigint[],
        $2::text[],
        $3::bigint[],
        $4::integer[],
        $5::timestamptz[],
        $6::text[],
        $7::bigint[],
        $8::integer[],
        $9::timestamptz[],
        $10::timestamptz[],
        $11::timestamptz[],
        $12::text[]
    ) AS values(
        id,
        expected_status,
        expected_version,
        expected_attempt,
        expected_locked_at,
        next_status,
        next_version,
        next_attempt,
        next_attempt_at,
        next_locked_at,
        next_sent_at,
        next_error
    )
), updated AS (
    UPDATE youtube_notification_delivery AS delivery
    SET status = input.next_status,
        row_version = input.next_version,
        attempt_count = input.next_attempt,
        next_attempt_at = input.next_attempt_at,
        locked_at = input.next_locked_at,
        sent_at = input.next_sent_at,
        error = input.next_error
    FROM input
    WHERE delivery.id = input.id
      AND delivery.status = input.expected_status
      AND delivery.row_version = input.expected_version
      AND delivery.attempt_count = input.expected_attempt
      AND delivery.locked_at IS NOT DISTINCT FROM input.expected_locked_at
    RETURNING delivery.id, delivery.outbox_id
)
SELECT id, outbox_id FROM updated ORDER BY id;
