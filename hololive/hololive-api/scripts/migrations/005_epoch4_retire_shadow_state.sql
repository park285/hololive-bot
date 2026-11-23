BEGIN;
SET LOCAL lock_timeout = '3s';
DO $precondition$
BEGIN
    IF EXISTS (
        SELECT FROM public.event_grade_regions r
        JOIN public.event_grade_consumer_offsets o ON o.region = r.region
        WHERE o.applied_seq < r.published_seq
    ) THEN
        RAISE EXCEPTION 'event grade observations are not fully consumed; resume the worker until every region is caught up before 005'
            USING ERRCODE = '55000';
    END IF;
END
$precondition$;
SELECT public.lock_youtube_collection_runtime_bundles();
DROP TRIGGER youtube_collection_runtime_insert ON public.youtube_collection_projection_generations;
DROP TRIGGER youtube_collection_runtime_insert ON public.youtube_collection_targets;
DROP TRIGGER youtube_collection_runtime_update ON public.youtube_collection_projection_generations;
DROP TRIGGER youtube_collection_runtime_update ON public.youtube_collection_targets;
DROP TRIGGER youtube_collection_runtime_delete ON public.youtube_collection_projection_generations;
DROP TRIGGER youtube_collection_runtime_delete ON public.youtube_collection_targets;
DROP TRIGGER youtube_collection_runtime_truncate ON public.youtube_collection_projection_generations;
DROP TRIGGER youtube_collection_runtime_truncate ON public.youtube_collection_targets;
DROP FUNCTION public.refresh_youtube_collection_runtime_bundles_statement();

CREATE OR REPLACE FUNCTION public.lock_youtube_collection_runtime_bundles() RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
BEGIN
    IF pg_catalog.current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'youtube collection projection writers require read committed transactions';
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('youtube_collection_projection', 0));
END
$$;

CREATE OR REPLACE FUNCTION public.lock_current_youtube_collection_projection() RETURNS TABLE(generation bigint)
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
#variable_conflict use_column
BEGIN
    IF pg_catalog.current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'youtube collection projection readers require read committed transactions';
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock_shared(pg_catalog.hashtextextended('youtube_collection_projection', 0));

    RETURN QUERY
    SELECT projection.generation
    FROM public.youtube_collection_projection_generations AS projection
    WHERE projection.status = 'CURRENT'
      AND projection.valid_until > pg_catalog.clock_timestamp();
END
$$;

DROP TABLE public.youtube_collection_projection_guard;
DROP FUNCTION public.refresh_youtube_collection_runtime_bundles(bigint[]);
DROP FUNCTION public.refresh_youtube_collection_runtime_bundle_keys(bigint[], text[], text[]);
DROP TABLE public.youtube_collection_runtime_bundles;

DELETE FROM public.event_grade_collection_leases AS l
WHERE l.kind = 'cutoff'
  AND NOT EXISTS (
      SELECT FROM public.event_grade_chapters AS c
      WHERE (c.region, c.event_id, c.chapter_id) = (l.region, l.event_id, l.chapter_id));
ALTER TABLE public.event_grade_collection_leases
    DROP CONSTRAINT event_grade_collection_leases_region_event_id_chapter_id_fkey,
    DROP CONSTRAINT event_grade_collection_leases_check,
    DROP COLUMN target_generation,
    ADD CONSTRAINT event_grade_collection_leases_check CHECK ((kind = 'catalog') = (event_id IS NULL AND chapter_id IS NULL)),
    ADD CONSTRAINT event_grade_collection_leases_chapter_fkey FOREIGN KEY (region, event_id, chapter_id)
        REFERENCES public.event_grade_chapters (region, event_id, chapter_id);
ALTER TABLE public.event_grade_observations
    DROP CONSTRAINT event_grade_observations_check1,
    ADD CONSTRAINT event_grade_observations_check1 CHECK (kind = 'cutoff' OR target_generation IS NULL);

