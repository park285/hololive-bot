INSERT INTO youtube_content_evidence_clocks (
    video_id,
    first_positive_effective_at,
    last_positive_effective_at,
    last_positive_received_at,
    last_positive_value_sha256,
    last_positive_scope_sha256,
    last_positive_coverage,
    last_negative_effective_at,
    last_negative_received_at,
    first_absence_scheduled_for,
    second_absence_scheduled_for,
    last_absence_observation_id,
    missing_since_effective_at,
    consecutive_absence_slots,
    withdrawn_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
ON CONFLICT (video_id) DO UPDATE
SET first_positive_effective_at = EXCLUDED.first_positive_effective_at,
    last_positive_effective_at = EXCLUDED.last_positive_effective_at,
    last_positive_received_at = EXCLUDED.last_positive_received_at,
    last_positive_value_sha256 = EXCLUDED.last_positive_value_sha256,
    last_positive_scope_sha256 = EXCLUDED.last_positive_scope_sha256,
    last_positive_coverage = EXCLUDED.last_positive_coverage,
    last_negative_effective_at = EXCLUDED.last_negative_effective_at,
    last_negative_received_at = EXCLUDED.last_negative_received_at,
    first_absence_scheduled_for = EXCLUDED.first_absence_scheduled_for,
    second_absence_scheduled_for = EXCLUDED.second_absence_scheduled_for,
    last_absence_observation_id = EXCLUDED.last_absence_observation_id,
    missing_since_effective_at = EXCLUDED.missing_since_effective_at,
    consecutive_absence_slots = EXCLUDED.consecutive_absence_slots,
    withdrawn_at = EXCLUDED.withdrawn_at,
    updated_at = NOW()
