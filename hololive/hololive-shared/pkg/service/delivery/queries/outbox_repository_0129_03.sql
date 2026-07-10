WITH now_value AS MATERIALIZED (
	SELECT clock_timestamp() AS value
), claim AS (
	SELECT o.id
	FROM notification_delivery_outbox o
	CROSS JOIN now_value
	WHERE o.status = 'PENDING'
	  AND o.next_attempt_at <= now_value.value
	  AND (
		o.locked_at IS NULL
		OR o.lock_expires_at <= now_value.value
		OR (
			o.lock_expires_at IS NULL
			AND o.locked_at < now_value.value - ($1::double precision * INTERVAL '1 millisecond')
		)
	  )
	ORDER BY o.next_attempt_at ASC, o.created_at ASC, o.id ASC
	LIMIT $2
	FOR UPDATE OF o SKIP LOCKED
)
UPDATE notification_delivery_outbox o
SET locked_at = now_value.value,
	locked_by = $3,
	lock_expires_at = now_value.value + ($4::double precision * INTERVAL '1 millisecond')
FROM claim, now_value
WHERE o.id = claim.id
RETURNING o.id, o.kind, o.period_key, o.room_id, o.content_id, o.payload,
	o.status, o.attempt_count, o.next_attempt_at, o.created_at,
	o.locked_at, o.sent_at, o.error
