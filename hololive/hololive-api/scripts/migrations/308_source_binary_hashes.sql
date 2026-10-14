-- publisher/consumer를 중지한 점검 창 전용 cutover다. 각 표의 두 열을 한 번에 변환한다.
-- TYPE 변경은 표 전체 heap과 index를 재작성하며 ACCESS EXCLUSIVE를 COMMIT까지 보유한다.
-- lock_timeout은 획득 대기만 제한한다. 새 heap/index와 WAL/복제 지연을 감당할 공간이 필요하다.
-- 각 DO는 독립 autocommit이며 완료된 표는 타입 가드로 건너뛴다. 배치 UPDATE로 위장하지 않는다.
DO $migration$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.source_observations'::regclass
               AND attname = 'scope_sha256' AND atttypid = 'text'::regtype) THEN
        ALTER TABLE public.source_observations DROP CONSTRAINT chk_source_observation_hashes;
        ALTER TABLE public.source_observations
            ALTER COLUMN scope_sha256 TYPE bytea USING decode(scope_sha256, 'hex'),
            ALTER COLUMN evidence_sha256 TYPE bytea USING decode(evidence_sha256, 'hex');
        ALTER TABLE public.source_observations ADD CONSTRAINT chk_source_observation_hashes
            CHECK (octet_length(scope_sha256) = 32 AND octet_length(evidence_sha256) = 32) NOT VALID;
    END IF;
END
$migration$;
ALTER TABLE public.source_observations VALIDATE CONSTRAINT chk_source_observation_hashes;

DO $migration$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.youtube_content_absence_slots'::regclass
               AND attname = 'scope_sha256' AND atttypid = 'text'::regtype) THEN
        ALTER TABLE public.youtube_content_absence_slots DROP CONSTRAINT chk_youtube_content_absence_hashes;
        ALTER TABLE public.youtube_content_absence_slots
            ALTER COLUMN scope_sha256 TYPE bytea USING decode(scope_sha256, 'hex'),
            ALTER COLUMN evidence_sha256 TYPE bytea USING decode(evidence_sha256, 'hex');
        ALTER TABLE public.youtube_content_absence_slots ADD CONSTRAINT chk_youtube_content_absence_hashes
            CHECK (octet_length(scope_sha256) = 32 AND octet_length(evidence_sha256) = 32) NOT VALID;
    END IF;
END
$migration$;
ALTER TABLE public.youtube_content_absence_slots VALIDATE CONSTRAINT chk_youtube_content_absence_hashes;

DO $migration$
BEGIN
    PERFORM set_config('lock_timeout', '3s', true);
    IF EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.youtube_live_absence_slots'::regclass
               AND attname = 'scope_sha256' AND atttypid = 'text'::regtype) THEN
        ALTER TABLE public.youtube_live_absence_slots
            ALTER COLUMN scope_sha256 TYPE bytea USING decode(scope_sha256, 'hex'),
            ALTER COLUMN evidence_sha256 TYPE bytea USING decode(evidence_sha256, 'hex');
        ALTER TABLE public.youtube_live_absence_slots ADD CONSTRAINT chk_youtube_live_absence_hashes
            CHECK (octet_length(scope_sha256) = 32 AND octet_length(evidence_sha256) = 32) NOT VALID;
    END IF;
END
$migration$;
ALTER TABLE public.youtube_live_absence_slots VALIDATE CONSTRAINT chk_youtube_live_absence_hashes;

-- 공개/Go 해시 표현은 소문자 hex다. identity·replay 잠금 API도 같은 표현을 돌려준다.
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
      AND observation.observation_key = requested_observation_key
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
           observation.observation_key, observation.schema_version, observation.contract_generation,
           encode(observation.evidence_sha256, 'hex')
    FROM public.source_observations observation
    WHERE observation.id = requested_observation_id
    FOR SHARE OF observation
$function$;
