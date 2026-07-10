WITH locked AS MATERIALIZED (
	SELECT id
	FROM notification_delivery_outbox
	WHERE id = $3
	FOR UPDATE
), eligible AS MATERIALIZED (
	SELECT locked.id, clock_timestamp() AS transitioned_at
	FROM locked
	JOIN notification_delivery_outbox o ON o.id = locked.id
	WHERE o.status = $4
	  AND o.locked_by = $5
	  AND o.lock_expires_at > clock_timestamp()
)
UPDATE notification_delivery_outbox o
SET status = $1,
	sending_started_at = eligible.transitioned_at,
	lock_expires_at = eligible.transitioned_at + ($2::double precision * INTERVAL '1 millisecond')
FROM eligible
WHERE o.id = eligible.id
