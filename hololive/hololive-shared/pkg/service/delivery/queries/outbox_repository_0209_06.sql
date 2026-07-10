WITH locked AS MATERIALIZED (
	SELECT id
	FROM notification_delivery_outbox
	WHERE id = $4
	FOR UPDATE
), eligible AS MATERIALIZED (
	SELECT locked.id, clock_timestamp() AS transitioned_at
	FROM locked
	JOIN notification_delivery_outbox o ON o.id = locked.id
	WHERE o.status IN ($5, $6)
	  AND (
		(o.locked_by = $7 AND (o.status = $6 OR o.lock_expires_at > clock_timestamp()))
		OR (o.locked_by IS NULL AND o.status = $5 AND o.locked_at = $8)
	  )
)
UPDATE notification_delivery_outbox o
SET attempt_count = o.attempt_count + 1,
	error = $1,
	status = CASE WHEN o.attempt_count + 1 >= $2 THEN 'FAILED' ELSE 'PENDING' END,
	next_attempt_at = CASE
		WHEN o.attempt_count + 1 >= $2 THEN o.next_attempt_at
		ELSE eligible.transitioned_at + ($3::double precision * INTERVAL '1 millisecond')
	END,
	locked_at = NULL,
	locked_by = NULL,
	lock_expires_at = NULL,
	sending_started_at = NULL
FROM eligible
WHERE o.id = eligible.id
