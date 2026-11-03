-- ready 판정·lease·잠금 시각은 문장 시작에 한 번 채취한 clock 하나를 쓴다. lease는 $3 밀리초다.
WITH clock AS MATERIALIZED (
	SELECT clock_timestamp() AS now
), due_window AS MATERIALIZED (
	SELECT d.send_unit_id, d.next_attempt_at, d.id AS delivery_id
	FROM alarm_dispatch_deliveries d
	WHERE $1::INT > 0
	  AND /* ready_predicate */
	ORDER BY d.next_attempt_at ASC, d.id ASC
	LIMIT $1::INT * $4::INT
), unit_candidates AS (
	SELECT DISTINCT ON (send_unit_id)
		send_unit_id AS id, next_attempt_at, delivery_id
	FROM due_window
	ORDER BY send_unit_id, next_attempt_at ASC, delivery_id ASC
), locked_units AS (
	SELECT u.id, candidate.next_attempt_at, candidate.delivery_id,
		(
			SELECT count(d.id)
			FROM alarm_dispatch_deliveries d
			WHERE d.send_unit_id = u.id
			  AND d.status IN ('pending', 'retry')
		) AS delivery_count
	FROM unit_candidates candidate
	JOIN alarm_dispatch_send_units u ON u.id = candidate.id
	ORDER BY candidate.next_attempt_at ASC, candidate.delivery_id ASC
	LIMIT $1::INT
	FOR UPDATE OF u SKIP LOCKED
), ranked_units AS (
	SELECT id,
		row_number() OVER (ORDER BY next_attempt_at ASC, delivery_id ASC) AS ordinal,
		sum(delivery_count) OVER (ORDER BY next_attempt_at ASC, delivery_id ASC ROWS UNBOUNDED PRECEDING) AS cumulative_deliveries
	FROM locked_units
), next_units AS (
	SELECT id
	FROM ranked_units
	WHERE cumulative_deliveries <= $1::INT OR ordinal = 1
), picked AS (
	SELECT d.id
	FROM alarm_dispatch_deliveries d
	WHERE d.send_unit_id IN (SELECT id FROM next_units)
	  AND d.status IN ('pending', 'retry')
), updated AS (
	UPDATE alarm_dispatch_deliveries d
	SET status = 'leased',
		locked_by = $2,
		locked_at = clock.now,
		lock_expires_at = clock.now + ($3::BIGINT * INTERVAL '1 millisecond'),
		updated_at = clock.now
	FROM picked CROSS JOIN clock
	WHERE d.id = picked.id
	RETURNING d.id, d.event_id, d.room_id, d.dedupe_key, d.claim_keys, d.delivery_context,
		d.dispatch_group_key, d.send_unit_id, d.status, d.attempt_count, d.next_attempt_at,
		d.locked_by, d.locked_at, d.lock_expires_at, d.sending_started_at, d.sent_at,
		d.dlq_at, d.quarantined_at, d.cancelled_at, d.last_error_code, d.last_error,
		d.created_at, d.updated_at
)
SELECT d.id, d.event_id, d.room_id, d.dedupe_key, d.claim_keys, d.delivery_context,
	d.dispatch_group_key, d.send_unit_id, COALESCE(u.client_request_id, ''), d.status,
	d.attempt_count, d.next_attempt_at, d.locked_by, d.locked_at, d.lock_expires_at,
	d.sending_started_at, d.sent_at, d.dlq_at, d.quarantined_at, d.cancelled_at,
	d.last_error_code, d.last_error, d.created_at, d.updated_at
FROM updated d
LEFT JOIN alarm_dispatch_send_units u ON u.id = d.send_unit_id
ORDER BY d.next_attempt_at ASC, d.id ASC
