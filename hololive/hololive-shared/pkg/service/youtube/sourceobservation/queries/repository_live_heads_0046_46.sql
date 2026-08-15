SELECT video_id, status,
    last_upcoming_positive_at, last_upcoming_positive_seen_at,
    last_live_positive_at, last_live_positive_seen_at,
    last_end_evidence_at, last_complete_absence_at, last_absence_scheduled_for,
    consecutive_absence_slots, end_candidate_kind, end_candidate_observation_id,
    next_end_check_at, ended_at, end_reason
FROM youtube_live_reconciliation_heads
WHERE video_id = ANY($1::text[])
FOR UPDATE
