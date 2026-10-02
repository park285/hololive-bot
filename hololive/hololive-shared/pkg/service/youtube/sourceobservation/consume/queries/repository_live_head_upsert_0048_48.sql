INSERT INTO youtube_live_reconciliation_heads (
    video_id, status,
    last_upcoming_positive_at, last_upcoming_positive_seen_at,
    last_live_positive_at, last_live_positive_seen_at,
    last_end_evidence_at, last_complete_absence_at, last_absence_scheduled_for,
    consecutive_absence_slots, end_candidate_kind, end_candidate_observation_id,
    next_end_check_at, ended_at, end_reason,
    first_absence_scheduled_for, second_absence_scheduled_for,
    last_absence_observation_id, ignored_absence_scheduled_for
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    $16, $17, $18, COALESCE($19::timestamptz[], '{}')
)
ON CONFLICT (video_id) DO UPDATE SET
    status = excluded.status,
    last_upcoming_positive_at = excluded.last_upcoming_positive_at,
    last_upcoming_positive_seen_at = excluded.last_upcoming_positive_seen_at,
    last_live_positive_at = excluded.last_live_positive_at,
    last_live_positive_seen_at = excluded.last_live_positive_seen_at,
    last_end_evidence_at = excluded.last_end_evidence_at,
    last_complete_absence_at = excluded.last_complete_absence_at,
    last_absence_scheduled_for = excluded.last_absence_scheduled_for,
    consecutive_absence_slots = excluded.consecutive_absence_slots,
    end_candidate_kind = excluded.end_candidate_kind,
    end_candidate_observation_id = excluded.end_candidate_observation_id,
    next_end_check_at = excluded.next_end_check_at,
    ended_at = excluded.ended_at,
    end_reason = excluded.end_reason,
    first_absence_scheduled_for = excluded.first_absence_scheduled_for,
    second_absence_scheduled_for = excluded.second_absence_scheduled_for,
    last_absence_observation_id = excluded.last_absence_observation_id,
    ignored_absence_scheduled_for = excluded.ignored_absence_scheduled_for,
    updated_at = NOW()
