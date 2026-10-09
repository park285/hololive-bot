-- 2026-10-07 서울 운영 pg_stat_statements: projection guard 아래에서 5초마다 도는 live_check_videos.sql이 평균 25.7 ms로
-- 앱 DB CPU의 8.5%였고, EXPLAIN ANALYZE 27.3 ms 중 24.5 ms가 reviewed_videos였다. 그 약 73%가 영수증 쪽 의미 사실
-- 재계산(원본 TOAST 해제·jsonb_set·youtube_live_review_semantic_facts)이었다.
-- 영수증은 append-only(246 거부 trigger)라 영수증 쪽 의미 사실도 기록 뒤 바뀌지 않는다. 그래서 기록 시 한 번 계산해 저장하고
-- youtube_live_review_current_receipt는 저장값과 비교한다. 270의 현재 원본 쪽 계산·검토 가능 조건·반환 형식은 글자 그대로
-- 유지하므로 판정은 같다. 영수증 원본·기존 권한·보존 정책은 바꾸지 않는다.
-- 순서는 표 → trigger → backfill → 함수 교체이고 한 트랜잭션으로 적용한다. FK와 trigger 생성은 영수증 표에
-- SHARE ROW EXCLUSIVE만 잡아 커밋까지 영수증 기록만 막고 조회는 막지 않는다. 기록이 막힌 동안 backfill이 기존 영수증을
-- 모두 보므로 trigger와 backfill 사이에 빠지는 영수증이 없다.
BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE TABLE IF NOT EXISTS public.youtube_live_review_receipt_facts (
    receipt_id uuid PRIMARY KEY REFERENCES public.youtube_live_review_receipts (receipt_id),
    video_id TEXT NOT NULL,
    recorded_at timestamptz NOT NULL,
    facts jsonb NOT NULL,
    -- 영상별 판정 조회 인덱스를 빈 새 표와 함께 만든다. receipt_id가 PK라 유일성 의미는 더하지 않는다.
    CONSTRAINT uq_youtube_live_review_receipt_facts_video UNIQUE (video_id, receipt_id)
);
COMMENT ON TABLE public.youtube_live_review_receipt_facts IS
    '검토 영수증 기록 시 youtube_live_review_receipt_semantic_facts(original_snapshot)로 고정한 영수증 쪽 의미 사실이다. '
    '영수증이 append-only라 이 행도 변경·삭제를 거부한다. youtube_live_review_semantic_facts 정의를 바꾸는 migration은 '
    '같은 파일에서 이 표를 TRUNCATE한 뒤 모든 영수증으로 다시 채워야 한다. 다시 채우지 않으면 기존 영수증이 현재 사실과 '
    '맞지 않아 검토가 다시 열린다.';
REVOKE ALL ON TABLE public.youtube_live_review_receipt_facts FROM PUBLIC;

-- 270 receipt_snapshots·receipt_facts와 같은 식이다. head의 무시한 부재 slot 배열은 의미 사실이 아니므로 먼저 제거해
-- 큰 원본을 필드마다 다시 펼치지 않는다. MATERIALIZED는 축약 문서를 한 번만 만들게 고정한다. trigger와 backfill이 함께 쓴다.
CREATE OR REPLACE FUNCTION public.youtube_live_review_receipt_semantic_facts(p_snapshot jsonb)
RETURNS jsonb
LANGUAGE sql STABLE
AS $receipt_facts$
WITH receipt_snapshot AS MATERIALIZED (
    SELECT CASE WHEN jsonb_typeof(p_snapshot->'head') = 'object'
                THEN jsonb_set(p_snapshot, '{head}',
                    (p_snapshot->'head') - 'ignored_absence_scheduled_for')
                ELSE p_snapshot
           END AS snapshot
)
SELECT public.youtube_live_review_semantic_facts(snapshot) FROM receipt_snapshot;
$receipt_facts$;
REVOKE ALL ON FUNCTION public.youtube_live_review_receipt_semantic_facts(jsonb) FROM PUBLIC;

-- 영수증 INSERT와 같은 트랜잭션에서 영수증 쪽 의미 사실을 고정한다. 기록 역할의 권한으로 실행하므로
-- 새 기록 역할에는 이 표의 INSERT 권한도 함께 줘야 한다. 같은 receipt_id가 이미 있으면 숨기지 않고 실패한다.
CREATE OR REPLACE FUNCTION public.store_youtube_live_review_receipt_facts()
RETURNS trigger LANGUAGE plpgsql
AS $store$
BEGIN
    INSERT INTO public.youtube_live_review_receipt_facts (receipt_id,video_id,recorded_at,facts)
    VALUES (NEW.receipt_id,NEW.video_id,NEW.recorded_at,
            public.youtube_live_review_receipt_semantic_facts(NEW.original_snapshot));
    RETURN NULL;
END
$store$;
REVOKE ALL ON FUNCTION public.store_youtube_live_review_receipt_facts() FROM PUBLIC;
-- DROP TRIGGER는 영수증 표 조회까지 막는 ACCESS EXCLUSIVE를 잡으므로 OR REPLACE로 멱등하게 만든다.
CREATE OR REPLACE TRIGGER youtube_live_review_receipts_store_facts
    AFTER INSERT ON public.youtube_live_review_receipts
    FOR EACH ROW EXECUTE FUNCTION public.store_youtube_live_review_receipt_facts();
-- 영수증과 같은 거부 함수를 쓴다. 의미 사실 정의 변경 때 다시 채우는 TRUNCATE는 row trigger 대상이 아니다.
CREATE OR REPLACE TRIGGER youtube_live_review_receipt_facts_immutable
    BEFORE UPDATE OR DELETE ON public.youtube_live_review_receipt_facts
    FOR EACH ROW EXECUTE FUNCTION public.reject_youtube_live_review_receipt_change();

-- 기존 영수증은 trigger와 같은 식으로 한 번 채운다. 재적용 때는 이미 채운 행을 건너뛴다.
INSERT INTO public.youtube_live_review_receipt_facts (receipt_id,video_id,recorded_at,facts)
SELECT receipt_id,video_id,recorded_at,public.youtube_live_review_receipt_semantic_facts(original_snapshot)
FROM public.youtube_live_review_receipts
ON CONFLICT (receipt_id) DO NOTHING;

-- current_snapshot·current_facts는 270과 같다. 영수증 쪽만 기록 시 저장한 의미 사실을 읽는다.
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
JOIN public.youtube_live_review_receipt_facts receipt
  ON receipt.video_id = p_video_id AND receipt.facts = current_facts.facts
WHERE current_facts.reviewable;
$current$;
REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipt(TEXT) FROM PUBLIC;

-- migrator 기본 ACL은 새 표에 runtime 쓰기 권한(init-db 기준 CRUD)을 줄 수 있다. 다른 기본 ACL이 있어도 결과가
-- 같도록 runtime은 판정 함수 실행에 필요한 조회만 남기고, 검토 판정을 읽지 않는 scraper의 권한은 걷는다.
-- 새 함수는 PUBLIC 실행 권한만 걷고 기록 역할이 trigger로 실행한다. 판정 함수의 기존 runtime 실행 권한(262)은
-- 다시 선언한다.
DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON TABLE public.youtube_live_review_receipt_facts FROM hololive_runtime;
        GRANT SELECT ON TABLE public.youtube_live_review_receipt_facts TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.youtube_live_review_current_receipt(TEXT) TO hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TABLE public.youtube_live_review_receipt_facts FROM hololive_scraper;
        REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipt(TEXT) FROM hololive_scraper;
    END IF;
END
$roles$;

COMMIT;
