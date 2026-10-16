-- publisher/consumer/retention을 중지한 점검 창 전용 cutover다. 원본 표 전체를 재작성한다.
-- 수집 출처 3열(job_key·collector_instance·collection_job_kind)은 행당 약 140B의 저카디널리티 문자열이라
-- 사전 표의 정수 FK로 바꾸고, shared 계약이 항상 sha256 hex로 강제하는 observation_key는 32바이트 bytea로 저장한다.
-- 공개 SQL/Go 표현은 그대로 hex text·출처 문자열이며 lock/claim 경로가 사전 표를 조인해 복원한다.
-- TYPE 변경은 heap/index를 재작성하며 ACCESS EXCLUSIVE를 COMMIT까지 보유한다. lock_timeout은 획득 대기만 제한한다.
CREATE TABLE IF NOT EXISTS public.source_observation_provenances (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_key text NOT NULL,
    collector_instance text NOT NULL,
    collection_job_kind text NOT NULL,
    CONSTRAINT chk_source_observation_provenance_bounds CHECK (
        length(job_key) BETWEEN 1 AND 512 AND length(collector_instance) BETWEEN 1 AND 128
        AND length(collection_job_kind) BETWEEN 1 AND 128
    ),
    CONSTRAINT uq_source_observation_provenance UNIQUE (job_key, collector_instance, collection_job_kind)
);
REVOKE ALL ON TABLE public.source_observation_provenances FROM PUBLIC;

-- 사전 행은 SECURITY DEFINER 함수만 만든다. 동시 추가의 패자는 새 statement snapshot에서 다시 읽고,
-- REPEATABLE READ 이상에서 끝내 보이지 않으면 40001로 호출자의 기존 재시도에 맡긴다.
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
          AND provenance.collection_job_kind = requested_collection_job_kind;
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
REVOKE ALL ON FUNCTION public.source_observation_provenance_id(text, text, text) FROM PUBLIC;
DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.source_observation_provenances TO hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT ON TABLE public.source_observation_provenances TO hololive_scraper;
        GRANT EXECUTE ON FUNCTION public.source_observation_provenance_id(text, text, text) TO hololive_scraper;
    END IF;
END
$roles$;

-- 준비 단계는 출처 사전과 nullable FK 열만 만든다. 중단 뒤에도 기존 text 열은 남는다.
DO $prepare$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.source_observations'::regclass
                   AND attname = 'provenance_id' AND NOT attisdropped) THEN
        INSERT INTO public.source_observation_provenances (job_key, collector_instance, collection_job_kind)
        SELECT DISTINCT observation.job_key, observation.collector_instance, observation.collection_job_kind
        FROM public.source_observations observation
        ON CONFLICT (job_key, collector_instance, collection_job_kind) DO NOTHING;
        ALTER TABLE public.source_observations ADD COLUMN provenance_id integer;
    END IF;
END
$prepare$;

-- 각 5,000행 batch를 확정한다. 재실행은 같은 keyset을 읽되 이미 맞는 FK를 다시 쓰지 않는다.
DO $backfill$
DECLARE
    previous_id bigint;
    upper_id bigint;
    picked_ids bigint[];
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.source_observations'::regclass
                   AND attname = 'collector_instance' AND NOT attisdropped) THEN
        RETURN;
    END IF;
    SELECT max(id) INTO upper_id FROM public.source_observations;
    LOOP
        PERFORM set_config('lock_timeout', '3s', true);
        IF previous_id IS NULL THEN
            SELECT array_agg(id ORDER BY id) INTO picked_ids
            FROM (SELECT id FROM public.source_observations
                  WHERE id <= upper_id ORDER BY id LIMIT 5000) picked;
        ELSE
            SELECT array_agg(id ORDER BY id) INTO picked_ids
            FROM (SELECT id FROM public.source_observations
                  WHERE id > previous_id AND id <= upper_id ORDER BY id LIMIT 5000) picked;
        END IF;
        EXIT WHEN picked_ids IS NULL;
        UPDATE public.source_observations observation
        SET provenance_id = provenance.id
        FROM public.source_observation_provenances provenance
        WHERE observation.id = ANY(picked_ids)
          AND provenance.job_key = observation.job_key
          AND provenance.collector_instance = observation.collector_instance
          AND provenance.collection_job_kind = observation.collection_job_kind
          AND observation.provenance_id IS DISTINCT FROM provenance.id;
        previous_id := picked_ids[cardinality(picked_ids)];
        COMMIT;
    END LOOP;
END
$backfill$;

DO $constraints$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.source_observations'::regclass
               AND attname = 'collector_instance' AND NOT attisdropped)
       AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'public.source_observations'::regclass
                       AND conname = 'source_observations_provenance_nn') THEN
        ALTER TABLE public.source_observations ADD CONSTRAINT source_observations_provenance_nn
            CHECK (provenance_id IS NOT NULL) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'public.source_observations'::regclass
                   AND conname = 'fk_source_observation_provenance') THEN
        ALTER TABLE public.source_observations ADD CONSTRAINT fk_source_observation_provenance
            FOREIGN KEY (provenance_id) REFERENCES public.source_observation_provenances(id)
            ON DELETE RESTRICT NOT VALID;
    END IF;
END
$constraints$;
DO $validate$
DECLARE
    pending text;
