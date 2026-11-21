BEGIN;
SET LOCAL lock_timeout = '3s';
ALTER TYPE public.alarm_type ADD VALUE 'EVENT_GRADE';

CREATE TABLE public.event_grade_catalogs (
    region text PRIMARY KEY CHECK (region IN ('jp','us','as')),
    observed_at timestamptz NOT NULL
);

CREATE TABLE public.event_grade_chapters (
    region text NOT NULL REFERENCES public.event_grade_catalogs,
    event_id varchar(96) NOT NULL CHECK (event_id ~ '^[A-Za-z0-9_-]+$'),
    chapter_id varchar(96) NOT NULL CHECK (chapter_id ~ '^[A-Za-z0-9_-]+$'),
    event_type text NOT NULL CHECK (event_type IN ('normal','relay')),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    event_ends_at timestamptz NOT NULL,
    reveals_at timestamptz NOT NULL,
    observed_at timestamptz NOT NULL,
    binding_conflicted boolean NOT NULL DEFAULT false,
    PRIMARY KEY (region,event_id,chapter_id),
    UNIQUE (region,chapter_id),
    CHECK (starts_at < ends_at AND ends_at <= event_ends_at AND ends_at <= reveals_at)
);
CREATE INDEX event_grade_chapters_due ON public.event_grade_chapters (region, greatest(event_ends_at,reveals_at));

CREATE TABLE public.event_grade_borders (
    region text NOT NULL,
    event_id varchar(96) NOT NULL,
    chapter_id varchar(96) NOT NULL,
    board_kind text NOT NULL CHECK (board_kind = 'marathon_score'),
    rank integer NOT NULL CHECK (rank > 0),
    score bigint NOT NULL CHECK (score >= 0),
    started_at timestamptz NOT NULL,
    observed_at timestamptz NOT NULL,
    is_final boolean NOT NULL,
    PRIMARY KEY (region,event_id,chapter_id,board_kind,is_final,rank),
    FOREIGN KEY (region,event_id,chapter_id) REFERENCES public.event_grade_chapters
);

CREATE TABLE public.event_grade_requests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    room_id varchar(100) NOT NULL,
    user_id varchar(100) NOT NULL,
    region text NOT NULL,
    event_id varchar(96) NOT NULL,
    chapter_id varchar(96) NOT NULL,
    board_kind text NOT NULL CHECK (board_kind = 'marathon_score'),
    score bigint NOT NULL CHECK (score > 0),
    outcome text CHECK (outcome IN ('outside','boundary','unavailable') OR outcome ~ '^top[1-9][0-9]{0,9}$'),
    published_at timestamptz,
    dispatch_rejection text CHECK (dispatch_rejection IN ('rejected_collision','rejected_terminal')),
    UNIQUE (room_id,user_id,region,event_id,chapter_id,board_kind),
    FOREIGN KEY (region,event_id,chapter_id) REFERENCES public.event_grade_chapters,
    CHECK (published_at IS NULL OR outcome IS NOT NULL),
    CHECK (dispatch_rejection IS NULL OR (outcome IS NOT NULL AND published_at IS NULL))
);
CREATE INDEX event_grade_requests_due ON public.event_grade_requests (region,event_id,chapter_id,id) WHERE outcome IS NULL;
CREATE INDEX event_grade_requests_pending ON public.event_grade_requests (id) WHERE outcome IS NOT NULL AND published_at IS NULL AND dispatch_rejection IS NULL;

CREATE TABLE public.event_grade_regions (
    region text PRIMARY KEY CHECK (region IN ('jp','us','as')),
    published_seq bigint NOT NULL DEFAULT 0 CHECK (published_seq >= 0),
    catalog_observed_at timestamptz,
    catalog_attempt_started_at timestamptz
);
INSERT INTO public.event_grade_regions(region) VALUES ('jp'),('us'),('as');

