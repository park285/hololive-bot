BEGIN;

CREATE OR REPLACE FUNCTION public.youtube_collector_session_instance()
RETURNS text
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
    SELECT CASE session_user
        WHEN 'hololive_collector_a' THEN 'youtube-collector-a'
        WHEN 'hololive_collector_b' THEN 'youtube-collector-b'
        WHEN 'hololive_collector_c' THEN 'youtube-collector-c'
        WHEN 'hololive_collector_d' THEN 'youtube-collector-d'
        ELSE NULL
    END
$function$;

CREATE OR REPLACE FUNCTION public.youtube_collector_session_is_privileged()
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
    SELECT session_user = 'hololive_runtime'
        OR pg_catalog.pg_has_role(
            session_user,
            pg_catalog.pg_get_userbyid(object.relowner),
            'MEMBER'
        )
    FROM pg_catalog.pg_class AS object
    WHERE object.oid = 'public.youtube_collection_job_leases'::regclass
$function$;

CREATE OR REPLACE FUNCTION public.youtube_collection_job_contract(
    requested_provider text,
    requested_job_kind text
)
RETURNS TABLE(
    job_class text,
    exact_subject boolean,
    lease_subject text,
    membership_kinds text[],
    cadence_kinds text[],
    emission_kinds text[]
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path TO 'pg_catalog'
ROWS 1
AS $function$
    SELECT contract.job_class,
           contract.exact_subject,
           contract.lease_subject,
           contract.membership_kinds,
           contract.cadence_kinds,
           contract.emission_kinds
    FROM (VALUES
        ('youtubejs', 'community_collect', 'SUBJECT', TRUE, NULL::text,
            ARRAY['community_page']::text[], ARRAY['community_page']::text[], ARRAY['community_page']::text[]),
        ('youtubejs', 'youtubejs_content', 'SUBJECT', TRUE, NULL::text,
            ARRAY['shorts_list', 'video_list']::text[], ARRAY['shorts_list', 'video_list']::text[],
            ARRAY['shorts_list', 'video_list']::text[]),
        ('youtubejs', 'youtubejs_channel_live', 'SUBJECT', TRUE, NULL::text,
            ARRAY['live_snapshot']::text[], ARRAY['live_snapshot']::text[], ARRAY['live_snapshot']::text[]),
        ('youtubejs', 'youtubejs_channel_live_check', 'SUBJECT', TRUE, NULL::text,
            ARRAY['channel_live_check']::text[], ARRAY['channel_live_check']::text[], ARRAY['channel_live_check']::text[]),
        ('youtubejs', 'youtubejs_channel_metadata', 'SUBJECT', TRUE, NULL::text,
            ARRAY['channel_photo', 'channel_profile']::text[], ARRAY['channel_photo', 'channel_profile']::text[],
            ARRAY['channel_photo', 'channel_profile']::text[]),
        ('youtubejs', 'youtubejs_video_live', 'SUBJECT', TRUE, NULL::text,
            ARRAY['video_live_check']::text[], ARRAY['video_live_check']::text[], ARRAY['video_live_check']::text[]),
        ('holodex', 'holodex_live', 'GLOBAL', FALSE, 'global:holodex_live',
            ARRAY['live_snapshot']::text[], ARRAY['live_snapshot']::text[], ARRAY['live_snapshot']::text[]),
        ('holodex', 'holodex_metadata', 'GLOBAL', FALSE, 'global:holodex_metadata',
            ARRAY['channel_photo']::text[], ARRAY['channel_photo']::text[], ARRAY['channel_photo']::text[]),
        ('holodex', 'holodex_schedule', 'GLOBAL', FALSE, 'global:holodex_schedule',
            ARRAY['live_snapshot', 'schedule_snapshot']::text[], ARRAY['schedule_snapshot']::text[],
            ARRAY['schedule_snapshot']::text[]),
        ('hololive_official', 'official_schedule', 'GLOBAL', TRUE, 'global:hololive-schedule',
            ARRAY['schedule_snapshot']::text[], ARRAY['schedule_snapshot']::text[], ARRAY['schedule_snapshot']::text[])
    ) AS contract(
        provider,
        job_kind,
        job_class,
        exact_subject,
        lease_subject,
        membership_kinds,
        cadence_kinds,
        emission_kinds
    )
    WHERE contract.provider = requested_provider
      AND contract.job_kind = requested_job_kind
$function$;

CREATE OR REPLACE FUNCTION public.enforce_youtube_collection_job_lease_identity()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    collector_instance text;
    contract record;
    now_at timestamptz := pg_catalog.clock_timestamp();
    lease_ttl interval;
    acquiring boolean := FALSE;
BEGIN
    IF public.youtube_collector_session_is_privileged() THEN
        RETURN NEW;
    END IF;

    collector_instance := public.youtube_collector_session_instance();
    IF collector_instance IS NULL THEN
        RAISE EXCEPTION 'collector database identity is not authorized'
            USING ERRCODE = '42501';
    END IF;

    IF TG_OP = 'UPDATE' AND ROW(
        NEW.job_key,
        NEW.provider,
        NEW.job_class,
        NEW.collection_job_kind,
        NEW.subject_key
    ) IS DISTINCT FROM ROW(
        OLD.job_key,
        OLD.provider,
        OLD.job_class,
        OLD.collection_job_kind,
        OLD.subject_key
    ) THEN
        RAISE EXCEPTION 'collector lease identity is immutable'
            USING ERRCODE = '42501';
    END IF;

    IF TG_OP = 'INSERT' THEN
        acquiring := TRUE;
        IF NEW.slot_state <> 'ACTIVE' OR NEW.fence_epoch <> 1 THEN
            RAISE EXCEPTION 'collector lease insert must acquire the first active fence'
                USING ERRCODE = '42501';
        END IF;

        lease_ttl := NEW.lease_expires_at - NEW.scheduled_for;
        IF lease_ttl < INTERVAL '1 second' OR lease_ttl > INTERVAL '30 minutes' THEN
            RAISE EXCEPTION 'collector lease duration is outside the authorized range'
                USING ERRCODE = '42501';
        END IF;

        NEW.scheduled_for := now_at;
        NEW.next_due_at := now_at;
        NEW.lease_expires_at := now_at + lease_ttl;
        NEW.updated_at := now_at;
    ELSIF NEW.slot_state = 'ACTIVE'
       AND NOT (
           OLD.slot_state = 'ACTIVE'
           AND OLD.owner_instance = collector_instance
           AND OLD.lease_expires_at > now_at
       ) THEN
        acquiring := TRUE;

        IF OLD.owner_instance IS NOT NULL AND OLD.lease_expires_at > now_at THEN
            RAISE EXCEPTION 'collector cannot acquire another active lease'
                USING ERRCODE = '42501';
        END IF;

        IF NEW.next_due_at IS DISTINCT FROM OLD.next_due_at THEN
            RAISE EXCEPTION 'collector lease acquisition cannot rewrite the next due time'
                USING ERRCODE = '42501';
        END IF;

        IF NOT (
            (OLD.slot_state = 'IDLE'
             AND LEAST(
                 OLD.next_due_at,
                 OLD.scheduled_for + NEW.poll_interval_ms * INTERVAL '1 millisecond'
             ) <= now_at)
            OR (OLD.slot_state = 'DEFERRED' AND OLD.retry_not_before <= now_at)
            OR (OLD.slot_state = 'ACTIVE' AND OLD.lease_expires_at <= now_at)
        ) THEN
            RAISE EXCEPTION 'collector lease is not due for acquisition'
                USING ERRCODE = '42501';
        END IF;

        IF NEW.fence_epoch <> OLD.fence_epoch + 1 THEN
            RAISE EXCEPTION 'collector lease acquisition must advance the fence'
                USING ERRCODE = '42501';
        END IF;

        lease_ttl := NEW.lease_expires_at - NEW.updated_at;
        IF lease_ttl < INTERVAL '1 second' OR lease_ttl > INTERVAL '30 minutes' THEN
            RAISE EXCEPTION 'collector lease duration is outside the authorized range'
                USING ERRCODE = '42501';
        END IF;

        IF OLD.slot_state = 'IDLE' THEN
            NEW.scheduled_for := pg_catalog.date_bin(
                NEW.poll_interval_ms * INTERVAL '1 millisecond',
                now_at,
                LEAST(
                    OLD.next_due_at,
                    OLD.scheduled_for + NEW.poll_interval_ms * INTERVAL '1 millisecond'
                )
            );
        ELSE
            NEW.scheduled_for := OLD.scheduled_for;
        END IF;

        NEW.lease_expires_at := now_at + lease_ttl;
        NEW.updated_at := now_at;
    ELSIF NEW.slot_state = 'ACTIVE' THEN
        IF OLD.owner_instance <> collector_instance
           OR NEW.fence_epoch <> OLD.fence_epoch
           OR NEW.projection_generation <> OLD.projection_generation
           OR NEW.poll_interval_ms <> OLD.poll_interval_ms
           OR NEW.next_due_at IS DISTINCT FROM OLD.next_due_at
           OR NEW.membership_kinds IS DISTINCT FROM OLD.membership_kinds
           OR NEW.membership_exact_subject IS DISTINCT FROM OLD.membership_exact_subject
           OR NEW.membership_target_count <> OLD.membership_target_count THEN
            RAISE EXCEPTION 'collector cannot rewrite an active lease proof'
                USING ERRCODE = '42501';
        END IF;

        IF OLD.updated_at >= pg_catalog.transaction_timestamp()
           AND OLD.fence_epoch = 1 THEN
            lease_ttl := NEW.lease_expires_at - NEW.updated_at;
            IF lease_ttl < INTERVAL '1 second' OR lease_ttl > INTERVAL '30 minutes' THEN
                RAISE EXCEPTION 'collector lease duration is outside the authorized range'
                    USING ERRCODE = '42501';
            END IF;

            NEW.scheduled_for := pg_catalog.date_bin(
                NEW.poll_interval_ms * INTERVAL '1 millisecond',
                now_at,
                OLD.next_due_at
            );
            NEW.lease_expires_at := now_at + lease_ttl;
            NEW.updated_at := now_at;
        ELSIF NEW.scheduled_for IS DISTINCT FROM OLD.scheduled_for
           OR NEW.lease_expires_at <= now_at
           OR NEW.lease_expires_at > now_at + INTERVAL '30 minutes' THEN
            RAISE EXCEPTION 'collector lease renewal cannot rewrite the scheduled slot'
                USING ERRCODE = '42501';
        END IF;
    ELSE
        IF OLD.slot_state <> 'ACTIVE'
           OR OLD.owner_instance <> collector_instance
           OR OLD.lease_expires_at <= now_at
           OR NEW.owner_instance IS NOT NULL
           OR NEW.fence_epoch <> OLD.fence_epoch
           OR NEW.projection_generation <> OLD.projection_generation
           OR NEW.poll_interval_ms <> OLD.poll_interval_ms
           OR NEW.scheduled_for IS DISTINCT FROM OLD.scheduled_for
           OR NEW.next_due_at > GREATEST(
               OLD.next_due_at,
               OLD.scheduled_for + OLD.poll_interval_ms * INTERVAL '1 millisecond'
           )
           OR NEW.membership_kinds IS DISTINCT FROM OLD.membership_kinds
           OR NEW.membership_exact_subject IS DISTINCT FROM OLD.membership_exact_subject
           OR NEW.membership_target_count <> OLD.membership_target_count THEN
            RAISE EXCEPTION 'collector can release only its current active lease'
                USING ERRCODE = '42501';
        END IF;

        RETURN NEW;
    END IF;

    IF NEW.owner_instance <> collector_instance THEN
        RAISE EXCEPTION 'collector lease owner does not match the database identity'
            USING ERRCODE = '42501';
    END IF;

    SELECT * INTO contract
    FROM public.youtube_collection_job_contract(NEW.provider, NEW.collection_job_kind);
    IF NOT FOUND
       OR NEW.job_class <> contract.job_class
       OR NEW.membership_exact_subject IS DISTINCT FROM contract.exact_subject
       OR NEW.membership_kinds IS DISTINCT FROM contract.membership_kinds
       OR (NEW.job_class = 'GLOBAL' AND NEW.subject_key <> contract.lease_subject)
       OR NEW.job_key <> 'collector:' || NEW.provider || ':' || NEW.collection_job_kind || ':' ||
            CASE WHEN NEW.job_class = 'GLOBAL' THEN 'global' ELSE NEW.subject_key END THEN
        RAISE EXCEPTION 'collector lease does not match a canonical job contract'
            USING ERRCODE = '42501';
    END IF;

    IF acquiring AND NOT EXISTS (
        SELECT 1
        FROM public.youtube_collection_projection_generations AS projection
        WHERE projection.generation = NEW.projection_generation
          AND projection.status = 'CURRENT'
          AND projection.valid_until > pg_catalog.statement_timestamp()
    ) THEN
        RAISE EXCEPTION 'collector lease acquisition requires the current projection'
            USING ERRCODE = '42501';
    END IF;

    IF NOT public.youtube_collection_membership_valid(
        NEW.membership_kinds,
        NEW.membership_exact_subject,
        NEW.subject_key,
        NEW.membership_target_count,
        NEW.projection_generation
    ) OR EXISTS (
        SELECT 1
        FROM public.youtube_collection_projection_generations AS projection
        JOIN public.youtube_collection_targets AS target
          ON target.projection_generation = projection.generation
        WHERE projection.status = 'CURRENT'
          AND projection.valid_until > pg_catalog.statement_timestamp()
          AND target.observation_kind = ANY (contract.cadence_kinds)
          AND target.enabled
          AND (NOT NEW.membership_exact_subject OR target.subject_key = NEW.subject_key)
          AND target.poll_interval_ms <> NEW.poll_interval_ms
    ) THEN
        RAISE EXCEPTION 'collector lease membership or cadence is not current'
            USING ERRCODE = '42501';
    END IF;

    IF acquiring AND NOT EXISTS (
        SELECT 1
        FROM public.youtube_collection_projection_generations AS projection
        JOIN public.youtube_collection_targets AS target
          ON target.projection_generation = projection.generation
        WHERE projection.status = 'CURRENT'
          AND projection.valid_until > pg_catalog.statement_timestamp()
          AND target.observation_kind = ANY (contract.cadence_kinds)
          AND target.enabled
          AND (NOT NEW.membership_exact_subject OR target.subject_key = NEW.subject_key)
          AND (target.not_before IS NULL OR target.not_before <= now_at)
    ) THEN
        RAISE EXCEPTION 'collector lease has no admissible current target'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$function$;

CREATE OR REPLACE FUNCTION public.enforce_source_observation_provenance_identity()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    collector_instance text;
BEGIN
    IF public.youtube_collector_session_is_privileged() THEN
        RETURN NEW;
    END IF;

    collector_instance := public.youtube_collector_session_instance();
    IF collector_instance IS NULL
       OR NEW.collector_instance <> collector_instance
       OR NOT EXISTS (
           SELECT 1
           FROM public.youtube_collection_job_leases AS lease
           WHERE lease.job_key = NEW.job_key
             AND lease.owner_instance = collector_instance
             AND lease.collection_job_kind = NEW.collection_job_kind
             AND lease.slot_state = 'ACTIVE'
             AND lease.lease_expires_at > pg_catalog.clock_timestamp()
       ) THEN
        RAISE EXCEPTION 'observation provenance does not match the database identity'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$function$;

CREATE OR REPLACE FUNCTION public.enforce_source_observation_payload_identity_fence()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    collector_instance text;
BEGIN
    IF public.youtube_collector_session_is_privileged() THEN
        RETURN NULL;
    END IF;

    collector_instance := public.youtube_collector_session_instance();
    IF collector_instance IS NULL OR NOT EXISTS (
        SELECT 1
        FROM public.source_observations AS observation
        JOIN public.source_observation_provenances AS provenance
          ON provenance.id = observation.provenance_id
        WHERE observation.payload_id = NEW.id
          AND provenance.collector_instance = collector_instance
    ) THEN
        RAISE EXCEPTION 'collector payload requires an authorized observation in the same transaction'
            USING ERRCODE = '42501';
    END IF;

    RETURN NULL;
END
$function$;

CREATE OR REPLACE FUNCTION public.enforce_source_observation_identity_fence()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    collector_instance text;
    provenance record;
    lease record;
    contract record;
BEGIN
    IF public.youtube_collector_session_is_privileged() THEN
        RETURN NEW;
    END IF;

    collector_instance := public.youtube_collector_session_instance();
    IF collector_instance IS NULL THEN
        RAISE EXCEPTION 'collector database identity is not authorized'
            USING ERRCODE = '42501';
    END IF;

    SELECT value.job_key, value.collector_instance, value.collection_job_kind
    INTO provenance
    FROM public.source_observation_provenances AS value
    WHERE value.id = NEW.provenance_id;
    IF NOT FOUND OR provenance.collector_instance <> collector_instance THEN
        RAISE EXCEPTION 'observation provenance does not match the database identity'
            USING ERRCODE = '42501';
    END IF;

    SELECT value.* INTO lease
    FROM public.youtube_collection_job_leases AS value
    WHERE value.job_key = provenance.job_key
      AND value.owner_instance = collector_instance
      AND value.fence_epoch = NEW.fence_epoch
      AND value.projection_generation = NEW.projection_generation
      AND value.scheduled_for = NEW.scheduled_for
      AND value.slot_state = 'ACTIVE'
      AND value.lease_expires_at > pg_catalog.clock_timestamp()
    FOR UPDATE OF value;
    IF NOT FOUND
       OR lease.provider <> NEW.provider
       OR lease.collection_job_kind <> provenance.collection_job_kind THEN
        RAISE EXCEPTION 'observation is not authorized by an active lease fence'
            USING ERRCODE = '42501';
    END IF;

    SELECT * INTO contract
    FROM public.youtube_collection_job_contract(lease.provider, lease.collection_job_kind);
    IF NOT FOUND
       OR lease.job_class <> contract.job_class
       OR lease.membership_exact_subject IS DISTINCT FROM contract.exact_subject
       OR lease.membership_kinds IS DISTINCT FROM contract.membership_kinds
       OR (lease.job_class = 'GLOBAL' AND lease.subject_key <> contract.lease_subject)
       OR lease.job_key <> 'collector:' || lease.provider || ':' || lease.collection_job_kind || ':' ||
            CASE WHEN lease.job_class = 'GLOBAL' THEN 'global' ELSE lease.subject_key END
       OR NOT (NEW.observation_kind = ANY (contract.emission_kinds))
       OR (contract.exact_subject AND NEW.subject_key <> lease.subject_key)
       OR NOT public.youtube_collection_membership_valid(
            lease.membership_kinds,
            lease.membership_exact_subject,
            lease.subject_key,
            lease.membership_target_count,
            lease.projection_generation
       ) THEN
        RAISE EXCEPTION 'observation is outside the active job contract'
            USING ERRCODE = '42501';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM public.youtube_collection_projection_generations AS projection
        JOIN public.youtube_collection_targets AS target
          ON target.projection_generation = projection.generation
        WHERE projection.status = 'CURRENT'
          AND projection.valid_until > pg_catalog.statement_timestamp()
          AND target.subject_key = NEW.subject_key
          AND target.observation_kind = NEW.observation_kind
          AND target.enabled
          AND target.member_since_generation BETWEEN 1 AND lease.projection_generation
    ) THEN
        RAISE EXCEPTION 'observation target is not enabled for the lease generation'
            USING ERRCODE = '42501';
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM public.observation_contract_generations AS current
        WHERE current.provider = NEW.provider
          AND current.observation_kind = NEW.observation_kind
          AND current.current_schema_version = NEW.schema_version
          AND current.current_generation = NEW.contract_generation
    ) THEN
        RAISE EXCEPTION 'observation contract is not current'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$function$;