DROP FUNCTION public.event_grade_claim_cutoffs(text, text, integer, integer);
CREATE FUNCTION public.event_grade_claim_cutoffs(p_region text, p_owner text, p_ttl_ms integer, p_limit integer)
    RETURNS TABLE(job_key text, event_id text, chapter_id text, final_after timestamptz, binding_observed_at timestamptz, fence_epoch bigint, claimed_at timestamptz)
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    v_owner text := public.event_grade_collector_owner(p_owner);
    v_catalog_observed timestamptz;
    v_now timestamptz := pg_catalog.statement_timestamp();
BEGIN
    IF p_ttl_ms NOT BETWEEN 1000 AND 300000 OR p_limit NOT BETWEEN 1 AND 3 THEN
        RAISE EXCEPTION 'event grade cutoff claim is outside bounds' USING ERRCODE = '22023';
    END IF;
    SELECT r.catalog_observed_at INTO v_catalog_observed FROM public.event_grade_regions r WHERE r.region = p_region FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'unknown event grade region' USING ERRCODE = '22023';
    END IF;
    INSERT INTO public.event_grade_collection_leases AS l (job_key, region, kind, event_id, chapter_id)
    SELECT 'cutoff:' || c.region || ':' || c.event_id || ':' || c.chapter_id, c.region, 'cutoff', c.event_id, c.chapter_id
    FROM public.event_grade_chapters c
    WHERE c.region = p_region AND NOT c.binding_conflicted
      AND ((c.starts_at <= v_now AND c.ends_at > v_now)
           OR v_now BETWEEN greatest(c.event_ends_at, c.reveals_at) AND greatest(c.event_ends_at, c.reveals_at) + interval '24 hours')
    ON CONFLICT ON CONSTRAINT event_grade_collection_leases_pkey DO NOTHING;
    RETURN QUERY
    WITH candidates AS (
        SELECT l.job_key, greatest(c.event_ends_at, c.reveals_at) AS final_after
        FROM public.event_grade_chapters c
        JOIN public.event_grade_collection_leases l
          ON (l.region, l.event_id, l.chapter_id) = (c.region, c.event_id, c.chapter_id)
        WHERE c.region = p_region AND NOT c.binding_conflicted
          AND (l.owner_instance IS NULL OR l.lease_expires_at <= v_now)
          AND (
              (c.starts_at <= v_now AND c.ends_at > v_now AND v_catalog_observed >= v_now - interval '2 minutes')
              OR (v_now BETWEEN greatest(c.event_ends_at, c.reveals_at) AND greatest(c.event_ends_at, c.reveals_at) + interval '24 hours'
                  AND NOT EXISTS (
                      SELECT FROM public.event_grade_borders b
                      WHERE (b.region, b.event_id, b.chapter_id) = (c.region, c.event_id, c.chapter_id)
                        AND b.board_kind = 'marathon_score' AND b.is_final)
                  AND NOT EXISTS (
                      SELECT FROM public.event_grade_observations o
                      JOIN public.event_grade_consumer_offsets co ON co.region = o.region
                      WHERE o.job_key = l.job_key AND o.kind = 'cutoff' AND o.seq > co.applied_seq
                        AND o.payload -> 'final' = 'true'::jsonb))
          )
        ORDER BY l.last_completed_at NULLS FIRST, final_after, c.event_id, c.chapter_id
        LIMIT p_limit
        FOR UPDATE OF l
    )
    UPDATE public.event_grade_collection_leases AS l
    SET fence_epoch = l.fence_epoch + 1, owner_instance = v_owner, claimed_at = v_now,
        lease_expires_at = v_now + p_ttl_ms * interval '1 millisecond'
    FROM candidates
    WHERE l.job_key = candidates.job_key
    RETURNING l.job_key, l.event_id::text, l.chapter_id::text, candidates.final_after, v_catalog_observed, l.fence_epoch, l.claimed_at;
END
$$;

