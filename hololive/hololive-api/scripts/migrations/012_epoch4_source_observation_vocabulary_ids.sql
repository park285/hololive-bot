BEGIN;
SET LOCAL lock_timeout = '3s';

CREATE TABLE public.source_observation_providers (
    id smallint NOT NULL,
    name text NOT NULL,
    CONSTRAINT pk_source_observation_providers PRIMARY KEY (id),
    CONSTRAINT uq_source_observation_providers_name UNIQUE (name),
    CONSTRAINT uq_source_observation_providers_id_name UNIQUE (id, name),
    CONSTRAINT chk_source_observation_providers_bounds CHECK (id > 0 AND length(name) BETWEEN 1 AND 64)
);

INSERT INTO public.source_observation_providers (id, name) VALUES
    (1, 'holodex'),
    (2, 'youtubejs'),
    (3, 'hololive_official');

CREATE TABLE public.source_observation_kinds (
    id smallint NOT NULL,
    name text NOT NULL,
    CONSTRAINT pk_source_observation_kinds PRIMARY KEY (id),
    CONSTRAINT uq_source_observation_kinds_name UNIQUE (name),
    CONSTRAINT uq_source_observation_kinds_id_name UNIQUE (id, name),
    CONSTRAINT chk_source_observation_kinds_bounds CHECK (id > 0 AND length(name) BETWEEN 1 AND 64)
);

INSERT INTO public.source_observation_kinds (id, name) VALUES
    (1, 'community_page'),
    (2, 'video_list'),
    (3, 'shorts_list'),
    (4, 'live_snapshot'),
    (5, 'channel_profile'),
    (6, 'channel_photo'),
    (7, 'schedule_snapshot'),
    (8, 'channel_live_check'),
    (9, 'video_live_check');

CREATE TABLE public.source_observation_completeness_values (
    id smallint NOT NULL,
    name text NOT NULL,
    CONSTRAINT pk_source_observation_completeness_values PRIMARY KEY (id),
    CONSTRAINT uq_source_observation_completeness_values_name UNIQUE (name),
    CONSTRAINT uq_source_observation_completeness_values_id_name UNIQUE (id, name),
    CONSTRAINT chk_source_observation_completeness_values_bounds CHECK (id > 0 AND length(name) BETWEEN 1 AND 64)
);

INSERT INTO public.source_observation_completeness_values (id, name) VALUES
    (1, 'COMPLETE'),
    (2, 'PARTIAL'),
    (3, 'UNKNOWN');

CREATE TABLE public.source_observation_continuity_values (
    id smallint NOT NULL,
    name text NOT NULL,
    CONSTRAINT pk_source_observation_continuity_values PRIMARY KEY (id),
    CONSTRAINT uq_source_observation_continuity_values_name UNIQUE (name),
    CONSTRAINT uq_source_observation_continuity_values_id_name UNIQUE (id, name),
    CONSTRAINT chk_source_observation_continuity_values_bounds CHECK (id > 0 AND length(name) BETWEEN 1 AND 64)
);

INSERT INTO public.source_observation_continuity_values (id, name) VALUES
    (1, 'CONTIGUOUS'),
    (2, 'GAP_UNRESOLVED'),
    (3, 'NOT_APPLICABLE');

DO $acl$
DECLARE
    dictionary regclass;
    item record;
BEGIN
    FOREACH dictionary IN ARRAY ARRAY[
        'public.source_observation_providers'::regclass, 'public.source_observation_kinds'::regclass,
        'public.source_observation_completeness_values'::regclass, 'public.source_observation_continuity_values'::regclass]
    LOOP
        EXECUTE format('REVOKE ALL ON TABLE %s FROM PUBLIC', dictionary);
        FOR item IN
            SELECT DISTINCT r.rolname
            FROM pg_catalog.pg_class c
            CROSS JOIN LATERAL pg_catalog.aclexplode(c.relacl) a
            JOIN pg_catalog.pg_roles r ON r.oid = a.grantee
            WHERE c.oid = dictionary AND a.grantee <> c.relowner
        LOOP
            EXECUTE format('REVOKE ALL ON TABLE %s FROM %I', dictionary, item.rolname);
        END LOOP;
        IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_runtime') THEN
            EXECUTE format('GRANT SELECT ON TABLE %s TO hololive_runtime', dictionary);
        END IF;
        IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_scraper') THEN
            EXECUTE format('GRANT SELECT ON TABLE %s TO hololive_scraper', dictionary);
        END IF;
    END LOOP;