CREATE OR REPLACE FUNCTION public.source_observation_authorized_for_session(
    requested_observation_id bigint,
    requested_collector_instance text
)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
    SELECT EXISTS (
        SELECT 1
        FROM public.source_observations AS observation
        JOIN public.source_observation_provenances AS provenance
          ON provenance.id = observation.provenance_id
        JOIN public.youtube_collection_job_leases AS lease
          ON lease.job_key = provenance.job_key
         AND lease.owner_instance = requested_collector_instance
         AND lease.fence_epoch = observation.fence_epoch
         AND lease.projection_generation = observation.projection_generation
         AND lease.scheduled_for = observation.scheduled_for
         AND lease.slot_state = 'ACTIVE'
         AND lease.lease_expires_at > pg_catalog.clock_timestamp()
        CROSS JOIN LATERAL public.youtube_collection_job_contract(
            lease.provider,
            lease.collection_job_kind
        ) AS contract
        WHERE observation.id = requested_observation_id
          AND provenance.collector_instance = requested_collector_instance
          AND provenance.collection_job_kind = lease.collection_job_kind
          AND observation.provider = lease.provider
          AND observation.observation_kind = ANY (contract.emission_kinds)
          AND lease.job_class = contract.job_class
          AND lease.membership_exact_subject IS NOT DISTINCT FROM contract.exact_subject
          AND lease.membership_kinds IS NOT DISTINCT FROM contract.membership_kinds
          AND (lease.job_class <> 'GLOBAL' OR lease.subject_key = contract.lease_subject)
          AND lease.job_key = 'collector:' || lease.provider || ':' || lease.collection_job_kind || ':' ||
              CASE WHEN lease.job_class = 'GLOBAL' THEN 'global' ELSE lease.subject_key END
          AND (NOT contract.exact_subject OR observation.subject_key = lease.subject_key)
          AND public.youtube_collection_membership_valid(
              lease.membership_kinds,
              lease.membership_exact_subject,
              lease.subject_key,
              lease.membership_target_count,
              lease.projection_generation
          )
          AND EXISTS (
              SELECT 1
              FROM public.youtube_collection_projection_generations AS projection
              JOIN public.youtube_collection_targets AS target
                ON target.projection_generation = projection.generation
              WHERE projection.status = 'CURRENT'
                AND projection.valid_until > pg_catalog.statement_timestamp()
                AND target.subject_key = observation.subject_key
                AND target.observation_kind = observation.observation_kind
                AND target.enabled
                AND target.member_since_generation BETWEEN 1 AND lease.projection_generation
          )
          AND EXISTS (
              SELECT 1
              FROM public.observation_contract_generations AS current
              WHERE current.provider = observation.provider
                AND current.observation_kind = observation.observation_kind
                AND current.current_schema_version = observation.schema_version
                AND current.current_generation = observation.contract_generation
          )
    )
