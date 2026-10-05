WITH locked AS MATERIALIZED (
	SELECT id, status, locked_by, lock_expires_at
	FROM notification_delivery_outbox
	WHERE id = $2
	FOR UPDATE
), eligible AS MATERIALIZED (
	SELECT locked.id, clock_timestamp() AS transitioned_at
	FROM locked
	WHERE locked.status IN ($3, $4)
	  AND locked.locked_by = $5
	  AND (locked.status = $4 OR locked.lock_expires_at > clock_timestamp())
)
UPDATE notification_delivery_outbox o
SET status = $1,
	sent_at = eligible.transitioned_at,
	locked_at = NULL,
	locked_by = NULL,
	lock_expires_at = NULL,
	sending_started_at = NULL,
	error = NULL
FROM eligible
WHERE o.id = eligible.id
