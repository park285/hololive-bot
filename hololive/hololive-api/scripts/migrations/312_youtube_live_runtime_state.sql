-- 수명 소비자가 읽는 좁은 상태를 원본과 같은 트랜잭션에서 확정한다. 시간 경과 조건은 소비자가 판정한다.
BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE TABLE IF NOT EXISTS public.youtube_live_runtime_state_guard (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    writer xid8 NOT NULL
);
COMMENT ON TABLE public.youtube_live_runtime_state_guard IS
    '전체 수명 쓰기의 MVCC 직렬화 행. 오래된 RR/SERIALIZABLE snapshot은 40001로 실패하며 재시도하지 않는다.';
REVOKE ALL ON TABLE public.youtube_live_runtime_state_guard FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.lock_youtube_live_runtime_state()
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $lock$
BEGIN
    -- 같은 트랜잭션은 잠금을 유지하되 행 버전을 다시 쓰지 않는다. TRUNCATE 뒤 빈 표도 복구한다.
    INSERT INTO public.youtube_live_runtime_state_guard AS guard (singleton,writer)
    VALUES (true,pg_catalog.pg_current_xact_id())
    ON CONFLICT (singleton) DO UPDATE SET writer=EXCLUDED.writer
    WHERE guard.writer IS DISTINCT FROM EXCLUDED.writer;
END
$lock$;
REVOKE ALL ON FUNCTION public.lock_youtube_live_runtime_state() FROM PUBLIC;

SELECT public.lock_youtube_live_runtime_state();
-- backfill과 trigger 설치 사이의 쓰기 누락을 막는다. FK SET NULL의 부모도 같은 배제 구간에 둔다.
LOCK TABLE public.source_observations, public.youtube_live_sessions,
    public.youtube_live_reconciliation_heads, public.youtube_live_pending_ends,
    public.youtube_video_availability, public.youtube_live_review_receipts,
    public.youtube_live_review_receipt_facts IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE IF NOT EXISTS public.youtube_live_runtime_state (
    video_id text PRIMARY KEY,
    channel_id text,
    session_status text CHECK (session_status IN ('UPCOMING','LIVE','ENDED')),
    head_status text CHECK (head_status IN ('UPCOMING','LIVE','ENDED')),
    scheduled_start_time timestamptz,
    lifecycle_origin text CHECK (lifecycle_origin IN ('metadata_only','observed','legacy_unknown')),
    last_live_positive_at timestamptz,
    last_live_positive_seen_at timestamptz,
    last_upcoming_positive_at timestamptz,
    last_upcoming_positive_seen_at timestamptz,
    availability_at timestamptz,
    availability_seen_at timestamptz,
    unresolvable_since timestamptz,
    reviewed_at timestamptz,
    CHECK ((channel_id IS NULL) = (session_status IS NULL)),
    CHECK (COALESCE(session_status IN ('LIVE','UPCOMING'),false)
        OR COALESCE(head_status IN ('LIVE','UPCOMING'),false))
);
COMMENT ON TABLE public.youtube_live_runtime_state IS
    'session 또는 head가 LIVE/UPCOMING인 영상의 동기 읽기 모델. ENDED 상대 행과 session 없는 head도 보존한다. '
    'reviewed_at은 현재 검토 가능하고 정확한 310 의미 사실이 일치하는 영수증 중 최대 기록 시각이다. '
    '원본·claim 잠금/조회 전에 lock_youtube_live_runtime_state()를 호출한다. 직접 DML은 statement trigger가 보호한다.';
REVOKE ALL ON TABLE public.youtube_live_runtime_state FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.refresh_youtube_live_runtime_state(p_video_ids text[])
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $refresh$
BEGIN
    PERFORM public.lock_youtube_live_runtime_state();
    -- VOLATILE의 문장별 snapshot으로 guard 대기 전에 시작한 DML에서도 앞선 커밋을 읽는다.
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
REVOKE ALL ON FUNCTION public.refresh_youtube_live_runtime_state(text[]) FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.guard_youtube_live_runtime_state()
RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $guard$
BEGIN
    PERFORM public.lock_youtube_live_runtime_state();
    RETURN NULL;
END
$guard$;
REVOKE ALL ON FUNCTION public.guard_youtube_live_runtime_state() FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.refresh_youtube_live_runtime_state_statement()
RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $statement$
DECLARE
    video_ids text[];