END
$acl$;

LOCK TABLE public.observation_contract_generations IN ACCESS EXCLUSIVE MODE;
LOCK TABLE public.source_observations IN ACCESS EXCLUSIVE MODE;

ALTER TABLE public.observation_contract_generations
    ADD COLUMN provider_id smallint,
    ADD COLUMN observation_kind_id smallint;

ALTER TABLE public.observation_contract_generations
    ALTER COLUMN provider_id SET DATA TYPE smallint USING (CASE provider WHEN 'holodex' THEN 1 WHEN 'youtubejs' THEN 2 WHEN 'hololive_official' THEN 3 END),
    ALTER COLUMN observation_kind_id SET DATA TYPE smallint USING (CASE observation_kind WHEN 'community_page' THEN 1 WHEN 'video_list' THEN 2 WHEN 'shorts_list' THEN 3 WHEN 'live_snapshot' THEN 4 WHEN 'channel_profile' THEN 5 WHEN 'channel_photo' THEN 6 WHEN 'schedule_snapshot' THEN 7 WHEN 'channel_live_check' THEN 8 WHEN 'video_live_check' THEN 9 END),
    ALTER COLUMN provider_id SET NOT NULL,
    ALTER COLUMN observation_kind_id SET NOT NULL,
    ADD CONSTRAINT fk_observation_contract_provider_name FOREIGN KEY (provider_id, provider)
        REFERENCES public.source_observation_providers (id, name) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_observation_contract_kind_name FOREIGN KEY (observation_kind_id, observation_kind)
        REFERENCES public.source_observation_kinds (id, name) ON DELETE RESTRICT,
    ADD CONSTRAINT uq_observation_contract_generation_ids UNIQUE (provider_id, observation_kind_id);

ALTER TABLE public.source_observations
    ADD COLUMN provider_id smallint,
    ADD COLUMN observation_kind_id smallint,
    ADD COLUMN completeness_id smallint,
    ADD COLUMN continuity_id smallint;

ALTER TABLE public.source_observations
    ALTER COLUMN provider_id SET DATA TYPE smallint USING (CASE provider WHEN 'holodex' THEN 1 WHEN 'youtubejs' THEN 2 WHEN 'hololive_official' THEN 3 END),
    ALTER COLUMN observation_kind_id SET DATA TYPE smallint USING (CASE observation_kind WHEN 'community_page' THEN 1 WHEN 'video_list' THEN 2 WHEN 'shorts_list' THEN 3 WHEN 'live_snapshot' THEN 4 WHEN 'channel_profile' THEN 5 WHEN 'channel_photo' THEN 6 WHEN 'schedule_snapshot' THEN 7 WHEN 'channel_live_check' THEN 8 WHEN 'video_live_check' THEN 9 END),
    ALTER COLUMN completeness_id SET DATA TYPE smallint USING (CASE completeness WHEN 'COMPLETE' THEN 1 WHEN 'PARTIAL' THEN 2 WHEN 'UNKNOWN' THEN 3 END),
    ALTER COLUMN continuity_id SET DATA TYPE smallint USING (CASE continuity WHEN 'CONTIGUOUS' THEN 1 WHEN 'GAP_UNRESOLVED' THEN 2 WHEN 'NOT_APPLICABLE' THEN 3 END),
    ALTER COLUMN provider_id SET NOT NULL,
    ALTER COLUMN observation_kind_id SET NOT NULL,
    ALTER COLUMN completeness_id SET NOT NULL,
    ALTER COLUMN continuity_id SET NOT NULL,
    ADD CONSTRAINT fk_source_observation_provider_name FOREIGN KEY (provider_id, provider)
        REFERENCES public.source_observation_providers (id, name) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_source_observation_kind_name FOREIGN KEY (observation_kind_id, observation_kind)
        REFERENCES public.source_observation_kinds (id, name) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_source_observation_completeness_name FOREIGN KEY (completeness_id, completeness)
        REFERENCES public.source_observation_completeness_values (id, name) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_source_observation_continuity_name FOREIGN KEY (continuity_id, continuity)
        REFERENCES public.source_observation_continuity_values (id, name) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_source_observation_contract_ids FOREIGN KEY (provider_id, observation_kind_id)
        REFERENCES public.observation_contract_generations (provider_id, observation_kind_id) ON DELETE RESTRICT;