$function$;

CREATE OR REPLACE FUNCTION public.enforce_source_observation_queue_identity_fence()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path TO 'pg_catalog'
AS $function$
DECLARE
    collector_instance text;
BEGIN
    IF public.youtube_collector_session_is_privileged() THEN
        RETURN NEW;
    END IF;

    collector_instance := public.youtube_collector_session_instance();
    IF collector_instance IS NULL THEN
        RAISE EXCEPTION 'observation queue insert is not authorized by the active lease fence'
            USING ERRCODE = '42501';
    END IF;

    PERFORM 1
    FROM public.source_observations AS observation
    JOIN public.source_observation_provenances AS provenance
      ON provenance.id = observation.provenance_id
    JOIN public.youtube_collection_job_leases AS lease
      ON lease.job_key = provenance.job_key
    WHERE observation.id = NEW.observation_id
    FOR UPDATE OF lease;
    IF NOT FOUND
       OR NOT public.source_observation_authorized_for_session(
           NEW.observation_id,
           collector_instance
       ) THEN
        RAISE EXCEPTION 'observation queue insert is not authorized by the active lease fence'
            USING ERRCODE = '42501';
    END IF;

    RETURN NEW;
END
$function$;

DROP TRIGGER IF EXISTS enforce_youtube_collection_job_lease_identity
    ON public.youtube_collection_job_leases;
