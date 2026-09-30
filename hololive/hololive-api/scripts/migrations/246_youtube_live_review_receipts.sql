-- 수명 사실과 별개인 운영 검토를 보존한다. runtime은 읽기만 가능하다.
CREATE TABLE IF NOT EXISTS public.youtube_live_review_receipts (
    receipt_id uuid PRIMARY KEY,
    video_id TEXT NOT NULL,
    snapshot_sha256 TEXT NOT NULL CHECK (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
    original_snapshot jsonb NOT NULL CHECK (octet_length(original_snapshot::text) <= 262144),
    evidence_refs jsonb NOT NULL,
    disposition TEXT NOT NULL CHECK (disposition = 'closed_unresolved'),
    operator_id TEXT NOT NULL CHECK (length(operator_id) BETWEEN 1 AND 128 AND operator_id = btrim(operator_id) AND operator_id !~ '[[:cntrl:]]'),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 1024 AND reason = btrim(reason) AND reason !~ '[[:cntrl:]]'),
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (video_id, snapshot_sha256)
);
REVOKE ALL ON public.youtube_live_review_receipts FROM PUBLIC;

-- 기존 closeout 원장처럼 변경·삭제를 거부한다. canonical을 자동 보정하는 trigger가 아니다.
CREATE OR REPLACE FUNCTION public.reject_youtube_live_review_receipt_change()
RETURNS trigger LANGUAGE plpgsql
AS $immutable$
BEGIN
    RAISE EXCEPTION 'live review receipts are append-only';
END
$immutable$;
REVOKE ALL ON FUNCTION public.reject_youtube_live_review_receipt_change() FROM PUBLIC;
DROP TRIGGER IF EXISTS youtube_live_review_receipts_immutable ON public.youtube_live_review_receipts;
CREATE TRIGGER youtube_live_review_receipts_immutable
    BEFORE UPDATE OR DELETE ON public.youtube_live_review_receipts
    FOR EACH ROW EXECUTE FUNCTION public.reject_youtube_live_review_receipt_change();

CREATE OR REPLACE FUNCTION public.youtube_live_review_snapshot(p_video_id TEXT)
RETURNS TABLE (original_snapshot jsonb, snapshot_sha256 TEXT, evidence_refs jsonb, reviewable boolean)
LANGUAGE sql STABLE
AS $snapshot$
    WITH facts AS (
        SELECT jsonb_build_object('session',to_jsonb(session),'head',to_jsonb(head),
                   'pending',to_jsonb(pending),'availability',to_jsonb(availability)) AS snapshot,
               jsonb_build_object('availability_observation_id',availability.observation_id,
                   'availability_evidence_sha256',availability.evidence_sha256,
                   'pending_observation_id',pending.observation_id) AS refs,
               session.status = 'UPCOMING'
                   AND (head.video_id IS NULL OR head.status = session.status)
                   AND (session.lifecycle_origin <> 'observed' OR head.video_id IS NOT NULL)
                   -- 가용성 PUBLIC도 수명 미상일 수 있다. 현재 확인보다 새롭거나
                   -- 같은 positive가 있으면 확인된 UPCOMING을 unresolved로 닫지 않는다.
                   AND availability.video_id IS NOT NULL
                   AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)
                       >= availability.effective_at,false) AS reviewable
        FROM public.youtube_live_sessions session
        LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)
        LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)
        LEFT JOIN public.youtube_video_availability availability USING (video_id)
        WHERE session.video_id = p_video_id
    )
    SELECT snapshot, encode(sha256(convert_to(snapshot::TEXT,'UTF8')),'hex'), refs,
           COALESCE(reviewable,false) FROM facts
$snapshot$;
REVOKE ALL ON FUNCTION public.youtube_live_review_snapshot(TEXT) FROM PUBLIC;

-- 원본 잠금 순서는 수명 consumer와 같다. 커밋 오류·결과 불명은 재시도하지
-- 않고 receipt_id를 조회한다. 원본 status/head/dispatch 원장을 갱신하지 않는다.
CREATE OR REPLACE FUNCTION public.record_youtube_live_review(
    p_receipt_id uuid, p_video_id TEXT, p_expected_sha256 TEXT, p_operator_id TEXT, p_reason TEXT
) RETURNS void LANGUAGE plpgsql
AS $record$
DECLARE
    snapshot record;
BEGIN
    IF current_setting('transaction_isolation') <> 'serializable' THEN
        RAISE EXCEPTION 'live review requires serializable transaction';
    END IF;
    IF p_receipt_id IS NULL OR p_video_id IS NULL OR length(p_video_id) NOT BETWEEN 1 AND 128
       OR p_expected_sha256 IS NULL OR p_expected_sha256 !~ '^[0-9a-f]{64}$'
       OR p_operator_id IS NULL OR length(p_operator_id) NOT BETWEEN 1 AND 128
       OR p_operator_id <> btrim(p_operator_id) OR p_operator_id ~ '[[:cntrl:]]'
       OR p_reason IS NULL OR length(p_reason) NOT BETWEEN 1 AND 1024
       OR p_reason <> btrim(p_reason) OR p_reason ~ '[[:cntrl:]]' THEN
        RAISE EXCEPTION 'invalid live review request';
    END IF;
    PERFORM video_id FROM public.youtube_live_sessions WHERE video_id = p_video_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'reviewed live session missing';
    END IF;
    PERFORM video_id FROM public.youtube_live_reconciliation_heads WHERE video_id = p_video_id FOR UPDATE;
    PERFORM video_id FROM public.youtube_live_pending_ends WHERE video_id = p_video_id FOR UPDATE;
    PERFORM video_id FROM public.youtube_video_availability WHERE video_id = p_video_id FOR UPDATE;
    SELECT reviewed.original_snapshot,reviewed.snapshot_sha256,reviewed.evidence_refs,reviewed.reviewable
    INTO snapshot FROM public.youtube_live_review_snapshot(p_video_id) reviewed;
    IF snapshot.snapshot_sha256 IS DISTINCT FROM p_expected_sha256 OR NOT snapshot.reviewable THEN
        RAISE EXCEPTION 'reviewed live snapshot changed or is not unresolved';
    END IF;
    INSERT INTO public.youtube_live_review_receipts
        (receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason)
    VALUES (p_receipt_id,p_video_id,snapshot.snapshot_sha256,snapshot.original_snapshot,
            snapshot.evidence_refs,'closed_unresolved',p_operator_id,p_reason);
END
$record$;
REVOKE ALL ON FUNCTION public.record_youtube_live_review(uuid,TEXT,TEXT,TEXT,TEXT) FROM PUBLIC;

DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE ALL ON public.youtube_live_review_receipts FROM hololive_runtime;
        GRANT SELECT ON public.youtube_live_review_receipts TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.youtube_live_review_snapshot(TEXT) TO hololive_runtime;
        REVOKE ALL ON FUNCTION public.record_youtube_live_review(uuid,TEXT,TEXT,TEXT,TEXT) FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON public.youtube_live_review_receipts FROM hololive_scraper;
        REVOKE ALL ON FUNCTION public.youtube_live_review_snapshot(TEXT) FROM hololive_scraper;
        REVOKE ALL ON FUNCTION public.record_youtube_live_review(uuid,TEXT,TEXT,TEXT,TEXT) FROM hololive_scraper;
    END IF;
END
$roles$;