CREATE INDEX idx_source_observations_kind_id_received_id
    ON public.source_observations (observation_kind_id, received_at, id);

CREATE FUNCTION public.derive_source_observation_vocabulary() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
    NEW.provider_id := coalesce(NEW.provider_id,
        (SELECT v.id FROM public.source_observation_providers AS v WHERE v.name = NEW.provider));
    NEW.provider := coalesce(NEW.provider,
        (SELECT v.name FROM public.source_observation_providers AS v WHERE v.id = NEW.provider_id));
    NEW.observation_kind_id := coalesce(NEW.observation_kind_id,
        (SELECT v.id FROM public.source_observation_kinds AS v WHERE v.name = NEW.observation_kind));
    NEW.observation_kind := coalesce(NEW.observation_kind,
        (SELECT v.name FROM public.source_observation_kinds AS v WHERE v.id = NEW.observation_kind_id));
    NEW.completeness_id := coalesce(NEW.completeness_id,
        (SELECT v.id FROM public.source_observation_completeness_values AS v WHERE v.name = NEW.completeness));
    NEW.completeness := coalesce(NEW.completeness,
        (SELECT v.name FROM public.source_observation_completeness_values AS v WHERE v.id = NEW.completeness_id));
    NEW.continuity_id := coalesce(NEW.continuity_id,
        (SELECT v.id FROM public.source_observation_continuity_values AS v WHERE v.name = NEW.continuity));
    NEW.continuity := coalesce(NEW.continuity,
        (SELECT v.name FROM public.source_observation_continuity_values AS v WHERE v.id = NEW.continuity_id));
    RETURN NEW;
END
$$;

REVOKE ALL ON FUNCTION public.derive_source_observation_vocabulary() FROM PUBLIC;

CREATE TRIGGER derive_source_observation_vocabulary BEFORE INSERT ON public.source_observations
    FOR EACH ROW EXECUTE FUNCTION public.derive_source_observation_vocabulary();

CREATE OR REPLACE FUNCTION public.enforce_source_observation_collector_fence()
 RETURNS trigger
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog'
AS $function$
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
        JOIN public.source_observation_providers AS provider_value
          ON provider_value.name = lease.provider
        WHERE provenance.id = NEW.provenance_id
          AND lease.collection_job_kind = provenance.collection_job_kind
          AND provider_value.id = NEW.provider_id
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
$function$;

CREATE OR REPLACE FUNCTION public.delete_youtube_content_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer)
 RETURNS TABLE(deleted_scheduled_for timestamp with time zone)
 LANGUAGE sql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog'
AS $function$
    WITH active_old_content AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PENDING'
          AND observation.observation_kind_id IN (SELECT kind.id FROM public.source_observation_kinds AS kind WHERE kind.name IN ('video_list', 'shorts_list'))
          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff
        LIMIT 1
    ),
    processing_old_content AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PROCESSING'
          AND observation.observation_kind_id IN (SELECT kind.id FROM public.source_observation_kinds AS kind WHERE kind.name IN ('video_list', 'shorts_list'))
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
$function$;

CREATE OR REPLACE FUNCTION public.delete_youtube_live_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer)
 RETURNS TABLE(deleted_id bigint)
 LANGUAGE sql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog'
