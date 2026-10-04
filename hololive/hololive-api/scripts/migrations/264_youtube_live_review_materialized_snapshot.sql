-- SQL 함수 inlining이 snapshot 생성식을 각 JSON 필드마다 다시 실행하지 않게 고정합니다.
-- 기존 사실 판정·영수증·권한·보존 정책은 유지하며 행 데이터는 변경하지 않습니다.
CREATE OR REPLACE FUNCTION public.youtube_live_review_current_receipt(p_video_id TEXT)
RETURNS TABLE (reviewed_at timestamptz)
LANGUAGE sql STABLE
AS $current$
WITH current_snapshot AS MATERIALIZED (
    SELECT jsonb_build_object(
        'session',to_jsonb(session),
        'head',CASE WHEN head.video_id IS NOT NULL THEN jsonb_build_object(
            'status',head.status,
            'last_upcoming_positive_at',head.last_upcoming_positive_at,
            'last_upcoming_positive_seen_at',head.last_upcoming_positive_seen_at,
            'last_live_positive_at',head.last_live_positive_at,
            'last_live_positive_seen_at',head.last_live_positive_seen_at,
            'last_end_evidence_at',head.last_end_evidence_at,
            'last_complete_absence_at',head.last_complete_absence_at,
            'last_absence_scheduled_for',head.last_absence_scheduled_for,
            'first_absence_scheduled_for',head.first_absence_scheduled_for,
            'second_absence_scheduled_for',head.second_absence_scheduled_for,
            'consecutive_absence_slots',head.consecutive_absence_slots,
            'last_absence_observation_id',head.last_absence_observation_id,
            'end_candidate_kind',head.end_candidate_kind,
            'end_candidate_observation_id',head.end_candidate_observation_id,
            'ended_at',head.ended_at,'end_reason',head.end_reason) END,
        'pending',to_jsonb(pending),'availability',to_jsonb(availability.availability)) AS snapshot,
        session.status = 'UPCOMING'
            AND (head.video_id IS NULL OR head.status = session.status)
            AND (session.lifecycle_origin <> 'observed' OR head.video_id IS NOT NULL)
            AND availability.video_id IS NOT NULL
            AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)
                >= availability.effective_at,false) AS reviewable
    FROM public.youtube_live_sessions session
    LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)
    LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)
    LEFT JOIN public.youtube_video_availability availability USING (video_id)
    WHERE session.video_id = p_video_id
), current_facts AS MATERIALIZED (
    SELECT public.youtube_live_review_semantic_facts(snapshot) AS facts, reviewable
    FROM current_snapshot
)
SELECT max(receipt.recorded_at)
FROM current_facts
JOIN public.youtube_live_review_receipts receipt ON receipt.video_id = p_video_id
WHERE current_facts.reviewable
  AND public.youtube_live_review_semantic_facts(receipt.original_snapshot) = current_facts.facts;
$current$;
REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipt(TEXT) FROM PUBLIC;
