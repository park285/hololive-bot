WITH locked AS MATERIALIZED (
	SELECT id
	FROM notification_delivery_outbox
	WHERE id = ANY($2)
	ORDER BY id
	FOR UPDATE
), transition_time AS MATERIALIZED (
	SELECT clock_timestamp() AS value
	FROM locked
	LIMIT 1
)
UPDATE notification_delivery_outbox o
SET status = $1,
	sent_at = transition_time.value,
	locked_at = NULL,
	locked_by = NULL,
	lock_expires_at = NULL,
	sending_started_at = NULL,
	error = NULL
FROM locked, transition_time
WHERE o.id = locked.id
	AND o.status = $3
	AND o.locked_by IS NULL
	AND o.locked_at IS NULL
	AND o.lock_expires_at IS NULL
	AND o.sending_started_at IS NULL
