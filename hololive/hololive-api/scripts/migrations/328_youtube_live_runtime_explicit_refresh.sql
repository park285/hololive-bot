-- 라이브 수명 모델을 writer의 명시 갱신으로 바꾸고 전역 MVCC guard 행을 advisory 잠금으로 대체한다.
-- 관측 writer는 shared 전역 잠금 뒤 원본을 쓰고 트랜잭션 끝에 변경 key로 refresh를 한 번 호출한다.
-- 증거 retention·수동 영수증·head 복구·data migration은 같은 key의 exclusive 잠금으로 모든 writer를 배제한다.
-- 순서가 의미를 가진다. 기존 guard로 writer를 배제한 뒤 트리거·함수를 바꾸고 마지막에 전체 key를 재계산한다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SELECT public.lock_youtube_live_runtime_state();

DO $triggers$
DECLARE
    source_table text;
BEGIN
    FOREACH source_table IN ARRAY ARRAY['youtube_live_sessions','youtube_live_reconciliation_heads',
        'youtube_live_pending_ends','youtube_video_availability','youtube_live_review_receipts','youtube_live_review_receipt_facts']
    LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_guard ON public.%I',source_table);
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_insert ON public.%I',source_table);
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_update ON public.%I',source_table);
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_delete ON public.%I',source_table);
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_truncate ON public.%I',source_table);
    END LOOP;
END
$triggers$;
DROP TRIGGER IF EXISTS youtube_live_runtime_guard ON public.source_observations;
DROP FUNCTION IF EXISTS public.refresh_youtube_live_runtime_state_statement();
DROP FUNCTION IF EXISTS public.guard_youtube_live_runtime_state();

-- 이름과 ACL을 유지해 315 retention 함수·운영 스크립트·기록 함수가 같은 배제 의미를 그대로 쓴다.
CREATE OR REPLACE FUNCTION public.lock_youtube_live_runtime_state()
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $lock$
BEGIN
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('youtube_live_runtime_global', 0));
END
$lock$;

CREATE OR REPLACE FUNCTION public.lock_youtube_live_runtime_state_shared()
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $shared$
BEGIN
    PERFORM pg_catalog.pg_advisory_xact_lock_shared(pg_catalog.hashtextextended('youtube_live_runtime_global', 0));
