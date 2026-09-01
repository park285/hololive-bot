WITH completion_time AS (
    SELECT clock_timestamp() AS completed_at
)
UPDATE youtube_notification_delivery_ledger_state AS state
SET legacy_coverage_start_at = COALESCE(state.legacy_coverage_start_at, $1),
    coverage_verified_at = COALESCE(state.coverage_verified_at, completion_time.completed_at),
    completed_at = completion_time.completed_at,
    updated_at = completion_time.completed_at
FROM completion_time
WHERE state.singleton
  AND state.schema_version = $2
  AND state.completed_at IS NULL
RETURNING
    state.schema_version,
    state.delivery_high_water_id,
    state.outbox_high_water_id,
    state.delivery_cursor_id,
    state.delivery_verify_cursor_id,
    state.outbox_cursor_id,
    state.legacy_coverage_start_at,
    state.coverage_verified_at,
    state.started_at,
    state.completed_at,
    state.updated_at;
