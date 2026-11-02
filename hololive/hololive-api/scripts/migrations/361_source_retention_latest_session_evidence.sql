-- LIVE/UPCOMING 세션 보호를 세션별 최신 application 근거로 좁힌다. 351까지는 그 세션에 적용된 모든 원본을
-- 보호해, 몇 주씩 UPCOMING으로 남는 프리챗·미시작 예약이 스냅샷마다 받은 application 때문에 보관 기간이 지난
-- 원본이 세션 종료 전까지 하나도 지워지지 않았다(2026-11-02 운영: 세션 163개가 application 137만 행 보호).
-- 런타임 소비자는 큐·종료 후보·pending-end로만 원본을 읽고 이 사유들은 그대로 보호하므로, 활성 세션의 현재
-- 근거는 최신 application 하나로 충분하다. 나머지 보호 사유는 351과 같다.
-- 함수 이름·인자·반환형이 같아 351 이후 이미지에서도 그대로 동작한다.
DO $guard$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_catalog.pg_index
        WHERE indexrelid = to_regclass('public.idx_source_application_entity_latest')
          AND indisvalid AND indisready
    ) THEN
        RAISE EXCEPTION 'idx_source_application_entity_latest is missing or invalid; rebuild the concurrent index (360) before resuming';
    END IF;
END
$guard$;

CREATE OR REPLACE FUNCTION public.source_observation_retention_protected(requested_id bigint)
RETURNS boolean LANGUAGE sql STABLE SET search_path = pg_catalog
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM public.source_observations AS observation
        WHERE observation.id = requested_id AND (
            EXISTS (SELECT 1 FROM public.source_observation_queue q WHERE q.observation_id = observation.id)
            OR EXISTS (SELECT 1 FROM public.youtube_live_reconciliation_heads h
                       WHERE h.end_candidate_observation_id = observation.id)
            OR EXISTS (SELECT 1 FROM public.youtube_collection_projection_generations g
                       WHERE g.generation = observation.projection_generation AND g.status IN ('CURRENT', 'STAGING'))
            OR EXISTS (SELECT 1 FROM public.youtube_collection_job_leases l
                       JOIN public.source_observation_provenances p ON p.id = observation.provenance_id
                       WHERE l.job_key = p.job_key AND l.slot_state = 'ACTIVE')
            OR EXISTS (SELECT 1 FROM public.source_observation_applications a
                       JOIN public.source_observation_entity_kinds k ON k.id = a.entity_kind_id
                       JOIN public.youtube_live_sessions s ON s.video_id = a.entity_key
                       WHERE a.observation_id = observation.id AND k.name = 'youtube_live_session'
                         AND s.status IN ('LIVE', 'UPCOMING')
                         -- 같은 세션의 더 새 원본이 있으면 이 원본은 현재 근거가 아니다.
                         AND NOT EXISTS (SELECT 1 FROM public.source_observation_applications newer
                                         WHERE newer.entity_kind_id = a.entity_kind_id
                                           AND newer.entity_key = a.entity_key
                                           AND newer.observation_id > a.observation_id))
            OR EXISTS (SELECT 1 FROM public.youtube_live_pending_ends p
                       JOIN public.youtube_live_sessions s ON s.video_id = p.video_id
                       WHERE p.observation_id = observation.id AND s.status IN ('LIVE', 'UPCOMING'))
        )
    )
$function$;