CREATE TABLE public.event_grade_collection_targets (
    region text NOT NULL REFERENCES public.event_grade_regions,
    event_id varchar(96) NOT NULL CHECK (event_id ~ '^[A-Za-z0-9_-]+$'),
    chapter_id varchar(96) NOT NULL CHECK (chapter_id ~ '^[A-Za-z0-9_-]+$'),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    final_after timestamptz NOT NULL,
    generation bigint NOT NULL DEFAULT 1 CHECK (generation > 0),
    conflicted boolean NOT NULL DEFAULT false,
    final_published boolean NOT NULL DEFAULT false,
    PRIMARY KEY (region,event_id,chapter_id),
    CHECK (starts_at < ends_at AND ends_at <= final_after)
);
CREATE INDEX event_grade_collection_targets_chapter ON public.event_grade_collection_targets (region,chapter_id);

CREATE TABLE public.event_grade_collection_leases (
    job_key text PRIMARY KEY CHECK (length(job_key) BETWEEN 1 AND 256),
    region text NOT NULL REFERENCES public.event_grade_regions,
    kind text NOT NULL CHECK (kind IN ('catalog','cutoff')),
    event_id varchar(96),
    chapter_id varchar(96),
    target_generation bigint,
    fence_epoch bigint NOT NULL DEFAULT 0 CHECK (fence_epoch >= 0),
    owner_instance text CHECK (owner_instance IS NULL OR length(owner_instance) BETWEEN 1 AND 128),
    claimed_at timestamptz,
    lease_expires_at timestamptz,
    catalog_interrupted boolean NOT NULL DEFAULT false,
    last_completed_at timestamptz,
    CHECK ((kind = 'catalog') = (event_id IS NULL AND chapter_id IS NULL AND target_generation IS NULL)),
    CHECK ((owner_instance IS NULL) = (lease_expires_at IS NULL) AND (owner_instance IS NULL) = (claimed_at IS NULL)),
    FOREIGN KEY (region,event_id,chapter_id) REFERENCES public.event_grade_collection_targets
);

CREATE TABLE public.event_grade_observations (
    region text NOT NULL REFERENCES public.event_grade_regions,
    seq bigint NOT NULL CHECK (seq > 0),
    schema_version smallint NOT NULL CHECK (schema_version = 1),
    kind text NOT NULL CHECK (kind IN ('catalog','cutoff')),
    job_key text NOT NULL,
    scheduled_for timestamptz NOT NULL,
    owner_instance text NOT NULL,
    fence_epoch bigint NOT NULL CHECK (fence_epoch > 0),
    target_generation bigint,
    started_at timestamptz NOT NULL,
    observed_at timestamptz NOT NULL,
    published_at timestamptz NOT NULL,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 65536),
    PRIMARY KEY (region,seq),
    UNIQUE (job_key,owner_instance,fence_epoch),
    CHECK (started_at <= observed_at AND observed_at <= published_at),
    CHECK ((kind = 'cutoff') = (target_generation IS NOT NULL))
);

CREATE TABLE public.event_grade_consumer_offsets (
    region text PRIMARY KEY REFERENCES public.event_grade_regions,
    applied_seq bigint NOT NULL DEFAULT 0 CHECK (applied_seq >= 0)
);
INSERT INTO public.event_grade_consumer_offsets(region) VALUES ('jp'),('us'),('as');

CREATE FUNCTION public.event_grade_collector_owner(p_owner text) RETURNS text
    LANGUAGE plpgsql STABLE
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    session_instance text;
BEGIN
    IF session_user <> 'hololive_collector_c' THEN
        RAISE EXCEPTION 'event grade collection is reserved for collector c' USING ERRCODE = '42501';
    END IF;
    session_instance := public.youtube_collector_session_instance();
    IF p_owner IS NULL OR length(p_owner) NOT BETWEEN 1 AND 128 OR session_instance IS NULL
       OR pg_catalog.split_part(p_owner, ':', 1) <> session_instance
       OR pg_catalog.split_part(p_owner, ':', 2) = '' THEN
        RAISE EXCEPTION 'event grade lease owner does not match the database identity' USING ERRCODE = '42501';
    END IF;
    RETURN p_owner;
END
$$;

CREATE FUNCTION public.event_grade_claim_catalog(p_region text, p_owner text, p_ttl_ms integer)
    RETURNS TABLE(job_key text, fence_epoch bigint, claimed_at timestamptz, interrupted boolean)
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    v_owner text := public.event_grade_collector_owner(p_owner);
    v_attempt_started timestamptz;
    v_key text := 'catalog:' || p_region;
