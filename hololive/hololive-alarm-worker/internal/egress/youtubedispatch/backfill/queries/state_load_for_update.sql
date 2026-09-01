SELECT
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
    updated_at
FROM youtube_notification_delivery_ledger_state
WHERE singleton
FOR UPDATE;
