SELECT video_id,
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
FROM youtube_content_evidence_clocks
WHERE video_id = ANY($1)
FOR UPDATE