BEGIN
    IF p_ttl_ms NOT BETWEEN 1000 AND 300000 THEN
        RAISE EXCEPTION 'event grade lease TTL is outside bounds' USING ERRCODE = '22023';
    END IF;
    SELECT r.catalog_attempt_started_at INTO v_attempt_started FROM public.event_grade_regions r WHERE r.region = p_region FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'unknown event grade region' USING ERRCODE = '22023';
    END IF;
    INSERT INTO public.event_grade_collection_leases AS l (job_key, region, kind)
    VALUES (v_key, p_region, 'catalog') ON CONFLICT ON CONSTRAINT event_grade_collection_leases_pkey DO NOTHING;
    RETURN QUERY
    UPDATE public.event_grade_collection_leases AS l
    SET fence_epoch = l.fence_epoch + 1, owner_instance = v_owner,
        claimed_at = pg_catalog.statement_timestamp(),
        lease_expires_at = pg_catalog.statement_timestamp() + p_ttl_ms * interval '1 millisecond',
        catalog_interrupted = v_attempt_started IS NOT NULL
    WHERE l.job_key = v_key
      AND (l.owner_instance IS NULL OR l.lease_expires_at <= pg_catalog.statement_timestamp())
      AND (l.last_completed_at IS NULL OR l.last_completed_at <= pg_catalog.statement_timestamp() - interval '50 seconds')
    RETURNING l.job_key, l.fence_epoch, l.claimed_at, l.catalog_interrupted;
    IF FOUND THEN
        UPDATE public.event_grade_regions r SET catalog_attempt_started_at = pg_catalog.statement_timestamp() WHERE r.region = p_region;
    END IF;
END
$$;

CREATE FUNCTION public.event_grade_claim_cutoffs(p_region text, p_owner text, p_ttl_ms integer, p_limit integer)
    RETURNS TABLE(job_key text, event_id text, chapter_id text, final_after timestamptz, binding_observed_at timestamptz, fence_epoch bigint, target_generation bigint, claimed_at timestamptz)
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    v_owner text := public.event_grade_collector_owner(p_owner);
    v_catalog_observed timestamptz;
BEGIN
    IF p_ttl_ms NOT BETWEEN 1000 AND 300000 OR p_limit NOT BETWEEN 1 AND 3 THEN
        RAISE EXCEPTION 'event grade cutoff claim is outside bounds' USING ERRCODE = '22023';
    END IF;
    SELECT r.catalog_observed_at INTO v_catalog_observed FROM public.event_grade_regions r WHERE r.region = p_region FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'unknown event grade region' USING ERRCODE = '22023';
    END IF;
    INSERT INTO public.event_grade_collection_leases AS l (job_key, region, kind, event_id, chapter_id, target_generation)
    SELECT 'cutoff:' || t.region || ':' || t.event_id || ':' || t.chapter_id, t.region, 'cutoff', t.event_id, t.chapter_id, t.generation
    FROM public.event_grade_collection_targets t
    WHERE t.region = p_region
    ON CONFLICT ON CONSTRAINT event_grade_collection_leases_pkey DO NOTHING;
    RETURN QUERY
    WITH candidates AS (
        SELECT l.job_key, t.generation
        FROM public.event_grade_collection_targets t
        JOIN public.event_grade_collection_leases l
          ON (l.region, l.event_id, l.chapter_id) = (t.region, t.event_id, t.chapter_id)
        WHERE t.region = p_region AND NOT t.conflicted AND NOT t.final_published
          AND (l.owner_instance IS NULL OR l.lease_expires_at <= pg_catalog.statement_timestamp())
          AND (
              (t.starts_at <= pg_catalog.statement_timestamp() AND t.ends_at > pg_catalog.statement_timestamp()
               AND v_catalog_observed >= pg_catalog.statement_timestamp() - interval '2 minutes')
              OR (t.final_after <= pg_catalog.statement_timestamp()
                  AND t.final_after + interval '24 hours' >= pg_catalog.statement_timestamp())
          )
        ORDER BY l.last_completed_at NULLS FIRST, t.final_after, t.event_id, t.chapter_id
        LIMIT p_limit
        FOR UPDATE OF l
    )
    UPDATE public.event_grade_collection_leases AS l
    SET fence_epoch = l.fence_epoch + 1, owner_instance = v_owner, target_generation = c.generation,
        claimed_at = pg_catalog.statement_timestamp(),
        lease_expires_at = pg_catalog.statement_timestamp() + p_ttl_ms * interval '1 millisecond'
    FROM candidates c, public.event_grade_collection_targets t
    WHERE l.job_key = c.job_key AND (t.region, t.event_id, t.chapter_id) = (l.region, l.event_id, l.chapter_id)
    RETURNING l.job_key, l.event_id::text, l.chapter_id::text, t.final_after, v_catalog_observed, l.fence_epoch, l.target_generation, l.claimed_at;