AS $function$
    WITH active_old_live AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PENDING'
          AND observation.observation_kind_id IN (SELECT kind.id FROM public.source_observation_kinds AS kind WHERE kind.name IN ('live_snapshot'))
          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff
        LIMIT 1
    ),
    processing_old_live AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PROCESSING'
          AND observation.observation_kind_id IN (SELECT kind.id FROM public.source_observation_kinds AS kind WHERE kind.name IN ('live_snapshot'))
          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff
        LIMIT 1
    ),
    candidates AS (
        SELECT slot.observation_id
        FROM public.youtube_live_absence_slots AS slot
        WHERE slot.scheduled_for < requested_cutoff
          AND NOT EXISTS (SELECT 1 FROM active_old_live)
          AND NOT EXISTS (SELECT 1 FROM processing_old_live)
        ORDER BY slot.scheduled_for, slot.observation_id
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF slot SKIP LOCKED
    )
    DELETE FROM public.youtube_live_absence_slots AS slot
    USING candidates AS candidate
    WHERE slot.observation_id = candidate.observation_id
      AND slot.scheduled_for < requested_cutoff
    RETURNING slot.observation_id
$function$;

CREATE OR REPLACE FUNCTION public.lock_source_observation(requested_observation_id bigint)
 RETURNS TABLE(provider text, observation_kind text, subject_key text, observation_key text, schema_version smallint, contract_generation bigint, evidence_sha256 text)
 LANGUAGE sql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog'
AS $function$
    SELECT provider_value.name, kind.name, observation.subject_key,
           encode(observation.observation_key, 'hex'), observation.schema_version, observation.contract_generation,
           encode(observation.evidence_sha256, 'hex')
    FROM public.source_observations observation
    JOIN public.source_observation_providers AS provider_value ON provider_value.id = observation.provider_id
    JOIN public.source_observation_kinds AS kind ON kind.id = observation.observation_kind_id
    WHERE observation.id = requested_observation_id
    FOR SHARE OF observation
$function$;

CREATE OR REPLACE FUNCTION public.lock_source_observation_identity(requested_provider text, requested_observation_kind text, requested_subject_key text, requested_observation_key text, requested_schema_version smallint, requested_contract_generation bigint)
 RETURNS TABLE(id bigint, evidence_sha256 text)
 LANGUAGE sql
 SECURITY DEFINER ROWS 1
 SET search_path TO 'pg_catalog'
AS $function$
    SELECT observation.id, encode(observation.evidence_sha256, 'hex')
    FROM public.source_observations observation
    WHERE observation.provider_id = (SELECT v.id FROM public.source_observation_providers AS v WHERE v.name = requested_provider)
      AND observation.observation_kind_id = (SELECT v.id FROM public.source_observation_kinds AS v WHERE v.name = requested_observation_kind)
      AND observation.subject_key = requested_subject_key
      AND observation.observation_key = decode(requested_observation_key, 'hex')
      AND observation.schema_version = requested_schema_version
      AND observation.contract_generation = requested_contract_generation
    FOR SHARE OF observation
$function$;

CREATE OR REPLACE FUNCTION public.delete_source_observation_retention_window(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer, OUT deleted_ids bigint[], OUT passed integer, OUT traversal_completed boolean)
 RETURNS record
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog'
 SET jit TO 'off'
AS $function$
DECLARE
    scan_limit CONSTANT integer := 1000;
    previous_at timestamptz;
    previous_id bigint;
    upper_at timestamptz;
    high_id bigint;
    candidate_ids bigint[];
    candidate_times timestamptz[];
    locked_ids bigint[];
    locked_times timestamptz[];
    scanned integer;
    locked integer;