END
$shared$;
REVOKE ALL ON FUNCTION public.lock_youtube_live_runtime_state_shared() FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.refresh_youtube_live_runtime_state(p_video_ids text[])
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $refresh$
BEGIN
    -- 잠금 대기 뒤 문장이 새 snapshot으로 앞선 커밋을 읽어야 하므로 고정 snapshot 호출은 거부한다.
    IF pg_catalog.current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'live runtime refresh requires read committed transaction';
    END IF;
    IF p_video_ids IS NULL OR pg_catalog.cardinality(p_video_ids) = 0 THEN
        RETURN;
    END IF;
    -- 같은 영상의 갱신을 커밋까지 직렬화한다. 영상 key를 1024개 bucket으로 접어 전체 재계산도 잠금 표를 고갈시키지 않으며,
    -- 같은 bucket의 다른 영상은 커밋까지 기다릴 뿐이다. 한 문장에서 bucket 순서로 잡아 writer 사이 교착을 만들지 않는다.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtext('youtube_live_runtime_video'), ordered.bucket)
    FROM (SELECT DISTINCT (pg_catalog.hashtextextended(input.video_id, 0) & 1023)::integer AS bucket
          FROM pg_catalog.unnest(p_video_ids) AS input(video_id)
          WHERE input.video_id IS NOT NULL
          ORDER BY 1) AS ordered;

    -- VOLATILE의 문장별 snapshot으로 잠금 대기 중 커밋된 원본도 읽는다.
    DELETE FROM public.youtube_live_runtime_state state
    WHERE state.video_id=ANY(p_video_ids)
      AND NOT EXISTS (SELECT 1 FROM public.youtube_live_sessions session
          WHERE session.video_id=state.video_id AND session.status IN ('LIVE','UPCOMING'))
      AND NOT EXISTS (SELECT 1 FROM public.youtube_live_reconciliation_heads head
          WHERE head.video_id=state.video_id AND head.status IN ('LIVE','UPCOMING'));

    INSERT INTO public.youtube_live_runtime_state AS state
        (video_id,channel_id,session_status,head_status,scheduled_start_time,lifecycle_origin,
         last_live_positive_at,last_live_positive_seen_at,last_upcoming_positive_at,last_upcoming_positive_seen_at,
         availability_at,availability_seen_at,unresolvable_since,reviewed_at)
    SELECT ids.video_id,session.channel_id,session.status,head.status,session.scheduled_start_time,session.lifecycle_origin,
           head.last_live_positive_at,head.last_live_positive_seen_at,head.last_upcoming_positive_at,head.last_upcoming_positive_seen_at,
           availability.effective_at,availability.observed_at,head.unresolvable_since,review.reviewed_at
    FROM (SELECT DISTINCT unnest(p_video_ids) AS video_id) ids
    LEFT JOIN public.youtube_live_sessions session USING (video_id)
    LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)
    LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)
    LEFT JOIN public.youtube_video_availability availability USING (video_id)
    LEFT JOIN LATERAL (
        SELECT max(receipt.recorded_at) AS reviewed_at
        FROM public.youtube_live_review_receipt_facts receipt
        WHERE receipt.video_id=ids.video_id
          AND session.status='UPCOMING'
          AND (head.video_id IS NULL OR head.status=session.status)
          AND (session.lifecycle_origin<>'observed' OR head.video_id IS NOT NULL)
          AND availability.video_id IS NOT NULL
          AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)>=availability.effective_at,false)
          AND receipt.semantic_value=public.youtube_live_review_source_value(
              session.video_id,session.channel_id,session.status,session.lifecycle_origin,
              session.title,session.topic_id,session.is_premiere,session.scheduled_start_time,
              session.started_at,session.ended_at,session.live_first_seen_at,head.video_id,
              head.status,head.last_upcoming_positive_at,head.last_upcoming_positive_seen_at,head.last_live_positive_at,
              head.last_live_positive_seen_at,head.last_end_evidence_at,head.last_complete_absence_at,head.last_absence_scheduled_for,
              head.first_absence_scheduled_for,head.second_absence_scheduled_for,head.consecutive_absence_slots,head.last_absence_observation_id,
              head.end_candidate_kind,head.end_candidate_observation_id,head.ended_at,head.end_reason,
              pending.video_id,pending.channel_id,pending.kind,pending.observation_id,
              pending.effective_at,pending.scheduled_for,pending.ended_at,pending.negative_eligible,
              pending.scope_covers,availability.availability)
    ) review ON true
    WHERE session.status IN ('LIVE','UPCOMING') OR head.status IN ('LIVE','UPCOMING')
    ORDER BY ids.video_id COLLATE "C"
    ON CONFLICT (video_id) DO UPDATE SET
        channel_id=EXCLUDED.channel_id,session_status=EXCLUDED.session_status,head_status=EXCLUDED.head_status,
        scheduled_start_time=EXCLUDED.scheduled_start_time,lifecycle_origin=EXCLUDED.lifecycle_origin,
        last_live_positive_at=EXCLUDED.last_live_positive_at,last_live_positive_seen_at=EXCLUDED.last_live_positive_seen_at,
        last_upcoming_positive_at=EXCLUDED.last_upcoming_positive_at,last_upcoming_positive_seen_at=EXCLUDED.last_upcoming_positive_seen_at,
        availability_at=EXCLUDED.availability_at,availability_seen_at=EXCLUDED.availability_seen_at,
        unresolvable_since=EXCLUDED.unresolvable_since,reviewed_at=EXCLUDED.reviewed_at
    WHERE state IS DISTINCT FROM EXCLUDED;
END
$refresh$;

