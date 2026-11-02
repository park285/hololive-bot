-- 삭제 경로가 없던 네 표에 보존 정리 함수를 둔다. 2026-11-02 운영에서 네 표는 하루 약 6,300행씩 늘기만 했다.
-- 런타임은 retention tick(api)에서 SECURITY DEFINER 함수로만 지우며, 표 자체의 DELETE 권한은 주지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';

DO $guard$
DECLARE
    required text;
BEGIN
    FOREACH required IN ARRAY ARRAY[
        'public.idx_source_observations_provenance_id',
        'public.idx_youtube_content_absence_slots_scheduled_for',
        'public.idx_youtube_channel_profile_evidence_received_at',
        'public.idx_youtube_channel_photo_variants_received_at'
    ] LOOP
        IF NOT EXISTS (
            SELECT 1 FROM pg_catalog.pg_index
            WHERE indexrelid = to_regclass(required) AND indisvalid AND indisready
        ) THEN
            RAISE EXCEPTION '% is missing or invalid; rebuild the concurrent index (362-365) before resuming', required;
        END IF;
    END LOOP;
END
$guard$;

-- content absence slot: reducer는 현재 slot(재처리 판정)과 이번 관측보다 늦은 slot만 읽고, 이미 반영된 판정은
-- youtube_content_evidence_clocks에 남는다. live slot(221·351)처럼 scheduled_for가 보존 기간을 넘은 slot을 지우되,
-- 그보다 오래된 video_list·shorts_list 관측이 큐에서 대기·처리 중이면 그 재처리 판정에 쓰이므로 보류한다.
CREATE OR REPLACE FUNCTION public.delete_youtube_content_absence_slot_retention_batch(
    requested_cutoff timestamptz,
    requested_limit integer
)
RETURNS TABLE (deleted_scheduled_for timestamptz)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    WITH active_old_content AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PENDING'
          AND observation.observation_kind IN ('video_list', 'shorts_list')
          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff
        LIMIT 1
    ),
    processing_old_content AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PROCESSING'
          AND observation.observation_kind IN ('video_list', 'shorts_list')
          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff
        LIMIT 1
    ),
    candidates AS (
        SELECT slot.channel_id, slot.observation_kind, slot.scheduled_for
        FROM public.youtube_content_absence_slots AS slot
        WHERE slot.scheduled_for < requested_cutoff
          AND NOT EXISTS (SELECT 1 FROM active_old_content)
          AND NOT EXISTS (SELECT 1 FROM processing_old_content)
        ORDER BY slot.scheduled_for
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF slot SKIP LOCKED
    )
    DELETE FROM public.youtube_content_absence_slots AS slot
    USING candidates AS candidate
    WHERE slot.channel_id = candidate.channel_id
      AND slot.observation_kind = candidate.observation_kind
      AND slot.scheduled_for = candidate.scheduled_for
    RETURNING slot.scheduled_for
$$;

-- profile evidence·photo variant: 런타임은 head 표만 읽고 이 두 표는 slot별 감사 이력이다. 원본 kind
-- (channel_profile·channel_photo)와 같은 보존 기간이 지나면 지운다. 원본이 먼저 지워지면 observation_id만 NULL로 남는다.
CREATE OR REPLACE FUNCTION public.delete_youtube_channel_profile_evidence_retention_batch(
    requested_cutoff timestamptz,
    requested_limit integer
)
RETURNS TABLE (deleted_received_at timestamptz)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    WITH candidates AS (
        SELECT evidence.channel_id, evidence.scheduled_for, evidence.provider
        FROM public.youtube_channel_profile_evidence AS evidence
        WHERE evidence.received_at < requested_cutoff
        ORDER BY evidence.received_at
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF evidence SKIP LOCKED
    )
    DELETE FROM public.youtube_channel_profile_evidence AS evidence
    USING candidates AS candidate
    WHERE evidence.channel_id = candidate.channel_id
      AND evidence.scheduled_for = candidate.scheduled_for
      AND evidence.provider = candidate.provider
    RETURNING evidence.received_at
$$;

CREATE OR REPLACE FUNCTION public.delete_youtube_channel_photo_variant_retention_batch(
    requested_cutoff timestamptz,
    requested_limit integer
)
RETURNS TABLE (deleted_received_at timestamptz)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
    WITH candidates AS (
        SELECT variant.channel_id, variant.kind, variant.provider, variant.scheduled_for
        FROM public.youtube_channel_photo_variants AS variant
        WHERE variant.received_at < requested_cutoff
        ORDER BY variant.received_at
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF variant SKIP LOCKED
    )
    DELETE FROM public.youtube_channel_photo_variants AS variant
    USING candidates AS candidate
    WHERE variant.channel_id = candidate.channel_id
      AND variant.kind = candidate.kind
      AND variant.provider = candidate.provider
      AND variant.scheduled_for = candidate.scheduled_for
    RETURNING variant.received_at
$$;

