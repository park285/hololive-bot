WITH locked AS MATERIALIZED (
	SELECT id, status, locked_by, lock_expires_at, attempt_count
	FROM notification_delivery_outbox
	WHERE id = $4
	FOR UPDATE
), eligible AS MATERIALIZED (
	SELECT locked.id, clock_timestamp() AS transitioned_at
	FROM locked
	WHERE locked.status IN ($5, $6)
	  AND locked.locked_by = $7
	  AND locked.attempt_count = $8
	  AND (locked.status = $6 OR locked.lock_expires_at > clock_timestamp())
)
UPDATE notification_delivery_outbox o
SET attempt_count = o.attempt_count + 1,
	error = $1,
	status = $2,
	next_attempt_at = CASE
		WHEN $9 THEN eligible.transitioned_at + ($3::double precision * INTERVAL '1 millisecond')
		ELSE o.next_attempt_at
	END,
	locked_at = NULL,
	locked_by = NULL,
	lock_expires_at = NULL,
	sending_started_at = NULL
FROM eligible
WHERE o.id = eligible.id
