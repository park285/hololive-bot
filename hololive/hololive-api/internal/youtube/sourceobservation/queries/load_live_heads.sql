SELECT video_id, status,
    last_upcoming_positive_at, last_upcoming_positive_seen_at,
    last_live_positive_at, last_live_positive_seen_at,
    last_end_evidence_at, last_complete_absence_at, last_absence_scheduled_for,
    consecutive_absence_slots, end_candidate_kind, end_candidate_observation_id,
    next_end_check_at, ended_at, end_reason,
    first_absence_scheduled_for, second_absence_scheduled_for,
    last_absence_observation_id, unresolvable_since,
    -- scalar를 먼저 잠근 뒤 부재 평가가 필요한 관측만 별도로 큰 배열을 읽습니다.
    -- 열이 NOT NULL이라 NULL은 "적재하지 않음"만 뜻하며 빈 배열과 구분합니다.
    CASE WHEN video_id = ANY($2::text[]) THEN NULL ELSE ignored_absence_scheduled_for END
        AS ignored_absence_scheduled_for
FROM youtube_live_reconciliation_heads
WHERE video_id = ANY($1::text[])
ORDER BY video_id
FOR UPDATE