CREATE OR REPLACE FUNCTION public.event_grade_publish(p_job_key text, p_owner text, p_epoch bigint, p_started_at timestamptz, p_observed_at timestamptz, p_payload jsonb)
    RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    v_owner text := public.event_grade_collector_owner(p_owner);
    lease record;
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
    SELECT l.job_key, l.region, l.kind, l.event_id, l.chapter_id, l.fence_epoch, l.owner_instance,
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
       OR p_started_at > p_observed_at OR p_observed_at > now_at + interval '2 seconds' THEN
        RAISE EXCEPTION 'event grade observation timing is invalid' USING ERRCODE = '22023';
    END IF;

    IF lease.kind = 'cutoff' THEN
        IF p_payload->>'event_id' IS DISTINCT FROM lease.event_id OR p_payload->>'chapter_id' IS DISTINCT FROM lease.chapter_id THEN
            RAISE EXCEPTION 'event grade cutoff payload does not match the claimed target' USING ERRCODE = '22023';
        END IF;
        IF NOT EXISTS (
            SELECT FROM public.event_grade_chapters c
            WHERE (c.region, c.event_id, c.chapter_id) = (lease.region, lease.event_id, lease.chapter_id)
              AND NOT c.binding_conflicted
              AND ((c.starts_at <= lease.claimed_at AND c.ends_at > lease.claimed_at)
                   OR lease.claimed_at BETWEEN greatest(c.event_ends_at, c.reveals_at)
                                           AND greatest(c.event_ends_at, c.reveals_at) + interval '24 hours')
        ) THEN
            RAISE EXCEPTION 'event grade cutoff target left its collection window after the claim' USING ERRCODE = '22023';
        END IF;
    ELSE
        IF jsonb_typeof(p_payload->'quarantine_missing') IS DISTINCT FROM 'boolean' THEN
            RAISE EXCEPTION 'event grade catalog payload is invalid' USING ERRCODE = '22023';
        END IF;
        body := p_payload || jsonb_build_object('quarantine_missing', (p_payload->>'quarantine_missing')::boolean OR lease.catalog_interrupted);
        UPDATE public.event_grade_regions r
        SET catalog_observed_at = greatest(r.catalog_observed_at, p_observed_at), catalog_attempt_started_at = NULL
        WHERE r.region = lease.region;
    END IF;

    UPDATE public.event_grade_regions r SET published_seq = r.published_seq + 1
    WHERE r.region = lease.region RETURNING r.published_seq INTO next_seq;
    INSERT INTO public.event_grade_observations(region, seq, schema_version, kind, job_key, scheduled_for, owner_instance,
        fence_epoch, started_at, observed_at, published_at, payload)
    VALUES (lease.region, next_seq, 1, lease.kind, lease.job_key, lease.claimed_at, v_owner, lease.fence_epoch,
        p_started_at, p_observed_at, now_at, body);
    UPDATE public.event_grade_collection_leases l
    SET owner_instance = NULL, claimed_at = NULL, lease_expires_at = NULL, catalog_interrupted = false, last_completed_at = now_at
    WHERE l.job_key = lease.job_key;
    RETURN next_seq;
END
$$;

DROP FUNCTION public.event_grade_apply_catalog_targets(text, jsonb);
DROP TABLE public.event_grade_collection_targets;
REVOKE ALL ON FUNCTION public.event_grade_claim_cutoffs(text, text, integer, integer) FROM PUBLIC;

DO $acl$
BEGIN
    IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'hololive_collector_c') THEN
        GRANT EXECUTE ON FUNCTION public.event_grade_claim_cutoffs(text, text, integer, integer) TO hololive_collector_c;
    END IF;
END
$acl$;
ALTER TABLE public.youtube_live_review_receipt_facts DROP COLUMN semantic_value;
DROP FUNCTION public.youtube_live_review_receipt_value(jsonb);

