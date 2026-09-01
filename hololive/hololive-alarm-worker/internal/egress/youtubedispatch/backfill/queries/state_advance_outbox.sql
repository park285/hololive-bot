UPDATE youtube_notification_delivery_ledger_state AS state
SET outbox_cursor_id = $1,
    updated_at = clock_timestamp()
WHERE state.singleton
  AND state.schema_version = $2
  AND state.outbox_cursor_id = $3
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
