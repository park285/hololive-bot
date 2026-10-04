-- 검토 영수증의 유효성을 기술적 갱신 시각이 아니라 수명 판단에 쓰는 사실로 판정한다.
-- 기존 영수증·canonical·head·가용성 행과 보존 정책은 바꾸지 않는다. 함수 정의만 추가·교체한다.

-- 전체 snapshot(246 형식)과 축약 snapshot(262 형식)에서 같은 의미 사실을 뽑는다.
-- 시각은 기록 세션의 시간대 표기와 무관하게 epoch로 맞춘다. 제외: last_seen_at·*_observed_at·
-- thumbnail_url, head updated_at·next_end_check_at·ignored_absence_scheduled_for(요약 포함),
-- pending received_at, 가용성 확인 시각·관측 ID·evidence hash와 같은 판정의 진단 세부사항.
-- 상태·일정·출처·positive·종료/부재 근거·가용성 판정이 바뀌면 결과가 달라진다.
CREATE OR REPLACE FUNCTION public.youtube_live_review_semantic_facts(p_snapshot jsonb)
RETURNS jsonb
LANGUAGE sql STABLE
AS $semantic$
    SELECT jsonb_build_object(
        'session', CASE WHEN jsonb_typeof(p_snapshot->'session') = 'object' THEN jsonb_build_object(
            'video_id', p_snapshot#>'{session,video_id}',
            'channel_id', p_snapshot#>'{session,channel_id}',
            'status', p_snapshot#>'{session,status}',
            'lifecycle_origin', p_snapshot#>'{session,lifecycle_origin}',
            'title', p_snapshot#>'{session,title}',
            'topic_id', p_snapshot#>'{session,topic_id}',
            'is_premiere', p_snapshot#>'{session,is_premiere}',
            'scheduled_start_time', extract(epoch FROM (p_snapshot#>>'{session,scheduled_start_time}')::timestamptz),
            'started_at', extract(epoch FROM (p_snapshot#>>'{session,started_at}')::timestamptz),
            'ended_at', extract(epoch FROM (p_snapshot#>>'{session,ended_at}')::timestamptz),
            'live_first_seen_at', extract(epoch FROM (p_snapshot#>>'{session,live_first_seen_at}')::timestamptz)) END,
        'head', CASE WHEN jsonb_typeof(p_snapshot->'head') = 'object' THEN jsonb_build_object(
            'status', p_snapshot#>'{head,status}',
            'last_upcoming_positive_at', extract(epoch FROM (p_snapshot#>>'{head,last_upcoming_positive_at}')::timestamptz),
            'last_upcoming_positive_seen_at', extract(epoch FROM (p_snapshot#>>'{head,last_upcoming_positive_seen_at}')::timestamptz),
            'last_live_positive_at', extract(epoch FROM (p_snapshot#>>'{head,last_live_positive_at}')::timestamptz),
            'last_live_positive_seen_at', extract(epoch FROM (p_snapshot#>>'{head,last_live_positive_seen_at}')::timestamptz),
            'last_end_evidence_at', extract(epoch FROM (p_snapshot#>>'{head,last_end_evidence_at}')::timestamptz),
            'last_complete_absence_at', extract(epoch FROM (p_snapshot#>>'{head,last_complete_absence_at}')::timestamptz),
            'last_absence_scheduled_for', extract(epoch FROM (p_snapshot#>>'{head,last_absence_scheduled_for}')::timestamptz),
            'first_absence_scheduled_for', extract(epoch FROM (p_snapshot#>>'{head,first_absence_scheduled_for}')::timestamptz),
            'second_absence_scheduled_for', extract(epoch FROM (p_snapshot#>>'{head,second_absence_scheduled_for}')::timestamptz),
            'consecutive_absence_slots', p_snapshot#>'{head,consecutive_absence_slots}',
            'last_absence_observation_id', p_snapshot#>'{head,last_absence_observation_id}',
            'end_candidate_kind', p_snapshot#>'{head,end_candidate_kind}',
            'end_candidate_observation_id', p_snapshot#>'{head,end_candidate_observation_id}',
            'ended_at', extract(epoch FROM (p_snapshot#>>'{head,ended_at}')::timestamptz),
            'end_reason', p_snapshot#>'{head,end_reason}') END,
        'pending', CASE WHEN jsonb_typeof(p_snapshot->'pending') = 'object' THEN jsonb_build_object(
            'channel_id', p_snapshot#>'{pending,channel_id}',
            'kind', p_snapshot#>'{pending,kind}',
            'observation_id', p_snapshot#>'{pending,observation_id}',
            'effective_at', extract(epoch FROM (p_snapshot#>>'{pending,effective_at}')::timestamptz),
            'scheduled_for', extract(epoch FROM (p_snapshot#>>'{pending,scheduled_for}')::timestamptz),
            'ended_at', extract(epoch FROM (p_snapshot#>>'{pending,ended_at}')::timestamptz),
            'negative_eligible', p_snapshot#>'{pending,negative_eligible}',
            'scope_covers', p_snapshot#>'{pending,scope_covers}') END,
        -- 246의 동명 column/alias 때문에 기존 영수증에는 가용성 판정 문자열만 저장됐다.
        -- 신규 전체 row에서도 같은 수명 사실인 판정을 비교하며 진단 메타데이터로 과거 결정을 추정하지 않는다.
        'availability', CASE jsonb_typeof(p_snapshot->'availability')
            WHEN 'object' THEN p_snapshot#>'{availability,availability}'
            WHEN 'string' THEN p_snapshot->'availability'
        END)
$semantic$;
REVOKE ALL ON FUNCTION public.youtube_live_review_semantic_facts(jsonb) FROM PUBLIC;

-- snapshot_sha256은 현재 전체 원본의 digest다. 가용성은 동명 column이 아닌 전체 row를 명시해 모든 변경을 CAS로 보호한다.
-- 저장용 original_snapshot은 head의 무시한 부재 slot 배열만 개수·digest 요약으로 바꿔 상한 안에 둔다.
CREATE OR REPLACE FUNCTION public.youtube_live_review_snapshot(p_video_id TEXT)
RETURNS TABLE (original_snapshot jsonb, snapshot_sha256 TEXT, evidence_refs jsonb, reviewable boolean)
LANGUAGE sql STABLE
AS $snapshot$
    WITH facts AS (
        SELECT jsonb_build_object('session',to_jsonb(session),'head',to_jsonb(head),
                   'pending',to_jsonb(pending),'availability',to_jsonb(availability.*)) AS snapshot,
               jsonb_build_object('availability_observation_id',availability.observation_id,
                   'availability_evidence_sha256',availability.evidence_sha256,
                   'pending_observation_id',pending.observation_id) AS refs,
               CASE WHEN head.video_id IS NOT NULL THEN jsonb_build_object(
                   'count',cardinality(head.ignored_absence_scheduled_for),
                   'sha256',encode(sha256(convert_to(to_jsonb(head.ignored_absence_scheduled_for)::TEXT,'UTF8')),'hex'))
               END AS ignored_absence_summary,
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
    SELECT CASE WHEN ignored_absence_summary IS NULL THEN snapshot
                ELSE jsonb_set(snapshot,'{head}',
                    ((snapshot->'head') - 'ignored_absence_scheduled_for')
                    || jsonb_build_object('ignored_absence_summary',ignored_absence_summary))
           END,
           encode(sha256(convert_to(snapshot::TEXT,'UTF8')),'hex'), refs,
           COALESCE(reviewable,false) FROM facts
$snapshot$;
REVOKE ALL ON FUNCTION public.youtube_live_review_snapshot(TEXT) FROM PUBLIC;

-- 현재 원본에 적용되는 closed_unresolved 영수증의 최신 기록 시각. 현재도 검토 가능하고,
-- 정확한 digest가 같거나 기존·신규 형식 모두에서 같은 의미 사실을 가진 영수증만 인정한다.
-- 영수증이 없거나 사실이 바뀌면 NULL이다. 집계와 확인 대상 선택이 같은 판정을 쓴다.
CREATE OR REPLACE FUNCTION public.youtube_live_review_current_receipt(p_video_id TEXT)
RETURNS TABLE (reviewed_at timestamptz)
LANGUAGE sql STABLE
AS $current$
    SELECT max(receipt.recorded_at)
    FROM public.youtube_live_review_snapshot(p_video_id) current_snapshot
    CROSS JOIN LATERAL (
        SELECT public.youtube_live_review_semantic_facts(current_snapshot.original_snapshot) AS facts
    ) current_facts
    JOIN public.youtube_live_review_receipts receipt ON receipt.video_id = p_video_id
    WHERE current_snapshot.reviewable
      AND (receipt.snapshot_sha256 = current_snapshot.snapshot_sha256
           OR public.youtube_live_review_semantic_facts(receipt.original_snapshot) = current_facts.facts)
$current$;
REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipt(TEXT) FROM PUBLIC;

DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.youtube_live_review_semantic_facts(jsonb) TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.youtube_live_review_snapshot(TEXT) TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.youtube_live_review_current_receipt(TEXT) TO hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON FUNCTION public.youtube_live_review_semantic_facts(jsonb) FROM hololive_scraper;
        REVOKE ALL ON FUNCTION public.youtube_live_review_snapshot(TEXT) FROM hololive_scraper;
        REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipt(TEXT) FROM hololive_scraper;
    END IF;
END
$roles$;