CREATE FUNCTION public.youtube_live_review_source_facts(p_session_video_id text, p_session_channel_id text, p_session_status text, p_session_lifecycle_origin text, p_session_title text, p_session_topic_id text, p_session_is_premiere boolean, p_session_scheduled_start_time timestamp with time zone, p_session_started_at timestamp with time zone, p_session_ended_at timestamp with time zone, p_session_live_first_seen_at timestamp with time zone, p_head_video_id text, p_head_status text, p_head_last_upcoming_positive_at timestamp with time zone, p_head_last_upcoming_positive_seen_at timestamp with time zone, p_head_last_live_positive_at timestamp with time zone, p_head_last_live_positive_seen_at timestamp with time zone, p_head_last_end_evidence_at timestamp with time zone, p_head_last_complete_absence_at timestamp with time zone, p_head_last_absence_scheduled_for timestamp with time zone, p_head_first_absence_scheduled_for timestamp with time zone, p_head_second_absence_scheduled_for timestamp with time zone, p_head_consecutive_absence_slots smallint, p_head_last_absence_observation_id bigint, p_head_end_candidate_kind text, p_head_end_candidate_observation_id bigint, p_head_ended_at timestamp with time zone, p_head_end_reason text, p_pending_video_id text, p_pending_channel_id text, p_pending_kind text, p_pending_observation_id bigint, p_pending_effective_at timestamp with time zone, p_pending_scheduled_for timestamp with time zone, p_pending_ended_at timestamp with time zone, p_pending_negative_eligible boolean, p_pending_scope_covers boolean, p_availability_availability text) RETURNS jsonb
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
SELECT jsonb_build_object(
    'session', jsonb_build_object(
        'video_id', p_session_video_id,
        'channel_id', p_session_channel_id,
        'status', p_session_status,
        'lifecycle_origin', p_session_lifecycle_origin,
        'title', p_session_title,
        'topic_id', p_session_topic_id,
        'is_premiere', p_session_is_premiere,
        'scheduled_start_time', extract(epoch FROM p_session_scheduled_start_time),
        'started_at', extract(epoch FROM p_session_started_at),
        'ended_at', extract(epoch FROM p_session_ended_at),
        'live_first_seen_at', extract(epoch FROM p_session_live_first_seen_at)),
    'head', CASE WHEN p_head_video_id IS NOT NULL THEN jsonb_build_object(
        'status', p_head_status,
        'last_upcoming_positive_at', extract(epoch FROM p_head_last_upcoming_positive_at),
        'last_upcoming_positive_seen_at', extract(epoch FROM p_head_last_upcoming_positive_seen_at),
        'last_live_positive_at', extract(epoch FROM p_head_last_live_positive_at),
        'last_live_positive_seen_at', extract(epoch FROM p_head_last_live_positive_seen_at),
        'last_end_evidence_at', extract(epoch FROM p_head_last_end_evidence_at),
        'last_complete_absence_at', extract(epoch FROM p_head_last_complete_absence_at),
        'last_absence_scheduled_for', extract(epoch FROM p_head_last_absence_scheduled_for),
        'first_absence_scheduled_for', extract(epoch FROM p_head_first_absence_scheduled_for),
        'second_absence_scheduled_for', extract(epoch FROM p_head_second_absence_scheduled_for),
        'consecutive_absence_slots', p_head_consecutive_absence_slots,
        'last_absence_observation_id', p_head_last_absence_observation_id,
        'end_candidate_kind', p_head_end_candidate_kind,
        'end_candidate_observation_id', p_head_end_candidate_observation_id,
        'ended_at', extract(epoch FROM p_head_ended_at),
        'end_reason', p_head_end_reason) END,
    'pending', CASE WHEN p_pending_video_id IS NOT NULL THEN jsonb_build_object(
        'channel_id', p_pending_channel_id,
        'kind', p_pending_kind,
        'observation_id', p_pending_observation_id,
        'effective_at', extract(epoch FROM p_pending_effective_at),
        'scheduled_for', extract(epoch FROM p_pending_scheduled_for),
        'ended_at', extract(epoch FROM p_pending_ended_at),
        'negative_eligible', p_pending_negative_eligible,
        'scope_covers', p_pending_scope_covers) END,
    'availability', p_availability_availability);
$$;
REVOKE ALL ON FUNCTION public.youtube_live_review_source_facts(text,text,text,text,text,text,boolean,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,text,text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,smallint,bigint,text,bigint,timestamp with time zone,text,text,text,text,bigint,timestamp with time zone,timestamp with time zone,timestamp with time zone,boolean,boolean,text) FROM PUBLIC;

DO $acl$
DECLARE
    grantee text;
