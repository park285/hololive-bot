INSERT INTO youtube_notification_delivery_ledger_state (
    singleton,
    schema_version,
    delivery_high_water_id,
    outbox_high_water_id,
    delivery_cursor_id,
    delivery_verify_cursor_id,
    outbox_cursor_id,
    started_at,
    updated_at
) VALUES (true, $1, $2, $3, 0, 0, 0, $4, $4)
RETURNING
    schema_version,
    delivery_high_water_id,
    outbox_high_water_id,
    delivery_cursor_id,
    delivery_verify_cursor_id,
    outbox_cursor_id,
    legacy_coverage_start_at,
    coverage_verified_at,
    started_at,
    completed_at,
    updated_at;