BEGIN
    -- transition 행 전체를 비교/JSON 변환하지 않는다. head의 큰 무시 배열은 펼치지 않고 key만 읽는다.
    IF TG_OP='INSERT' THEN
        SELECT array_agg(video_id ORDER BY video_id COLLATE "C") INTO video_ids
        FROM (SELECT DISTINCT video_id::text FROM runtime_new) changed;
    ELSIF TG_OP='DELETE' THEN
        SELECT array_agg(video_id ORDER BY video_id COLLATE "C") INTO video_ids
        FROM (SELECT DISTINCT video_id::text FROM runtime_old) changed;
    ELSIF TG_OP='UPDATE' THEN
        SELECT array_agg(video_id ORDER BY video_id COLLATE "C") INTO video_ids
        FROM (SELECT video_id::text FROM runtime_old UNION SELECT video_id::text FROM runtime_new) changed;
    ELSE
        -- TRUNCATE는 transition table이 없다. 남은 원본과 기존 모델의 합집합으로 삭제/고아 head도 반영한다.
        SELECT array_agg(video_id ORDER BY video_id COLLATE "C") INTO video_ids
        FROM (SELECT video_id FROM public.youtube_live_runtime_state
              UNION SELECT video_id FROM public.youtube_live_sessions
              UNION SELECT video_id FROM public.youtube_live_reconciliation_heads) changed;
    END IF;
    IF video_ids IS NOT NULL THEN
        PERFORM public.refresh_youtube_live_runtime_state(video_ids);
    END IF;
    RETURN NULL;
END
$statement$;
REVOKE ALL ON FUNCTION public.refresh_youtube_live_runtime_state_statement() FROM PUBLIC;

DO $triggers$
DECLARE
    source_table text;
BEGIN
    FOREACH source_table IN ARRAY ARRAY['youtube_live_sessions','youtube_live_reconciliation_heads',
        'youtube_live_pending_ends','youtube_video_availability','youtube_live_review_receipts','youtube_live_review_receipt_facts']
    LOOP
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_live_runtime_guard BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON public.%I FOR EACH STATEMENT EXECUTE FUNCTION public.guard_youtube_live_runtime_state()',source_table);
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_live_runtime_insert AFTER INSERT ON public.%I REFERENCING NEW TABLE AS runtime_new FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_live_runtime_state_statement()',source_table);
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_live_runtime_update AFTER UPDATE ON public.%I REFERENCING OLD TABLE AS runtime_old NEW TABLE AS runtime_new FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_live_runtime_state_statement()',source_table);
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_live_runtime_delete AFTER DELETE ON public.%I REFERENCING OLD TABLE AS runtime_old FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_live_runtime_state_statement()',source_table);
        EXECUTE format('CREATE OR REPLACE TRIGGER youtube_live_runtime_truncate AFTER TRUNCATE ON public.%I FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_live_runtime_state_statement()',source_table);
    END LOOP;
END
$triggers$;
CREATE OR REPLACE TRIGGER youtube_live_runtime_guard
    BEFORE DELETE OR TRUNCATE ON public.source_observations
    FOR EACH STATEMENT EXECUTE FUNCTION public.guard_youtube_live_runtime_state();

-- invoker 권한·입력 검증·SERIALIZABLE·snapshot CAS와 기존 ACL은 그대로 두고 잠금 순서만 맞춘다.
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
END
$record$;

DO $roles$
DECLARE
    recording_role text;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='hololive_runtime') THEN
        REVOKE ALL ON TABLE public.youtube_live_runtime_state,public.youtube_live_runtime_state_guard FROM hololive_runtime;
        GRANT SELECT ON TABLE public.youtube_live_runtime_state TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.lock_youtube_live_runtime_state() TO hololive_runtime;
        REVOKE ALL ON FUNCTION public.refresh_youtube_live_runtime_state(text[]),public.guard_youtube_live_runtime_state(),public.refresh_youtube_live_runtime_state_statement() FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='hololive_scraper') THEN
        REVOKE ALL ON TABLE public.youtube_live_runtime_state,public.youtube_live_runtime_state_guard FROM hololive_scraper;
        REVOKE ALL ON FUNCTION public.lock_youtube_live_runtime_state(),public.refresh_youtube_live_runtime_state(text[]),public.guard_youtube_live_runtime_state(),public.refresh_youtube_live_runtime_state_statement() FROM hololive_scraper;
    END IF;
    -- 이미 기록을 허가받은 역할에만 새 선행 잠금을 허용한다. 기록 함수 자체의 ACL은 확대하지 않는다.
    FOR recording_role IN
        SELECT DISTINCT role.rolname
        FROM pg_catalog.pg_proc procedure
        CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(procedure.proacl,pg_catalog.acldefault('f',procedure.proowner))) privilege
        JOIN pg_catalog.pg_roles role ON role.oid=privilege.grantee
        WHERE procedure.oid='public.record_youtube_live_review(uuid,text,text,text,text)'::regprocedure
          AND privilege.privilege_type='EXECUTE'
    LOOP
        EXECUTE format('GRANT EXECUTE ON FUNCTION public.lock_youtube_live_runtime_state() TO %I',recording_role);
    END LOOP;
END
$roles$;

SELECT public.refresh_youtube_live_runtime_state(ARRAY(
    SELECT video_id FROM public.youtube_live_runtime_state
    UNION SELECT video_id FROM public.youtube_live_sessions
    UNION SELECT video_id FROM public.youtube_live_reconciliation_heads));
COMMIT;