BEGIN
    FOR grantee IN
        SELECT DISTINCT role.rolname
        FROM pg_catalog.pg_proc procedure
        CROSS JOIN LATERAL pg_catalog.aclexplode(procedure.proacl) privilege
        JOIN pg_catalog.pg_roles role ON role.oid = privilege.grantee
        WHERE procedure.oid = 'public.youtube_live_review_source_value(text,text,text,text,text,text,boolean,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,text,text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,smallint,bigint,text,bigint,timestamp with time zone,text,text,text,text,bigint,timestamp with time zone,timestamp with time zone,timestamp with time zone,boolean,boolean,text)'::regprocedure
          AND privilege.privilege_type = 'EXECUTE' AND privilege.grantee <> procedure.proowner
    LOOP
        EXECUTE format('GRANT EXECUTE ON FUNCTION public.youtube_live_review_source_facts(text,text,text,text,text,text,boolean,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,text,text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,smallint,bigint,text,bigint,timestamp with time zone,text,text,text,text,bigint,timestamp with time zone,timestamp with time zone,timestamp with time zone,boolean,boolean,text) TO %I', grantee);
    END LOOP;
END
$acl$;

CREATE OR REPLACE FUNCTION public.refresh_youtube_live_runtime_state(p_video_ids text[]) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
BEGIN
    IF pg_catalog.current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'live runtime refresh requires read committed transaction';
    END IF;
    IF p_video_ids IS NULL OR pg_catalog.cardinality(p_video_ids) = 0 THEN
        RETURN;
    END IF;
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtext('youtube_live_runtime_video'), ordered.bucket)
    FROM (SELECT DISTINCT (pg_catalog.hashtextextended(input.video_id, 0) & 1023)::integer AS bucket
          FROM pg_catalog.unnest(p_video_ids) AS input(video_id)
          WHERE input.video_id IS NOT NULL
          ORDER BY 1) AS ordered;

    DELETE FROM public.youtube_live_runtime_state state
    WHERE state.video_id=ANY(p_video_ids)
      AND NOT EXISTS (SELECT 1 FROM public.youtube_live_sessions session
          WHERE session.video_id=state.video_id AND session.status IN ('LIVE','UPCOMING'))
      AND NOT EXISTS (SELECT 1 FROM public.youtube_live_reconciliation_heads head
          WHERE head.video_id=state.video_id AND head.status IN ('LIVE','UPCOMING'));

    INSERT INTO public.youtube_live_runtime_state AS state
        (video_id,channel_id,session_status,head_status,scheduled_start_time,lifecycle_origin,
         last_live_positive_at,last_live_positive_seen_at,last_upcoming_positive_at,last_upcoming_positive_seen_at,
         availability_at,availability_seen_at,unresolvable_since,reviewed_at)
    SELECT ids.video_id,session.channel_id,session.status,head.status,session.scheduled_start_time,session.lifecycle_origin,
           head.last_live_positive_at,head.last_live_positive_seen_at,head.last_upcoming_positive_at,head.last_upcoming_positive_seen_at,
           availability.effective_at,availability.observed_at,head.unresolvable_since,review.reviewed_at
    FROM (SELECT DISTINCT unnest(p_video_ids) AS video_id) ids
    LEFT JOIN public.youtube_live_sessions session USING (video_id)
    LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)
    LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)
    LEFT JOIN public.youtube_video_availability availability USING (video_id)
    LEFT JOIN LATERAL (
        SELECT max(receipt.recorded_at) AS reviewed_at
        FROM public.youtube_live_review_receipt_facts receipt
        WHERE receipt.video_id=ids.video_id
          AND session.status='UPCOMING'
          AND (head.video_id IS NULL OR head.status=session.status)
          AND (session.lifecycle_origin<>'observed' OR head.video_id IS NOT NULL)
          AND availability.video_id IS NOT NULL
          AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)>=availability.effective_at,false)
          AND receipt.facts=public.youtube_live_review_source_facts(
              session.video_id,session.channel_id,session.status,session.lifecycle_origin,
              session.title,session.topic_id,session.is_premiere,session.scheduled_start_time,
              session.started_at,session.ended_at,session.live_first_seen_at,head.video_id,
              head.status,head.last_upcoming_positive_at,head.last_upcoming_positive_seen_at,head.last_live_positive_at,
              head.last_live_positive_seen_at,head.last_end_evidence_at,head.last_complete_absence_at,head.last_absence_scheduled_for,
              head.first_absence_scheduled_for,head.second_absence_scheduled_for,head.consecutive_absence_slots,head.last_absence_observation_id,
              head.end_candidate_kind,head.end_candidate_observation_id,head.ended_at,head.end_reason,
              pending.video_id,pending.channel_id,pending.kind,pending.observation_id,
              pending.effective_at,pending.scheduled_for,pending.ended_at,pending.negative_eligible,
              pending.scope_covers,availability.availability)
    ) review ON true
    WHERE session.status IN ('LIVE','UPCOMING') OR head.status IN ('LIVE','UPCOMING')
    ORDER BY ids.video_id COLLATE "C"
    ON CONFLICT (video_id) DO UPDATE SET
        channel_id=EXCLUDED.channel_id,session_status=EXCLUDED.session_status,head_status=EXCLUDED.head_status,
        scheduled_start_time=EXCLUDED.scheduled_start_time,lifecycle_origin=EXCLUDED.lifecycle_origin,
        last_live_positive_at=EXCLUDED.last_live_positive_at,last_live_positive_seen_at=EXCLUDED.last_live_positive_seen_at,
        last_upcoming_positive_at=EXCLUDED.last_upcoming_positive_at,last_upcoming_positive_seen_at=EXCLUDED.last_upcoming_positive_seen_at,
        availability_at=EXCLUDED.availability_at,availability_seen_at=EXCLUDED.availability_seen_at,
        unresolvable_since=EXCLUDED.unresolvable_since,reviewed_at=EXCLUDED.reviewed_at
    WHERE state IS DISTINCT FROM EXCLUDED;
