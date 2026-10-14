-- 현재 의미 사실은 typed 열에서 직접 만든다. NULL 행·NULL 열과 epoch 정밀도는 262의 JSON 사실과 같다.
-- 영수증 사실 정의·기록 CAS·append-only 원본은 바꾸지 않으므로 저장 사실을 다시 채우지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE OR REPLACE FUNCTION public.youtube_live_review_current_facts(p_video_ids TEXT[])
RETURNS TABLE (video_id TEXT, facts jsonb, reviewable boolean)
LANGUAGE sql STABLE
AS $facts$
SELECT session.video_id,
       jsonb_build_object(
           'session',jsonb_build_object(
               'video_id',session.video_id,
               'channel_id',session.channel_id,
               'status',session.status,
               'lifecycle_origin',session.lifecycle_origin,
               'title',session.title,
               'topic_id',session.topic_id,
               'is_premiere',session.is_premiere,
               'scheduled_start_time',extract(epoch FROM session.scheduled_start_time),
               'started_at',extract(epoch FROM session.started_at),
               'ended_at',extract(epoch FROM session.ended_at),
               'live_first_seen_at',extract(epoch FROM session.live_first_seen_at)),
           'head',CASE WHEN head.video_id IS NOT NULL THEN jsonb_build_object(
               'status',head.status,
               'last_upcoming_positive_at',extract(epoch FROM head.last_upcoming_positive_at),
               'last_upcoming_positive_seen_at',extract(epoch FROM head.last_upcoming_positive_seen_at),
               'last_live_positive_at',extract(epoch FROM head.last_live_positive_at),
               'last_live_positive_seen_at',extract(epoch FROM head.last_live_positive_seen_at),
               'last_end_evidence_at',extract(epoch FROM head.last_end_evidence_at),
               'last_complete_absence_at',extract(epoch FROM head.last_complete_absence_at),
               'last_absence_scheduled_for',extract(epoch FROM head.last_absence_scheduled_for),
               'first_absence_scheduled_for',extract(epoch FROM head.first_absence_scheduled_for),
               'second_absence_scheduled_for',extract(epoch FROM head.second_absence_scheduled_for),
               'consecutive_absence_slots',head.consecutive_absence_slots,
               'last_absence_observation_id',head.last_absence_observation_id,
               'end_candidate_kind',head.end_candidate_kind,
               'end_candidate_observation_id',head.end_candidate_observation_id,
               'ended_at',extract(epoch FROM head.ended_at),
               'end_reason',head.end_reason) END,
           'pending',CASE WHEN pending.video_id IS NOT NULL THEN jsonb_build_object(
               'channel_id',pending.channel_id,
               'kind',pending.kind,
               'observation_id',pending.observation_id,
               'effective_at',extract(epoch FROM pending.effective_at),
               'scheduled_for',extract(epoch FROM pending.scheduled_for),
               'ended_at',extract(epoch FROM pending.ended_at),
               'negative_eligible',pending.negative_eligible,
               'scope_covers',pending.scope_covers) END,
           'availability',availability.availability),
       session.status = 'UPCOMING'
           AND (head.video_id IS NULL OR head.status = session.status)
           AND (session.lifecycle_origin <> 'observed' OR head.video_id IS NOT NULL)
           AND availability.video_id IS NOT NULL
           AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)
               >= availability.effective_at,false)
FROM public.youtube_live_sessions session
LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)
LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)
LEFT JOIN public.youtube_video_availability availability USING (video_id)
WHERE session.video_id = ANY(p_video_ids);
$facts$;
REVOKE ALL ON FUNCTION public.youtube_live_review_current_facts(TEXT[]) FROM PUBLIC;

-- 입력 집합을 한 번 비교한다. 영수증 없는 영상의 사실 계산과 영상별 영수증 표 재탐색을 피한다.
CREATE OR REPLACE FUNCTION public.youtube_live_review_current_receipts(p_video_ids TEXT[])
RETURNS TABLE (video_id TEXT, reviewed_at timestamptz)
LANGUAGE sql STABLE
AS $receipts$
WITH receipts AS MATERIALIZED (
    SELECT receipt.video_id,receipt.recorded_at,receipt.facts
    FROM public.youtube_live_review_receipt_facts receipt
    WHERE receipt.video_id = ANY(p_video_ids)
), current_input AS MATERIALIZED (
    -- SubLink를 함수 인수에서 분리해 typed 사실 조회도 호출 문장 안에서 인라인되게 합니다.
    SELECT ARRAY(SELECT DISTINCT receipt.video_id FROM receipts receipt) AS video_ids
), current_facts AS MATERIALIZED (
    SELECT current.*
    FROM current_input input
    CROSS JOIN LATERAL public.youtube_live_review_current_facts(input.video_ids) current
)
SELECT current_facts.video_id,max(receipt.recorded_at)
FROM current_facts
JOIN receipts receipt ON receipt.video_id = current_facts.video_id AND receipt.facts = current_facts.facts
WHERE current_facts.reviewable
GROUP BY current_facts.video_id;
$receipts$;
REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipts(TEXT[]) FROM PUBLIC;

-- 단건 호출도 같은 집합 판정을 쓰되, 없는 영상·불일치에 NULL 한 행을 반환하는 계약을 보존한다.
CREATE OR REPLACE FUNCTION public.youtube_live_review_current_receipt(p_video_id TEXT)
RETURNS TABLE (reviewed_at timestamptz)
LANGUAGE sql STABLE
AS $current$
SELECT max(receipt.reviewed_at)
FROM public.youtube_live_review_current_receipts(ARRAY[p_video_id]) receipt;
$current$;
REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipt(TEXT) FROM PUBLIC;

DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.youtube_live_review_current_facts(TEXT[]),
            public.youtube_live_review_current_receipts(TEXT[]),
            public.youtube_live_review_current_receipt(TEXT) TO hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON FUNCTION public.youtube_live_review_current_facts(TEXT[]),
            public.youtube_live_review_current_receipts(TEXT[]),
            public.youtube_live_review_current_receipt(TEXT) FROM hololive_scraper;
    END IF;
END
$roles$;

COMMIT;
