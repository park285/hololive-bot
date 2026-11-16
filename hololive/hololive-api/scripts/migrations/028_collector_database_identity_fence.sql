-- 수집기 a~d가 각자 로그인(hololive_collector_<slot>)으로 접속한 세션은 자기 instance의 lease와 관측만 쓸 수 있다.
-- 신원은 session_user로 정하므로 SECURITY DEFINER 함수 안에서도 바뀌지 않는다. 수집기가 아닌 세션(runtime, owner, superuser)은
-- 검사하지 않는다. hololive_scraper 그룹을 상속하지만 slot에 묶이지 않은 로그인은 모두 거부한다.
-- 만료 판정은 앱 문장보다 늦지 않은 시각으로만 한다: 남의 lease가 아직 살아 있는지는 clock_timestamp(),
-- 자기 lease가 살아 있는지는 transaction_timestamp()로 본다. 앱이 허용한 정상 경로를 이 판정이 거부하지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE OR REPLACE FUNCTION public.youtube_collector_session_instance()
RETURNS text
LANGUAGE plpgsql
STABLE
SET search_path TO 'pg_catalog'
AS $function$
BEGIN
    CASE session_user
        WHEN 'hololive_collector_a' THEN RETURN 'youtube-collector-a';
        WHEN 'hololive_collector_b' THEN RETURN 'youtube-collector-b';
        WHEN 'hololive_collector_c' THEN RETURN 'youtube-collector-c';
        WHEN 'hololive_collector_d' THEN RETURN 'youtube-collector-d';
        ELSE NULL;
    END CASE;

    IF session_user = 'hololive_scraper' THEN
        RAISE EXCEPTION 'collector database identity is not bound to a collector instance'
            USING ERRCODE = '42501';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
        IF pg_catalog.pg_has_role(session_user, 'hololive_scraper', 'MEMBER')
           AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = session_user AND rolsuper) THEN
            RAISE EXCEPTION 'collector database identity is not bound to a collector instance'
                USING ERRCODE = '42501';
        END IF;
    END IF;

    RETURN NULL;
END
$function$;

CREATE OR REPLACE FUNCTION public.enforce_youtube_collection_job_lease_collector_identity()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    session_instance text := public.youtube_collector_session_instance();
BEGIN
    IF session_instance IS NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.owner_instance IS DISTINCT FROM session_instance AND (TG_OP = 'INSERT' OR NEW.owner_instance IS NOT NULL) THEN
        RAISE EXCEPTION 'collector lease owner does not match the database identity'
            USING ERRCODE = '42501';
    END IF;

    IF TG_OP = 'INSERT' THEN
        RETURN NEW;
    END IF;

    IF ROW(NEW.job_key, NEW.provider, NEW.job_class, NEW.collection_job_kind, NEW.subject_key)
       IS DISTINCT FROM ROW(OLD.job_key, OLD.provider, OLD.job_class, OLD.collection_job_kind, OLD.subject_key) THEN
        RAISE EXCEPTION 'collector lease identity is immutable'
            USING ERRCODE = '42501';
    END IF;

    IF NEW.fence_epoch < OLD.fence_epoch THEN
        RAISE EXCEPTION 'collector lease fence cannot move backwards'
            USING ERRCODE = '42501';
    END IF;

    IF OLD.owner_instance IS NOT NULL
       AND OLD.owner_instance <> session_instance
       AND OLD.slot_state = 'ACTIVE'
       AND OLD.lease_expires_at > pg_catalog.clock_timestamp() THEN
        RAISE EXCEPTION 'collector cannot change an active lease of another collector'
            USING ERRCODE = '42501';
    END IF;

    IF NEW.owner_instance = session_instance
       AND OLD.owner_instance IS DISTINCT FROM session_instance
       AND NEW.fence_epoch <= OLD.fence_epoch THEN
        RAISE EXCEPTION 'collector lease takeover must advance the fence'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$function$;