END
$$;

CREATE FUNCTION public.event_grade_release(p_job_key text, p_owner text, p_epoch bigint) RETURNS boolean
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    v_owner text := public.event_grade_collector_owner(p_owner);
BEGIN
    UPDATE public.event_grade_collection_leases AS l
    SET owner_instance = NULL, claimed_at = NULL, lease_expires_at = NULL
    WHERE l.job_key = p_job_key AND l.owner_instance = v_owner AND l.fence_epoch = p_epoch;
    RETURN FOUND;
END
$$;

CREATE FUNCTION public.event_grade_publish(p_job_key text, p_owner text, p_epoch bigint, p_started_at timestamptz, p_observed_at timestamptz, p_payload jsonb)
    RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    v_owner text := public.event_grade_collector_owner(p_owner);
    lease record;
    target record;
    existing record;
    lease_region text;
    next_seq bigint;
    body jsonb := p_payload;
    now_at timestamptz := pg_catalog.statement_timestamp();
BEGIN
    SELECT l.region INTO lease_region FROM public.event_grade_collection_leases l WHERE l.job_key = p_job_key;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'event grade lease does not exist' USING ERRCODE = '42501';
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock(20261121, CASE lease_region WHEN 'jp' THEN 1 WHEN 'us' THEN 2 ELSE 3 END);
    PERFORM 1 FROM public.event_grade_regions r WHERE r.region = lease_region FOR UPDATE;
    SELECT l.job_key, l.region, l.kind, l.event_id, l.chapter_id, l.target_generation, l.fence_epoch, l.owner_instance,
           l.claimed_at, l.lease_expires_at, l.catalog_interrupted
    INTO lease FROM public.event_grade_collection_leases l WHERE l.job_key = p_job_key FOR UPDATE;

    SELECT o.seq, o.started_at, o.observed_at, o.payload INTO existing FROM public.event_grade_observations o
    WHERE o.job_key = p_job_key AND o.owner_instance = v_owner AND o.fence_epoch = p_epoch;
    IF FOUND THEN
        IF existing.started_at = p_started_at AND existing.observed_at = p_observed_at
           AND existing.payload - 'quarantine_missing' = p_payload - 'quarantine_missing' THEN
            RETURN existing.seq;
        END IF;
        RAISE EXCEPTION 'event grade observation conflicts with the published execution' USING ERRCODE = '23505';
    END IF;

    IF lease.owner_instance IS DISTINCT FROM v_owner OR lease.fence_epoch <> p_epoch
       OR lease.lease_expires_at <= now_at THEN
        RAISE EXCEPTION 'event grade lease proof is not current' USING ERRCODE = '42501';
    END IF;
    IF p_started_at IS NULL OR p_observed_at IS NULL OR p_started_at < pg_catalog.date_trunc('milliseconds', lease.claimed_at) - interval '2 seconds'
       OR p_started_at > p_observed_at OR p_observed_at > now_at + interval '2 seconds'
       OR jsonb_typeof(p_payload) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'event grade observation timing or payload is invalid' USING ERRCODE = '22023';
    END IF;

    IF lease.kind = 'cutoff' THEN
        SELECT t.generation, t.conflicted, t.final_published, t.final_after INTO target FROM public.event_grade_collection_targets t
        WHERE (t.region, t.event_id, t.chapter_id) = (lease.region, lease.event_id, lease.chapter_id) FOR UPDATE;
        IF target.generation <> lease.target_generation OR target.conflicted OR target.final_published THEN
            RAISE EXCEPTION 'event grade cutoff target changed after claim' USING ERRCODE = '40001';
        END IF;
        IF jsonb_typeof(p_payload->'final') IS DISTINCT FROM 'boolean' OR jsonb_typeof(p_payload->'ranks') IS DISTINCT FROM 'array'
           OR p_payload->>'event_id' IS DISTINCT FROM lease.event_id
           OR p_payload->>'chapter_id' IS DISTINCT FROM lease.chapter_id THEN
            RAISE EXCEPTION 'event grade cutoff payload does not match the claimed target' USING ERRCODE = '22023';
        END IF;
        IF (p_payload->>'final')::boolean THEN
            IF p_started_at < target.final_after OR p_observed_at > target.final_after + interval '24 hours' THEN
                RAISE EXCEPTION 'event grade final cutoff is outside its window' USING ERRCODE = '22023';
            END IF;
            UPDATE public.event_grade_collection_targets t SET final_published = true
            WHERE (t.region, t.event_id, t.chapter_id) = (lease.region, lease.event_id, lease.chapter_id);
        END IF;
    ELSE
        IF jsonb_typeof(p_payload->'chapters') IS DISTINCT FROM 'array' OR jsonb_typeof(p_payload->'bindings') IS DISTINCT FROM 'array'
           OR jsonb_typeof(p_payload->'conflicts') IS DISTINCT FROM 'array'
           OR jsonb_typeof(p_payload->'quarantine_missing') IS DISTINCT FROM 'boolean' THEN
            RAISE EXCEPTION 'event grade catalog payload is invalid' USING ERRCODE = '22023';
        END IF;
        body := p_payload || jsonb_build_object('quarantine_missing', (p_payload->>'quarantine_missing')::boolean OR lease.catalog_interrupted);
        PERFORM public.event_grade_apply_catalog_targets(lease.region, body);
        UPDATE public.event_grade_regions r
        SET catalog_observed_at = greatest(r.catalog_observed_at, p_observed_at), catalog_attempt_started_at = NULL
        WHERE r.region = lease.region;
    END IF;

    UPDATE public.event_grade_regions r SET published_seq = r.published_seq + 1
    WHERE r.region = lease.region RETURNING r.published_seq INTO next_seq;
    INSERT INTO public.event_grade_observations(region, seq, schema_version, kind, job_key, scheduled_for, owner_instance,
        fence_epoch, target_generation, started_at, observed_at, published_at, payload)
    VALUES (lease.region, next_seq, 1, lease.kind, lease.job_key, lease.claimed_at, v_owner, lease.fence_epoch,
        lease.target_generation, p_started_at, p_observed_at, now_at, body);
    UPDATE public.event_grade_collection_leases l
    SET owner_instance = NULL, claimed_at = NULL, lease_expires_at = NULL, catalog_interrupted = false, last_completed_at = now_at
    WHERE l.job_key = lease.job_key;
    RETURN next_seq;