BEGIN
    FOR pending IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'public.source_observations'::regclass AND NOT convalidated
          AND conname IN ('chk_source_observation_text_bounds', 'source_observations_provenance_nn',
                          'fk_source_observation_provenance')
    LOOP
        EXECUTE format('ALTER TABLE public.source_observations VALIDATE CONSTRAINT %I', pending);
    END LOOP;
END
$validate$;

-- observation_key 재작성이 전체 heap/index를 다시 쓰므로 제거한 text 열의 공간도 같이 회수된다.
DO $migration$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.source_observations'::regclass
               AND attname = 'observation_key' AND atttypid = 'text'::regtype) THEN
        ALTER TABLE public.source_observations
            DROP CONSTRAINT chk_source_observation_text_bounds,
            ALTER COLUMN observation_key TYPE bytea USING decode(observation_key, 'hex'),
            ALTER COLUMN provenance_id SET NOT NULL,
            DROP COLUMN collector_instance,
            DROP COLUMN job_key,
            DROP COLUMN collection_job_kind,
            DROP CONSTRAINT IF EXISTS source_observations_provenance_nn,
            ADD CONSTRAINT chk_source_observation_text_bounds
                CHECK (length(subject_key) BETWEEN 1 AND 256) NOT VALID,
            ADD CONSTRAINT chk_source_observation_key CHECK (octet_length(observation_key) = 32) NOT VALID;
    END IF;
END
$migration$;
DO $validate$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'public.source_observations'::regclass
               AND conname = 'chk_source_observation_key' AND NOT convalidated) THEN
        ALTER TABLE public.source_observations VALIDATE CONSTRAINT chk_source_observation_key;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'public.source_observations'::regclass
               AND conname = 'chk_source_observation_text_bounds' AND NOT convalidated) THEN
        ALTER TABLE public.source_observations VALIDATE CONSTRAINT chk_source_observation_text_bounds;
    END IF;
END
$validate$;

-- 공개 식별 API는 계속 hex text를 받고 돌려준다. 저장 표현만 bytea다.
CREATE OR REPLACE FUNCTION public.lock_source_observation_identity(
    requested_provider text, requested_observation_kind text, requested_subject_key text,
    requested_observation_key text, requested_schema_version smallint, requested_contract_generation bigint
) RETURNS TABLE (id bigint, evidence_sha256 text)
LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog
AS $function$
    SELECT observation.id, encode(observation.evidence_sha256, 'hex')
    FROM public.source_observations observation
    WHERE observation.provider = requested_provider
      AND observation.observation_kind = requested_observation_kind
      AND observation.subject_key = requested_subject_key
      AND observation.observation_key = decode(requested_observation_key, 'hex')
      AND observation.schema_version = requested_schema_version
      AND observation.contract_generation = requested_contract_generation
    FOR SHARE OF observation
$function$;
CREATE OR REPLACE FUNCTION public.lock_source_observation(requested_observation_id bigint)
RETURNS TABLE (provider text, observation_kind text, subject_key text, observation_key text,
               schema_version smallint, contract_generation bigint, evidence_sha256 text)
LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog
AS $function$
    SELECT observation.provider, observation.observation_kind, observation.subject_key,
           encode(observation.observation_key, 'hex'), observation.schema_version, observation.contract_generation,
           encode(observation.evidence_sha256, 'hex')
    FROM public.source_observations observation
    WHERE observation.id = requested_observation_id
    FOR SHARE OF observation
$function$;

-- ACTIVE lease 보호는 사전 표의 job_key로 판정한다. 나머지 보호 사유는 307과 같다.
CREATE OR REPLACE FUNCTION public.source_observation_retention_protected(requested_id bigint)
RETURNS boolean LANGUAGE sql STABLE SET search_path = pg_catalog
AS $function$
    SELECT EXISTS (
        SELECT 1 FROM public.source_observations AS observation
        WHERE observation.id = requested_id AND (
            EXISTS (SELECT 1 FROM public.source_observation_queue q WHERE q.observation_id = observation.id)
            OR EXISTS (SELECT 1 FROM public.source_observation_replay_requests r
                       WHERE r.observation_id = observation.id AND r.status = 'PENDING')
            OR EXISTS (SELECT 1 FROM public.youtube_live_reconciliation_heads h
                       WHERE h.end_candidate_observation_id = observation.id)
            OR EXISTS (SELECT 1 FROM public.youtube_collection_projection_generations g
                       WHERE g.generation = observation.projection_generation AND g.status IN ('CURRENT', 'STAGING'))
            OR EXISTS (SELECT 1 FROM public.youtube_collection_job_leases l
                       JOIN public.source_observation_provenances p ON p.id = observation.provenance_id
                       WHERE l.job_key = p.job_key AND l.slot_state = 'ACTIVE')
            OR EXISTS (SELECT 1 FROM public.source_observation_applications a
                       JOIN public.youtube_live_sessions s ON s.video_id = a.entity_key
                       WHERE a.observation_id = observation.id AND a.entity_kind = 'youtube_live_session'
                         AND s.status IN ('LIVE', 'UPCOMING'))
            OR EXISTS (SELECT 1 FROM public.youtube_live_pending_ends p
                       JOIN public.youtube_live_sessions s ON s.video_id = p.video_id
                       WHERE p.observation_id = observation.id AND s.status IN ('LIVE', 'UPCOMING'))
        )
    )
$function$;