-- invoker 권한·입력 검증·snapshot CAS와 기존 ACL은 유지한다. exclusive 잠금 뒤의 FOR UPDATE·CAS·INSERT·refresh가
-- 모두 최신 커밋을 읽도록 READ COMMITTED를 요구한다.
CREATE OR REPLACE FUNCTION public.record_youtube_live_review(
    p_receipt_id uuid, p_video_id TEXT, p_expected_sha256 TEXT, p_operator_id TEXT, p_reason TEXT
) RETURNS void LANGUAGE plpgsql
AS $record$
DECLARE
    snapshot record;
BEGIN
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'live review requires read committed transaction';
    END IF;
    IF p_receipt_id IS NULL OR p_video_id IS NULL OR length(p_video_id) NOT BETWEEN 1 AND 128
       OR p_expected_sha256 IS NULL OR p_expected_sha256 !~ '^[0-9a-f]{64}$'
       OR p_operator_id IS NULL OR length(p_operator_id) NOT BETWEEN 1 AND 128
       OR p_operator_id <> btrim(p_operator_id) OR p_operator_id ~ '[[:cntrl:]]'
       OR p_reason IS NULL OR length(p_reason) NOT BETWEEN 1 AND 1024
       OR p_reason <> btrim(p_reason) OR p_reason ~ '[[:cntrl:]]' THEN
        RAISE EXCEPTION 'invalid live review request';
    END IF;
    PERFORM public.lock_youtube_live_runtime_state();
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
    PERFORM public.refresh_youtube_live_runtime_state(ARRAY[p_video_id]);
END
$record$;

DROP TABLE IF EXISTS public.youtube_live_runtime_state_guard;

DO $roles$
DECLARE
    recording_role text;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.refresh_youtube_live_runtime_state(text[]),
            public.lock_youtube_live_runtime_state_shared() TO hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='hololive_scraper') THEN
        REVOKE ALL ON FUNCTION public.lock_youtube_live_runtime_state_shared() FROM hololive_scraper;
    END IF;
    -- invoker 기록 함수가 SECURITY DEFINER refresh를 부르므로 이미 기록을 허가받은 역할에만 실행 권한을 보충한다.
    FOR recording_role IN
        SELECT DISTINCT role.rolname
        FROM pg_catalog.pg_proc procedure
        CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(procedure.proacl,pg_catalog.acldefault('f',procedure.proowner))) privilege
        JOIN pg_catalog.pg_roles role ON role.oid=privilege.grantee
        WHERE procedure.oid='public.record_youtube_live_review(uuid,text,text,text,text)'::regprocedure
          AND privilege.privilege_type='EXECUTE'
    LOOP
        EXECUTE format('GRANT EXECUTE ON FUNCTION public.refresh_youtube_live_runtime_state(text[]) TO %I',recording_role);
    END LOOP;
END
$roles$;

COMMENT ON TABLE public.youtube_live_runtime_state IS
    'session 또는 head가 LIVE/UPCOMING인 영상의 동기 읽기 모델. ENDED 상대 행과 session 없는 head도 보존한다. '
    'reviewed_at은 현재 검토 가능하고 정확한 310 의미 사실이 일치하는 영수증 중 최대 기록 시각이다. '
    '원본 writer는 같은 READ COMMITTED 트랜잭션 끝에서 변경 key로 refresh_youtube_live_runtime_state()를 호출한다. '
    '관측 writer는 lock_youtube_live_runtime_state_shared(), 배제 경로는 lock_youtube_live_runtime_state()를 원본 잠금보다 먼저 잡는다.';

-- 트리거 기간의 정합을 그대로 넘기고 drift 0에서 시작한다.
SELECT public.refresh_youtube_live_runtime_state(ARRAY(
    SELECT video_id FROM public.youtube_live_runtime_state
    UNION SELECT video_id FROM public.youtube_live_sessions
    UNION SELECT video_id FROM public.youtube_live_reconciliation_heads));
COMMIT;