END
$$;

CREATE FUNCTION public.event_grade_apply_catalog_targets(p_region text, p_body jsonb) RETURNS void
    LANGUAGE plpgsql
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    v_chapter jsonb;
BEGIN
    IF (p_body->>'quarantine_missing')::boolean THEN
        UPDATE public.event_grade_collection_targets t
        SET conflicted = true, generation = t.generation + 1
        WHERE t.region = p_region AND NOT t.conflicted
          AND t.chapter_id NOT IN (SELECT value->>'chapter_id' FROM jsonb_array_elements(p_body->'bindings'));
    END IF;
    UPDATE public.event_grade_collection_targets t
    SET conflicted = true, generation = t.generation + 1
    WHERE t.region = p_region AND NOT t.conflicted
      AND t.chapter_id IN (SELECT value #>> '{}' FROM jsonb_array_elements(p_body->'conflicts'));
    UPDATE public.event_grade_collection_targets t
    SET conflicted = true, generation = t.generation + 1
    FROM jsonb_array_elements(p_body->'bindings') b
    WHERE t.region = p_region AND NOT t.conflicted
      AND t.chapter_id = b.value->>'chapter_id' AND t.event_id <> b.value->>'event_id';
    FOR v_chapter IN SELECT value FROM jsonb_array_elements(p_body->'chapters') LOOP
        CONTINUE WHEN EXISTS (
            SELECT 1 FROM public.event_grade_collection_targets t
            WHERE t.region = p_region AND t.chapter_id = v_chapter->>'chapter_id' AND t.event_id <> v_chapter->>'event_id');
        INSERT INTO public.event_grade_collection_targets AS t (region, event_id, chapter_id, starts_at, ends_at, final_after)
        VALUES (p_region, v_chapter->>'event_id', v_chapter->>'chapter_id', (v_chapter->>'starts_at')::timestamptz,
                (v_chapter->>'ends_at')::timestamptz,
                greatest((v_chapter->>'event_ends_at')::timestamptz, (v_chapter->>'reveals_at')::timestamptz))
        ON CONFLICT ON CONSTRAINT event_grade_collection_targets_pkey DO UPDATE
        SET starts_at = EXCLUDED.starts_at, ends_at = EXCLUDED.ends_at, final_after = EXCLUDED.final_after,
            conflicted = false,
            final_published = t.final_published AND t.final_after = EXCLUDED.final_after AND NOT t.conflicted,
            generation = t.generation + CASE WHEN t.final_after = EXCLUDED.final_after AND NOT t.conflicted THEN 0 ELSE 1 END;
    END LOOP;
END
$$;

REVOKE ALL ON FUNCTION public.event_grade_collector_owner(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.event_grade_claim_catalog(text,text,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.event_grade_claim_cutoffs(text,text,integer,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.event_grade_release(text,text,bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.event_grade_publish(text,text,bigint,timestamptz,timestamptz,jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.event_grade_apply_catalog_targets(text,jsonb) FROM PUBLIC;

DO $acl$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT DISTINCT format('%s %s', CASE object.relkind WHEN 'S' THEN 'SEQUENCE' ELSE 'TABLE' END, object.oid::regclass) AS target,
               role.rolname AS grantee
        FROM pg_catalog.pg_class object
        CROSS JOIN LATERAL pg_catalog.aclexplode(object.relacl) privilege
        JOIN pg_catalog.pg_roles role ON role.oid = privilege.grantee
        WHERE object.relnamespace = 'public'::regnamespace AND object.relname LIKE 'event\_grade\_%'
          AND object.relkind IN ('r','S') AND privilege.grantee <> object.relowner
        UNION
        SELECT DISTINCT format('FUNCTION %s', object.oid::regprocedure), role.rolname
        FROM pg_catalog.pg_proc object
        CROSS JOIN LATERAL pg_catalog.aclexplode(object.proacl) privilege
        JOIN pg_catalog.pg_roles role ON role.oid = privilege.grantee
        WHERE object.pronamespace = 'public'::regnamespace AND object.proname LIKE 'event\_grade\_%'
          AND privilege.grantee <> object.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON %s FROM %I', item.target, item.grantee);
    END LOOP;
END
$acl$;
DO $acl$
BEGIN
    IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_alarm_worker') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.event_grade_catalogs, public.event_grade_chapters, public.event_grade_requests TO hololive_alarm_worker;
        GRANT SELECT,INSERT,DELETE ON TABLE public.event_grade_borders TO hololive_alarm_worker;
        GRANT USAGE ON SEQUENCE public.event_grade_requests_id_seq TO hololive_alarm_worker;
        GRANT SELECT ON TABLE public.event_grade_observations TO hololive_alarm_worker;
        GRANT SELECT,UPDATE ON TABLE public.event_grade_consumer_offsets TO hololive_alarm_worker;
        GRANT SELECT ON TABLE public.event_grade_regions, public.event_grade_collection_leases TO hololive_alarm_worker;
    END IF;
    IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_collector_c') THEN
        GRANT EXECUTE ON FUNCTION public.event_grade_claim_catalog(text,text,integer),
            public.event_grade_claim_cutoffs(text,text,integer,integer),
            public.event_grade_release(text,text,bigint),
            public.event_grade_publish(text,text,bigint,timestamptz,timestamptz,jsonb) TO hololive_collector_c;
    END IF;
END
$acl$;
COMMIT;