END
$$;

CREATE OR REPLACE FUNCTION public.youtube_live_review_current_facts(p_video_ids text[]) RETURNS TABLE(video_id text, facts jsonb, reviewable boolean)
    LANGUAGE sql STABLE
    AS $$
SELECT session.video_id,
       public.youtube_live_review_source_facts(
           session.video_id,session.channel_id,session.status,session.lifecycle_origin,
           session.title,session.topic_id,session.is_premiere,session.scheduled_start_time,
           session.started_at,session.ended_at,session.live_first_seen_at,head.video_id,
           head.status,head.last_upcoming_positive_at,head.last_upcoming_positive_seen_at,head.last_live_positive_at,
           head.last_live_positive_seen_at,head.last_end_evidence_at,head.last_complete_absence_at,head.last_absence_scheduled_for,
           head.first_absence_scheduled_for,head.second_absence_scheduled_for,head.consecutive_absence_slots,head.last_absence_observation_id,
           head.end_candidate_kind,head.end_candidate_observation_id,head.ended_at,head.end_reason,
           pending.video_id,pending.channel_id,pending.kind,pending.observation_id,
           pending.effective_at,pending.scheduled_for,pending.ended_at,pending.negative_eligible,
           pending.scope_covers,availability.availability),
       session.status = 'UPCOMING'
           AND (head.video_id IS NULL OR head.status = session.status)
           AND (session.lifecycle_origin <> 'observed' OR head.video_id IS NOT NULL)
           AND availability.video_id IS NOT NULL
           AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)
               >= availability.effective_at,false)
FROM public.youtube_live_sessions session
LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)
LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)
LEFT JOIN public.youtube_video_availability availability USING (video_id)
WHERE session.video_id = ANY(p_video_ids);
$$;

DROP FUNCTION public.youtube_live_review_source_value(text,text,text,text,text,text,boolean,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,text,text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,smallint,bigint,text,bigint,timestamp with time zone,text,text,text,text,bigint,timestamp with time zone,timestamp with time zone,timestamp with time zone,boolean,boolean,text);
DROP TYPE public.youtube_live_review_value;
DROP TYPE public.youtube_live_review_session_value;
DROP TYPE public.youtube_live_review_head_value;
DROP TYPE public.youtube_live_review_pending_value;
COMMIT;