CREATE OR REPLACE FUNCTION public.enforce_source_observation_provenance_collector_identity()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    session_instance text := public.youtube_collector_session_instance();
BEGIN
    IF session_instance IS NOT NULL AND NEW.collector_instance IS DISTINCT FROM session_instance THEN
        RAISE EXCEPTION 'observation provenance does not match the database identity'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$function$;

CREATE OR REPLACE FUNCTION public.enforce_source_observation_collector_fence()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    session_instance text := public.youtube_collector_session_instance();
BEGIN
    IF session_instance IS NULL THEN
        RETURN NEW;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM public.source_observation_provenances AS provenance
        JOIN public.youtube_collection_job_leases AS lease
          ON lease.job_key = provenance.job_key
        WHERE provenance.id = NEW.provenance_id
          AND provenance.collector_instance = session_instance
          AND lease.collection_job_kind = provenance.collection_job_kind
          AND lease.provider = NEW.provider
          AND lease.owner_instance = session_instance
          AND lease.fence_epoch = NEW.fence_epoch
          AND lease.projection_generation = NEW.projection_generation
          AND lease.slot_state = 'ACTIVE'
          AND lease.lease_expires_at > pg_catalog.transaction_timestamp()
    ) THEN
        RAISE EXCEPTION 'observation is not fenced by an active lease of the database identity'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$function$;

CREATE OR REPLACE FUNCTION public.enforce_source_observation_queue_collector_fence()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    session_instance text := public.youtube_collector_session_instance();
BEGIN
    IF session_instance IS NULL THEN
        RETURN NEW;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM public.source_observations AS observation
        JOIN public.source_observation_provenances AS provenance
          ON provenance.id = observation.provenance_id
        JOIN public.youtube_collection_job_leases AS lease
          ON lease.job_key = provenance.job_key
        WHERE observation.id = NEW.observation_id
          AND provenance.collector_instance = session_instance
          AND lease.owner_instance = session_instance
          AND lease.fence_epoch = observation.fence_epoch
          AND lease.slot_state = 'ACTIVE'
          AND lease.lease_expires_at > pg_catalog.transaction_timestamp()
    ) THEN
        RAISE EXCEPTION 'observation queue insert is not fenced by an active lease of the database identity'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$function$;

CREATE OR REPLACE FUNCTION public.enforce_source_observation_collision_collector_identity()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    session_instance text := public.youtube_collector_session_instance();
BEGIN
    IF session_instance IS NOT NULL AND NEW.collector_instance IS DISTINCT FROM session_instance THEN
        RAISE EXCEPTION 'observation collision does not match the database identity'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$function$;

CREATE OR REPLACE TRIGGER enforce_youtube_collection_job_lease_collector_identity
BEFORE INSERT OR UPDATE ON public.youtube_collection_job_leases
FOR EACH ROW
EXECUTE FUNCTION public.enforce_youtube_collection_job_lease_collector_identity();

CREATE OR REPLACE TRIGGER enforce_source_observation_provenance_collector_identity
BEFORE INSERT ON public.source_observation_provenances
FOR EACH ROW
EXECUTE FUNCTION public.enforce_source_observation_provenance_collector_identity();

CREATE OR REPLACE TRIGGER enforce_source_observation_collector_fence
BEFORE INSERT ON public.source_observations
FOR EACH ROW
EXECUTE FUNCTION public.enforce_source_observation_collector_fence();

CREATE OR REPLACE TRIGGER enforce_source_observation_queue_collector_fence
BEFORE INSERT ON public.source_observation_queue
FOR EACH ROW
EXECUTE FUNCTION public.enforce_source_observation_queue_collector_fence();

CREATE OR REPLACE TRIGGER enforce_source_observation_collision_collector_identity
BEFORE INSERT ON public.source_observation_collisions
FOR EACH ROW
EXECUTE FUNCTION public.enforce_source_observation_collision_collector_identity();

REVOKE ALL ON FUNCTION public.youtube_collector_session_instance() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_youtube_collection_job_lease_collector_identity() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_source_observation_provenance_collector_identity() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_source_observation_collector_fence() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_source_observation_queue_collector_fence() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_source_observation_collision_collector_identity() FROM PUBLIC;

COMMIT;
