WITH locked AS MATERIALIZED (
	SELECT id, status, locked_by, lock_expires_at
	FROM notification_delivery_outbox
	WHERE id = $3
	FOR UPDATE
), eligible AS MATERIALIZED (
	SELECT locked.id, clock_timestamp() AS transitioned_at
	FROM locked
	WHERE locked.status = $4
	  AND locked.locked_by = $5
	  AND locked.lock_expires_at > clock_timestamp()
)
UPDATE notification_delivery_outbox o
SET status = $1,
	sending_started_at = eligible.transitioned_at,
	lock_expires_at = eligible.transitioned_at + ($2::double precision * INTERVAL '1 millisecond')
FROM eligible
WHERE o.id = eligible.id
