-- 기존 영수증의 큰 진단 배열을 비교 전에 한 번만 제거합니다.
-- 의미 사실 함수가 필드마다 원본 TOAST를 반복해서 펼치지 않도록 축약 문서를 고정합니다.
-- 원본 영수증·현재 사실·검토 가능 조건·함수 권한은 유지합니다.
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
), receipt_snapshots AS MATERIALIZED (
    SELECT recorded_at,
           CASE WHEN jsonb_typeof(original_snapshot->'head') = 'object'
                THEN jsonb_set(original_snapshot, '{head}',
                    (original_snapshot->'head') - 'ignored_absence_scheduled_for')
                ELSE original_snapshot
           END AS snapshot
    FROM public.youtube_live_review_receipts
    WHERE video_id = p_video_id
), receipt_facts AS MATERIALIZED (
    SELECT recorded_at, public.youtube_live_review_semantic_facts(snapshot) AS facts
    FROM receipt_snapshots
)
SELECT max(receipt.recorded_at)
FROM current_facts
CROSS JOIN receipt_facts receipt
WHERE current_facts.reviewable
  AND receipt.facts = current_facts.facts;
$current$;
REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipt(TEXT) FROM PUBLIC;
