WITH locked AS MATERIALIZED (
	SELECT id, status, locked_by
	FROM notification_delivery_outbox
	WHERE id = $2
	FOR UPDATE
), eligible AS MATERIALIZED (
	SELECT locked.id, clock_timestamp() AS transitioned_at
	FROM locked
	WHERE locked.status = $3
	  AND locked.locked_by = $4
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
