BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE FUNCTION public.collector_session_instance() RETURNS text
    LANGUAGE plpgsql STABLE
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
    CASE session_user
        WHEN 'hololive_collector_a' THEN RETURN 'collector-a';
        WHEN 'hololive_collector_b' THEN RETURN 'collector-b';
        WHEN 'hololive_collector_c' THEN RETURN 'collector-c';
        WHEN 'hololive_collector_d' THEN RETURN 'collector-d';
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
$$;

REVOKE ALL ON FUNCTION public.collector_session_instance() FROM PUBLIC;

CREATE OR REPLACE FUNCTION public.enforce_source_observation_collector_fence() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    session_instance text := public.collector_session_instance();
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
          AND lease.collection_job_kind = provenance.collection_job_kind
          AND lease.provider = NEW.provider
          AND lease.owner_instance = provenance.collector_instance
          AND pg_catalog.split_part(lease.owner_instance, ':', 1) = session_instance
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
$$;


CREATE OR REPLACE FUNCTION public.enforce_source_observation_collision_collector_identity() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    session_instance text := public.collector_session_instance();
BEGIN
    IF session_instance IS NOT NULL
       AND (pg_catalog.split_part(NEW.collector_instance, ':', 1) = session_instance) IS NOT TRUE THEN
        RAISE EXCEPTION 'observation collision does not match the database identity'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$$;


CREATE OR REPLACE FUNCTION public.enforce_source_observation_provenance_collector_identity() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    session_instance text := public.collector_session_instance();
BEGIN
    IF session_instance IS NOT NULL
       AND (pg_catalog.split_part(NEW.collector_instance, ':', 1) = session_instance) IS NOT TRUE THEN
        RAISE EXCEPTION 'observation provenance does not match the database identity'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$$;


CREATE OR REPLACE FUNCTION public.enforce_source_observation_queue_collector_fence() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    session_instance text := public.collector_session_instance();
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
          AND lease.owner_instance = provenance.collector_instance
          AND pg_catalog.split_part(lease.owner_instance, ':', 1) = session_instance
          AND lease.fence_epoch = observation.fence_epoch
          AND lease.slot_state = 'ACTIVE'
          AND lease.lease_expires_at > pg_catalog.transaction_timestamp()
    ) THEN
        RAISE EXCEPTION 'observation queue insert is not fenced by an active lease of the database identity'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$$;


CREATE OR REPLACE FUNCTION public.enforce_youtube_collection_job_lease_collector_identity() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    session_instance text := public.collector_session_instance();
BEGIN
    IF session_instance IS NULL THEN
        RETURN NEW;
    END IF;

    IF (TG_OP = 'INSERT' OR NEW.owner_instance IS NOT NULL)
       AND (pg_catalog.split_part(NEW.owner_instance, ':', 1) = session_instance) IS NOT TRUE THEN
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
       AND pg_catalog.split_part(OLD.owner_instance, ':', 1) <> session_instance
       AND OLD.slot_state = 'ACTIVE'
       AND OLD.lease_expires_at > pg_catalog.clock_timestamp() THEN
        RAISE EXCEPTION 'collector cannot change an active lease of another collector'
            USING ERRCODE = '42501';
    END IF;

    IF NEW.owner_instance IS NOT NULL
       AND NEW.owner_instance IS DISTINCT FROM OLD.owner_instance
       AND NEW.fence_epoch <= OLD.fence_epoch THEN
        RAISE EXCEPTION 'collector lease takeover must advance the fence'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$$;


CREATE OR REPLACE FUNCTION public.event_grade_collector_owner(p_owner text) RETURNS text
    LANGUAGE plpgsql STABLE
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    session_instance text;
BEGIN
    IF session_user <> 'hololive_collector_c' THEN
        RAISE EXCEPTION 'event grade collection is reserved for collector c' USING ERRCODE = '42501';
    END IF;
    session_instance := public.collector_session_instance();
    IF p_owner IS NULL OR length(p_owner) NOT BETWEEN 1 AND 128 OR session_instance IS NULL
       OR pg_catalog.split_part(p_owner, ':', 1) <> session_instance
       OR pg_catalog.split_part(p_owner, ':', 2) = '' THEN
        RAISE EXCEPTION 'event grade lease owner does not match the database identity' USING ERRCODE = '42501';
    END IF;
    RETURN p_owner;
END
$$;


DROP FUNCTION public.youtube_collector_session_instance();

COMMIT;