-- provenance: collector_instance가 프로세스마다 달라 collector를 재시작·배포할 때마다 job_key 전체가 새 사전 행이
-- 된다(운영: 47,959행 중 원본이 참조하는 행 17,708). 어떤 원본도 참조하지 않는 사전 행만 지운다.
-- resolver는 재사용할 행을 FOR KEY SHARE로 잠그므로 정리의 FOR UPDATE SKIP LOCKED가 그 행을 건너뛴다. 정리가 먼저
-- 잠근 행은 resolver가 정리 커밋을 기다린 뒤 다시 읽어 새 행을 만들고, REPEATABLE READ 호출자는 40001로 기존 재시도를 탄다.
CREATE OR REPLACE FUNCTION public.source_observation_provenance_id(
    requested_job_key text, requested_collector_instance text, requested_collection_job_kind text
) RETURNS integer
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog
AS $function$
DECLARE
    resolved integer;
BEGIN
    FOR attempt IN 1..3 LOOP
        SELECT provenance.id INTO resolved
        FROM public.source_observation_provenances provenance
        WHERE provenance.job_key = requested_job_key
          AND provenance.collector_instance = requested_collector_instance
          AND provenance.collection_job_kind = requested_collection_job_kind
        FOR KEY SHARE OF provenance;
        IF FOUND THEN
            RETURN resolved;
        END IF;
        INSERT INTO public.source_observation_provenances (job_key, collector_instance, collection_job_kind)
        VALUES (requested_job_key, requested_collector_instance, requested_collection_job_kind)
        ON CONFLICT (job_key, collector_instance, collection_job_kind) DO NOTHING
        RETURNING id INTO resolved;
        IF resolved IS NOT NULL THEN
            RETURN resolved;
        END IF;
    END LOOP;
    RAISE EXCEPTION 'source observation provenance could not be resolved' USING ERRCODE = '40001';
END
$function$;

CREATE TABLE IF NOT EXISTS public.source_observation_provenance_gc_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    cursor_id integer NOT NULL DEFAULT 0 CHECK (cursor_id >= 0)
);
INSERT INTO public.source_observation_provenance_gc_state (singleton) VALUES (true)
ON CONFLICT (singleton) DO NOTHING;
REVOKE ALL ON TABLE public.source_observation_provenance_gc_state FROM PUBLIC;

-- id 순 창을 잠그지 않고 자른 뒤, 참조 없는 후보에만 행 잠금을 건다. 참조 중인 사전 행에 매 tick 행 잠금을 쓰지 않기
-- 위해서다. 잠금을 얻은 뒤 다음 문장의 새 snapshot에서 참조를 다시 확인하므로, 잠금 직전에 커밋된 원본이 가리키는
-- 행은 지우지 않는다. 창 끝까지 확인하면 다음 창으로 가고, 남은 행이 없으면 처음부터 다시 돈다.
CREATE OR REPLACE FUNCTION public.delete_source_observation_provenance_batch(requested_limit integer)
RETURNS bigint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = pg_catalog
AS $function$
DECLARE
    previous_id integer;
    window_end integer;
    locked_ids integer[];
    deleted bigint;
BEGIN
    IF requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000 THEN
        RAISE EXCEPTION 'invalid provenance retention request' USING ERRCODE = '22023';
    END IF;
    SELECT cursor_id INTO STRICT previous_id FROM public.source_observation_provenance_gc_state
    WHERE singleton FOR UPDATE;
    SELECT max(scan.id) INTO window_end
    FROM (
        SELECT provenance.id FROM public.source_observation_provenances AS provenance
        WHERE provenance.id > previous_id
        ORDER BY provenance.id LIMIT requested_limit
    ) AS scan;
    SELECT array_agg(candidate.id) INTO locked_ids
    FROM (
        SELECT provenance.id FROM public.source_observation_provenances AS provenance
        WHERE provenance.id > previous_id AND provenance.id <= window_end
          AND NOT EXISTS (
              SELECT 1 FROM public.source_observations AS observation
              WHERE observation.provenance_id = provenance.id
          )
        FOR UPDATE OF provenance SKIP LOCKED
    ) AS candidate;
    DELETE FROM public.source_observation_provenances AS provenance
    WHERE provenance.id = ANY(locked_ids)
      AND NOT EXISTS (
          SELECT 1 FROM public.source_observations AS observation
          WHERE observation.provenance_id = provenance.id
      );
    GET DIAGNOSTICS deleted = ROW_COUNT;
    UPDATE public.source_observation_provenance_gc_state
    SET cursor_id = coalesce(window_end, 0)
    WHERE singleton;
    RETURN deleted;
END
$function$;

REVOKE ALL ON FUNCTION public.delete_youtube_content_absence_slot_retention_batch(timestamptz, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.delete_youtube_channel_profile_evidence_retention_batch(timestamptz, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.delete_youtube_channel_photo_variant_retention_batch(timestamptz, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.delete_source_observation_provenance_batch(integer) FROM PUBLIC;

DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT EXECUTE ON FUNCTION public.delete_youtube_content_absence_slot_retention_batch(timestamptz, integer)
            TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.delete_youtube_channel_profile_evidence_retention_batch(timestamptz, integer)
            TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.delete_youtube_channel_photo_variant_retention_batch(timestamptz, integer)
            TO hololive_runtime;
        GRANT EXECUTE ON FUNCTION public.delete_source_observation_provenance_batch(integer) TO hololive_runtime;
    END IF;
END
$roles$;

COMMIT;
