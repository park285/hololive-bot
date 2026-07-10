WITH locked AS MATERIALIZED (
	SELECT id
	FROM notification_delivery_outbox
	WHERE id = $2
	FOR UPDATE
), eligible AS MATERIALIZED (
	SELECT locked.id, clock_timestamp() AS transitioned_at
	FROM locked
	JOIN notification_delivery_outbox o ON o.id = locked.id
	WHERE o.status IN ($3, $4)
	  AND (
		(o.locked_by = $5 AND (o.status = $4 OR o.lock_expires_at > clock_timestamp()))
		OR (o.locked_by IS NULL AND o.status = $3 AND o.locked_at = $6)
	  )
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
