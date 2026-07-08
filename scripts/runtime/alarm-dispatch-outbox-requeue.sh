#!/usr/bin/env bash
set -euo pipefail

usage="usage: alarm-dispatch-outbox-requeue.sh <delivery-id> <operator-id> <reason> force_duplicate_risk_ack=true"

id="${1:?$usage}"
operator_id="${2:?$usage}"
reason="${3:?$usage}"
ack="${4:?$usage}"

if [[ "$ack" != "force_duplicate_risk_ack=true" ]]; then
  echo "$usage" >&2
  exit 64
fi

psql "${DATABASE_URL:?set DATABASE_URL}" -v ON_ERROR_STOP=1 \
  -v id="$id" \
  -v operator_id="$operator_id" \
  -v reason="$reason" <<'SQL'
WITH target AS (
    SELECT id, status
    FROM alarm_dispatch_deliveries
    WHERE id = :'id'
      AND status IN ('dlq', 'quarantined')
    FOR UPDATE
), updated AS (
    UPDATE alarm_dispatch_deliveries d
    SET status = 'retry',
        next_attempt_at = now(),
        locked_by = NULL,
        locked_at = NULL,
        lock_expires_at = NULL,
        last_error = concat('[manual requeue] ', d.last_error),
        updated_at = now()
    FROM target
    WHERE d.id = target.id
    RETURNING d.id, target.status AS from_status, d.status AS to_status
), audit AS (
    INSERT INTO alarm_dispatch_admin_actions (
        delivery_id, action, operator_id, reason, from_status, to_status, duplicate_risk_ack
    )
    SELECT id, 'manual_requeue', :'operator_id', :'reason', from_status, to_status, TRUE
    FROM updated
    RETURNING id
)
SELECT count(id) AS requeued_rows
FROM audit;
SQL