CREATE TRIGGER enforce_youtube_collection_job_lease_identity
BEFORE INSERT OR UPDATE ON public.youtube_collection_job_leases
FOR EACH ROW
EXECUTE FUNCTION public.enforce_youtube_collection_job_lease_identity();

DROP TRIGGER IF EXISTS enforce_source_observation_provenance_identity
    ON public.source_observation_provenances;
CREATE TRIGGER enforce_source_observation_provenance_identity
BEFORE INSERT ON public.source_observation_provenances
FOR EACH ROW
EXECUTE FUNCTION public.enforce_source_observation_provenance_identity();

DROP TRIGGER IF EXISTS enforce_source_observation_payload_identity_fence
    ON public.source_observation_payloads;
CREATE CONSTRAINT TRIGGER enforce_source_observation_payload_identity_fence
AFTER INSERT ON public.source_observation_payloads
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION public.enforce_source_observation_payload_identity_fence();

DROP TRIGGER IF EXISTS enforce_source_observation_identity_fence
    ON public.source_observations;
CREATE TRIGGER enforce_source_observation_identity_fence
BEFORE INSERT ON public.source_observations
FOR EACH ROW
EXECUTE FUNCTION public.enforce_source_observation_identity_fence();

DROP TRIGGER IF EXISTS enforce_source_observation_queue_identity_fence
    ON public.source_observation_queue;
CREATE TRIGGER enforce_source_observation_queue_identity_fence
BEFORE INSERT ON public.source_observation_queue
FOR EACH ROW
EXECUTE FUNCTION public.enforce_source_observation_queue_identity_fence();

REVOKE ALL ON FUNCTION public.youtube_collector_session_instance() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.youtube_collector_session_is_privileged() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.youtube_collection_job_contract(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_youtube_collection_job_lease_identity() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_source_observation_provenance_identity() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_source_observation_payload_identity_fence() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_source_observation_identity_fence() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.source_observation_authorized_for_session(bigint, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.enforce_source_observation_queue_identity_fence() FROM PUBLIC;

COMMIT;