BEGIN
    deleted_ids := '{}';
    passed := 0;
    traversal_completed := false;

    IF requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000
       OR cardinality(requested_kinds) IS NULL OR cardinality(requested_kinds) NOT BETWEEN 1 AND 16
       OR cardinality(requested_cutoffs) IS DISTINCT FROM cardinality(requested_kinds) THEN
        RETURN;
    END IF;

    PERFORM public.lock_youtube_live_runtime_state();
    SELECT state.cursor_received_at, state.cursor_id, state.upper_received_at, state.upper_id
    INTO STRICT previous_at, previous_id, upper_at, high_id
    FROM public.source_observation_retention_state state WHERE singleton FOR UPDATE;

    IF upper_at IS NULL THEN
        SELECT edge.received_at, edge.id INTO upper_at, high_id
        FROM (
            SELECT kind.id AS kind_id, max(requested_cutoffs[p]) AS cutoff
            FROM generate_subscripts(requested_kinds, 1) p
            JOIN public.source_observation_kinds AS kind ON kind.name = requested_kinds[p]
            GROUP BY kind.id
        ) policy CROSS JOIN LATERAL (
            SELECT observation.received_at, observation.id
            FROM public.source_observations observation
            WHERE observation.observation_kind_id = policy.kind_id AND observation.received_at < policy.cutoff
            ORDER BY observation.received_at DESC, observation.id DESC LIMIT 1
        ) edge ORDER BY edge.received_at DESC, edge.id DESC LIMIT 1;
    END IF;

    WITH policies AS (
        SELECT kind.id AS kind_id, max(requested_cutoffs[p]) AS cutoff
        FROM generate_subscripts(requested_kinds, 1) p
        JOIN public.source_observation_kinds AS kind ON kind.name = requested_kinds[p]
        GROUP BY kind.id
    ), scan_window AS MATERIALIZED (
        SELECT candidate.id, candidate.received_at
        FROM policies policy CROSS JOIN LATERAL (
            SELECT observation.id, observation.received_at FROM public.source_observations observation
            WHERE observation.observation_kind_id = policy.kind_id AND observation.received_at < policy.cutoff
              AND (observation.received_at, observation.id) > (previous_at, previous_id)
              AND (observation.received_at, observation.id) <= (upper_at, high_id)
            ORDER BY observation.received_at, observation.id LIMIT scan_limit
        ) candidate ORDER BY candidate.received_at, candidate.id LIMIT scan_limit
    )
    SELECT array_agg(id ORDER BY received_at, id), array_agg(received_at ORDER BY received_at, id)
    INTO candidate_ids, candidate_times FROM scan_window;
    scanned := coalesce(cardinality(candidate_ids), 0);

    WITH protected AS MATERIALIZED (
        SELECT id FROM public.source_observation_retention_protected_ids(candidate_ids)
    )
    SELECT array_agg(candidate.id ORDER BY candidate.received_at, candidate.id),
           array_agg(candidate.received_at ORDER BY candidate.received_at, candidate.id)
    INTO locked_ids, locked_times
    FROM (
        SELECT observation.id, observation.received_at FROM public.source_observations observation
        WHERE observation.id = ANY(candidate_ids)
          AND NOT EXISTS (SELECT 1 FROM protected WHERE protected.id = observation.id)
        ORDER BY observation.received_at, observation.id
        LIMIT requested_limit FOR UPDATE OF observation SKIP LOCKED
    ) candidate;
    locked := coalesce(cardinality(locked_ids), 0);
    WITH protected AS MATERIALIZED (
        SELECT id FROM public.source_observation_retention_protected_ids(locked_ids)
    ), deleted AS (
        DELETE FROM public.source_observations observation
        WHERE observation.id = ANY(locked_ids)
          AND NOT EXISTS (SELECT 1 FROM protected WHERE protected.id = observation.id)
        RETURNING observation.id
    )
    SELECT coalesce(array_agg(deleted.id), '{}') INTO deleted_ids FROM deleted;

    IF locked = requested_limit THEN
        previous_at := locked_times[locked];
        previous_id := locked_ids[locked];
        passed := array_position(candidate_ids, previous_id);
    ELSE
        previous_at := candidate_times[scanned];
        previous_id := candidate_ids[scanned];
        passed := scanned;
    END IF;
    IF scanned = 0 OR (scanned < scan_limit AND locked < requested_limit)
       OR (previous_at, previous_id) >= (upper_at, high_id) THEN
        previous_at := '-infinity';
        previous_id := 0;
        upper_at := NULL;
        high_id := NULL;
        traversal_completed := true;
    END IF;
    UPDATE public.source_observation_retention_state state
    SET cursor_received_at = previous_at, cursor_id = previous_id,
        upper_received_at = upper_at, upper_id = high_id
    WHERE singleton;
END
$function$;

COMMIT;
