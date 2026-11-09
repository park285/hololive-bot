-- epoch-3 정본: epoch-2 372 head의 스키마·시드·시퀀스 정책을 고정합니다.
-- 기존 DB에는 실행하지 않으며, 완전한 epoch-2 원장만 러너가 원자 전환합니다.
BEGIN;
SET LOCAL check_function_bodies = false;
--
-- Name: alarm_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.alarm_type AS ENUM (
    'LIVE',
    'COMMUNITY',
    'SHORTS',
    'BIRTHDAY',
    'ANNIVERSARY'
);


--
-- Name: youtube_live_review_head_value; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.youtube_live_review_head_value AS (
	status text,
	last_upcoming_positive_at numeric,
	last_upcoming_positive_seen_at numeric,
	last_live_positive_at numeric,
	last_live_positive_seen_at numeric,
	last_end_evidence_at numeric,
	last_complete_absence_at numeric,
	last_absence_scheduled_for numeric,
	first_absence_scheduled_for numeric,
	second_absence_scheduled_for numeric,
	consecutive_absence_slots numeric,
	last_absence_observation_id numeric,
	end_candidate_kind text,
	end_candidate_observation_id numeric,
	ended_at numeric,
	end_reason text
);


--
-- Name: youtube_live_review_pending_value; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.youtube_live_review_pending_value AS (
	channel_id text,
	kind text,
	observation_id numeric,
	effective_at numeric,
	scheduled_for numeric,
	ended_at numeric,
	negative_eligible boolean,
	scope_covers boolean
);


--
-- Name: youtube_live_review_session_value; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.youtube_live_review_session_value AS (
	video_id text,
	channel_id text,
	status text,
	lifecycle_origin text,
	title text,
	topic_id text,
	is_premiere boolean,
	scheduled_start_time numeric,
	started_at numeric,
	ended_at numeric,
	live_first_seen_at numeric
);


--
-- Name: youtube_live_review_value; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.youtube_live_review_value AS (
	session public.youtube_live_review_session_value,
	head public.youtube_live_review_head_value,
	pending public.youtube_live_review_pending_value,
	availability text
);


--
-- Name: alarm_dispatch_closeout_snapshot(bigint); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.alarm_dispatch_closeout_snapshot(p_delivery_id bigint) RETURNS TABLE(send_unit_id bigint, target_ids bigint[], target_revisions jsonb, status_metadata jsonb, original_sha256 text, member_count bigint)
    LANGUAGE sql STABLE
    AS $_$
    WITH target AS (
        SELECT d.send_unit_id FROM public.alarm_dispatch_deliveries d WHERE d.id = p_delivery_id
    ), members AS (
        SELECT d.id, d.updated_at, d.status, d.attempt_count, d.last_error_code,
               d.quarantined_at, d.sent_at, d.cancelled_at,
               to_jsonb(d) AS delivery_facts, to_jsonb(e) AS event_facts,
               to_jsonb(u) AS unit_facts
        FROM public.alarm_dispatch_deliveries d
        JOIN target t ON d.send_unit_id = t.send_unit_id
        JOIN public.alarm_dispatch_events e ON e.id = d.event_id
        JOIN public.alarm_dispatch_send_units u ON u.id = d.send_unit_id
        ORDER BY d.id LIMIT 101
    ), aggregate AS (
        SELECT count(id) AS member_count,
               array_agg(id ORDER BY id) AS target_ids,
               jsonb_agg(jsonb_build_object('id', id::text, 'updatedAt', updated_at) ORDER BY id) AS target_revisions,
               jsonb_agg(jsonb_build_object(
                   'id', id::text, 'status', status, 'attemptCount', attempt_count,
                   'lastErrorCode', CASE
                       WHEN last_error_code ~ '^[A-Za-z0-9_.:-]{1,128}$' THEN last_error_code
                       WHEN last_error_code = '' THEN '' ELSE 'unclassified' END,
                   'updatedAt', updated_at,
                   'quarantinedAt', quarantined_at, 'sentAt', sent_at, 'cancelledAt', cancelled_at
               ) ORDER BY id) AS status_metadata,
               jsonb_agg(jsonb_build_object(
                   'delivery', delivery_facts,
                   'event', event_facts, 'sendUnit', unit_facts
               ) ORDER BY id) AS original_facts
        FROM members m
    )
    SELECT (SELECT t.send_unit_id FROM target t), a.target_ids, a.target_revisions,
           a.status_metadata, encode(sha256(convert_to(a.original_facts::text, 'UTF8')), 'hex'),
           a.member_count
    FROM aggregate a
    WHERE a.member_count > 0
$_$;


--
-- Name: append_bot_reply_outbox_replay_claim_audit(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.append_bot_reply_outbox_replay_claim_audit() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    granted_actor TEXT;
    granted_reason TEXT;
BEGIN
    IF NEW.status = 'submitting'
        AND OLD.status <> 'submitting'
        AND NEW.operator_replay_grants > 0
    THEN
        SELECT actor, reason
        INTO granted_actor, granted_reason
        FROM public.bot_reply_outbox_replay_audit
        WHERE outbox_id = NEW.id
          AND grant_number = NEW.operator_replay_grants
          AND event_type = 'granted';

        IF NOT FOUND THEN
            RAISE EXCEPTION 'manual replay grant audit is missing for outbox %, grant %',
                NEW.id, NEW.operator_replay_grants
                USING ERRCODE = '23514';
        END IF;

        INSERT INTO public.bot_reply_outbox_replay_audit (
            outbox_id, grant_number, event_type, actor, reason
        ) VALUES (
            NEW.id, NEW.operator_replay_grants, 'replayed', granted_actor, granted_reason
        )
        ON CONFLICT (outbox_id, grant_number, event_type) DO NOTHING;
    END IF;

    RETURN NEW;
END
$$;


--
-- Name: assert_source_observation_payload_count(bigint, bigint); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.assert_source_observation_payload_count(actual bigint, expected bigint) RETURNS bigint
    LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
    IF actual <> expected THEN
        RAISE EXCEPTION 'source observation payload resolution incomplete: % of %', actual, expected;
    END IF;
    RETURN actual;
END
$$;


--
-- Name: assert_source_observation_payload_match(bigint, jsonb, jsonb); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.assert_source_observation_payload_match(requested_id bigint, stored_payload jsonb, expected_payload jsonb) RETURNS bigint
    LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
    IF stored_payload IS DISTINCT FROM expected_payload THEN
        RAISE EXCEPTION 'source observation payload digest collision or content mismatch';
    END IF;
    RETURN requested_id;
END
$$;


--
-- Name: delete_retired_youtube_collection_job_leases(timestamp with time zone, integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_retired_youtube_collection_job_leases(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_job_key text)
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
    WITH candidate AS (
        SELECT lease.job_key
        FROM public.youtube_collection_job_leases AS lease
        JOIN public.youtube_collection_projection_generations AS generation
          ON generation.generation = lease.projection_generation
        WHERE generation.status = 'RETIRED'
          AND generation.valid_until < requested_cutoff
          AND (
              lease.slot_state <> 'ACTIVE'
              OR lease.lease_expires_at < clock_timestamp()
          )
        ORDER BY generation.generation, lease.job_key
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF lease SKIP LOCKED
    )
    DELETE FROM public.youtube_collection_job_leases AS lease
    USING candidate
    WHERE lease.job_key = candidate.job_key
    RETURNING lease.job_key
$$;


--
-- Name: delete_retired_youtube_projection_batch(timestamp with time zone, integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_retired_youtube_projection_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_reasons bigint, deleted_targets bigint, deleted_generations bigint)
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    chosen_generation BIGINT;
    locked_target_ctids TID[];
BEGIN
    deleted_reasons := 0;
    deleted_targets := 0;
    deleted_generations := 0;
    IF requested_cutoff IS NULL OR requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000 THEN
        RAISE EXCEPTION 'invalid projection retention request' USING ERRCODE = '22023';
    END IF;

    PERFORM public.lock_youtube_collection_runtime_bundles();

    -- generation의 UPDATE 잠금은 새 lease의 FK KEY SHARE와 충돌합니다.
    -- 잠금을 얻는 동안 commit된 lease가 있으면 새 문장의 snapshot으로 다시 확인하여
    -- 아직 참조되는 generation의 하위 데이터를 지우지 않습니다.
    SELECT generation.generation INTO chosen_generation
    FROM public.youtube_collection_projection_generations AS generation
    WHERE generation.status = 'RETIRED'
      AND generation.valid_until < requested_cutoff
      AND NOT EXISTS (
          SELECT 1 FROM public.youtube_collection_job_leases AS lease
          WHERE lease.projection_generation = generation.generation
      )
    ORDER BY generation.valid_until, generation.generation
    LIMIT 1
    FOR UPDATE OF generation SKIP LOCKED;

    IF chosen_generation IS NULL OR EXISTS (
        SELECT 1 FROM public.youtube_collection_job_leases AS lease
        WHERE lease.projection_generation = chosen_generation
    ) THEN
        RETURN NEXT;
        RETURN;
    END IF;

    WITH candidates AS (
        SELECT reason.projection_generation, reason.subject_key,
               reason.observation_kind, reason.reason_kind, reason.reason_key
        FROM public.youtube_collection_target_reasons AS reason
        WHERE reason.projection_generation = chosen_generation
        ORDER BY reason.subject_key, reason.observation_kind,
                 reason.reason_kind, reason.reason_key
        LIMIT requested_limit
        FOR UPDATE OF reason SKIP LOCKED
    )
    DELETE FROM public.youtube_collection_target_reasons AS reason
    USING candidates AS candidate
    WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind,
           reason.reason_kind, reason.reason_key) =
          (candidate.projection_generation, candidate.subject_key, candidate.observation_kind,
           candidate.reason_kind, candidate.reason_key);
    GET DIAGNOSTICS deleted_reasons = ROW_COUNT;

    -- target을 먼저 제한된 수만큼 잠급니다. reason INSERT의 FK KEY SHARE와 충돌하므로,
    -- 잠금을 기다리는 동안 추가된 reason까지 새 문장의 snapshot으로 확인할 수 있습니다.
    -- 자식이 없는 target만 지워 FK cascade가 배치 상한을 우회하지 않게 합니다.
    IF deleted_reasons < requested_limit THEN
        SELECT pg_catalog.array_agg(candidate.ctid) INTO locked_target_ctids
        FROM (
            SELECT target.ctid
            FROM public.youtube_collection_targets AS target
            WHERE target.projection_generation = chosen_generation
              AND NOT EXISTS (
                  SELECT 1 FROM public.youtube_collection_target_reasons AS reason
                  WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind) =
                        (target.projection_generation, target.subject_key, target.observation_kind)
              )
            ORDER BY target.subject_key, target.observation_kind
            LIMIT requested_limit - deleted_reasons
            FOR UPDATE OF target SKIP LOCKED
        ) AS candidate;

        DELETE FROM public.youtube_collection_targets AS target
        WHERE target.projection_generation = chosen_generation
          AND target.ctid = ANY(locked_target_ctids)
          AND NOT EXISTS (
              SELECT 1 FROM public.youtube_collection_target_reasons AS reason
              WHERE (reason.projection_generation, reason.subject_key, reason.observation_kind) =
                    (target.projection_generation, target.subject_key, target.observation_kind)
          );
        GET DIAGNOSTICS deleted_targets = ROW_COUNT;
    END IF;

    DELETE FROM public.youtube_collection_projection_generations AS generation
    WHERE generation.generation = chosen_generation
      AND generation.status = 'RETIRED'
      AND generation.valid_until < requested_cutoff
      AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_job_leases AS lease
                      WHERE lease.projection_generation = generation.generation)
      AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_targets AS target
                      WHERE target.projection_generation = generation.generation);
    GET DIAGNOSTICS deleted_generations = ROW_COUNT;
    RETURN NEXT;
END
$$;


--
-- Name: delete_source_collection_checkpoint_retention_batch(timestamp with time zone, integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_source_collection_checkpoint_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_scope_sha256 text)
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
    WITH candidates AS (
        SELECT checkpoint.provider,
               checkpoint.observation_kind,
               checkpoint.subject_key,
               checkpoint.scope_sha256
        FROM public.source_collection_checkpoints AS checkpoint
        WHERE checkpoint.updated_at < requested_cutoff
          AND EXISTS (
              SELECT 1
              FROM public.source_collection_checkpoints AS newer
              WHERE newer.provider = checkpoint.provider
                AND newer.observation_kind = checkpoint.observation_kind
                AND newer.subject_key = checkpoint.subject_key
                AND (newer.updated_at, newer.scope_sha256) >
                    (checkpoint.updated_at, checkpoint.scope_sha256)
          )
        ORDER BY checkpoint.updated_at,
                 checkpoint.provider,
                 checkpoint.observation_kind,
                 checkpoint.subject_key,
                 checkpoint.scope_sha256
        LIMIT CASE
            WHEN requested_limit BETWEEN 1 AND 1000 THEN requested_limit
            ELSE 0
        END
        FOR UPDATE OF checkpoint SKIP LOCKED
    )
    DELETE FROM public.source_collection_checkpoints AS checkpoint
    USING candidates AS candidate
    WHERE checkpoint.provider = candidate.provider
      AND checkpoint.observation_kind = candidate.observation_kind
      AND checkpoint.subject_key = candidate.subject_key
      AND checkpoint.scope_sha256 = candidate.scope_sha256
      AND checkpoint.updated_at < requested_cutoff
      AND EXISTS (
          SELECT 1
          FROM public.source_collection_checkpoints AS newer
          WHERE newer.provider = checkpoint.provider
            AND newer.observation_kind = checkpoint.observation_kind
            AND newer.subject_key = checkpoint.subject_key
            AND (newer.updated_at, newer.scope_sha256) >
                (checkpoint.updated_at, checkpoint.scope_sha256)
      )
    RETURNING checkpoint.scope_sha256
$$;


--
-- Name: delete_source_observation_payload_batch(timestamp with time zone, integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_source_observation_payload_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    previous_id bigint;
    candidate_ids bigint[];
    deleted bigint;
BEGIN
    IF requested_cutoff IS NULL OR requested_limit IS NULL OR requested_limit NOT BETWEEN 1 AND 1000 THEN
        RAISE EXCEPTION 'invalid payload retention request' USING ERRCODE = '22023';
    END IF;
    SELECT cursor_id INTO previous_id FROM public.source_observation_payload_gc_state
    WHERE singleton FOR UPDATE;
    SELECT array_agg(candidate.id ORDER BY candidate.id) INTO candidate_ids
    FROM (
        SELECT payload.id FROM public.source_observation_payloads payload
        WHERE payload.id > previous_id AND payload.created_at < requested_cutoff
        ORDER BY payload.id LIMIT requested_limit FOR UPDATE SKIP LOCKED
    ) candidate;
    DELETE FROM public.source_observation_payloads payload
    WHERE payload.id = ANY(candidate_ids)
      AND NOT EXISTS (SELECT 1 FROM public.source_observations observation WHERE observation.payload_id = payload.id);
    GET DIAGNOSTICS deleted = ROW_COUNT;
    UPDATE public.source_observation_payload_gc_state
    SET cursor_id = coalesce(candidate_ids[array_length(candidate_ids, 1)], 0)
    WHERE singleton;
    RETURN deleted;
END
$$;


--
-- Name: delete_source_observation_provenance_batch(integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_source_observation_provenance_batch(requested_limit integer) RETURNS bigint
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
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
$$;


--
-- Name: delete_source_observation_retention_batch(text[], timestamp with time zone[], integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_source_observation_retention_batch(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer) RETURNS TABLE(deleted_id bigint)
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    SET jit TO 'off'
    AS $$
BEGIN
    RETURN QUERY SELECT unnest(retention_window.deleted_ids)
    FROM public.delete_source_observation_retention_window(requested_kinds, requested_cutoffs, requested_limit) retention_window;
END
$$;


--
-- Name: delete_source_observation_retention_window(text[], timestamp with time zone[], integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_source_observation_retention_window(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer, OUT deleted_ids bigint[], OUT passed integer, OUT traversal_completed boolean) RETURNS record
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    SET jit TO 'off'
    AS $$
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

    -- 직접 SQL 호출도 live guard → 커서 → 원본 순서를 지킨다. 다음 문장이 대기 후 새 snapshot을 읽는다.
    PERFORM public.lock_youtube_live_runtime_state();
    SELECT state.cursor_received_at, state.cursor_id, state.upper_received_at, state.upper_id
    INTO STRICT previous_at, previous_id, upper_at, high_id
    FROM public.source_observation_retention_state state WHERE singleton FOR UPDATE;

    IF upper_at IS NULL THEN
        -- 순회 상한을 고정해 새 원본이 계속 들어와도 보호/잠금 해제된 앞쪽을 다시 방문한다.
        SELECT edge.received_at, edge.id INTO upper_at, high_id
        FROM (
            SELECT requested_kinds[p] AS kind, max(requested_cutoffs[p]) AS cutoff
            FROM generate_subscripts(requested_kinds, 1) p GROUP BY requested_kinds[p]
        ) policy CROSS JOIN LATERAL (
            SELECT observation.received_at, observation.id
            FROM public.source_observations observation
            WHERE observation.observation_kind = policy.kind AND observation.received_at < policy.cutoff
            ORDER BY observation.received_at DESC, observation.id DESC LIMIT 1
        ) edge ORDER BY edge.received_at DESC, edge.id DESC LIMIT 1;
    END IF;

    -- kind별 인덱스 탐색은 최대 16×1000행, 합친 후보는 최대 1000행이다.
    -- 보호 판정과 SKIP LOCKED는 이 창을 만든 뒤 실행하므로 보호/잠금 prefix를 무한히 훑지 않는다.
    WITH policies AS (
        SELECT requested_kinds[p] AS kind, max(requested_cutoffs[p]) AS cutoff
        FROM generate_subscripts(requested_kinds, 1) p GROUP BY requested_kinds[p]
    ), scan_window AS MATERIALIZED (
        SELECT candidate.id, candidate.received_at
        FROM policies policy CROSS JOIN LATERAL (
            SELECT observation.id, observation.received_at FROM public.source_observations observation
            WHERE observation.observation_kind = policy.kind AND observation.received_at < policy.cutoff
              AND (observation.received_at, observation.id) > (previous_at, previous_id)
              AND (observation.received_at, observation.id) <= (upper_at, high_id)
            ORDER BY observation.received_at, observation.id LIMIT scan_limit
        ) candidate ORDER BY candidate.received_at, candidate.id LIMIT scan_limit
    )
    SELECT array_agg(id ORDER BY received_at, id), array_agg(received_at ORDER BY received_at, id)
    INTO candidate_ids, candidate_times FROM scan_window;
    scanned := coalesce(cardinality(candidate_ids), 0);

    SELECT array_agg(candidate.id ORDER BY candidate.received_at, candidate.id),
           array_agg(candidate.received_at ORDER BY candidate.received_at, candidate.id)
    INTO locked_ids, locked_times
    FROM (
        SELECT observation.id, observation.received_at FROM public.source_observations observation
        WHERE observation.id = ANY(candidate_ids)
          AND NOT public.source_observation_retention_protected(observation.id)
        ORDER BY observation.received_at, observation.id
        LIMIT requested_limit FOR UPDATE OF observation SKIP LOCKED
    ) candidate;
    locked := coalesce(cardinality(locked_ids), 0);
    -- 행 잠금 획득 뒤 새 snapshot에서 보호를 재검사하고 FK CASCADE/SET NULL과 원자적으로 삭제한다.
    WITH deleted AS (
        DELETE FROM public.source_observations observation
        WHERE observation.id = ANY(locked_ids)
          AND NOT public.source_observation_retention_protected(observation.id)
        RETURNING observation.id
    )
    SELECT coalesce(array_agg(deleted.id), '{}') INTO deleted_ids FROM deleted;

    -- 삭제 상한을 채운 창은 마지막 잠근 행 다음부터, 아니면 창 끝 다음부터 이어 간다.
    -- passed는 다음 창이 다시 보지 않는 후보 수이며 삭제된 후보를 포함한다.
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
$$;


--
-- Name: delete_youtube_channel_photo_variant_retention_batch(timestamp with time zone, integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_youtube_channel_photo_variant_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_received_at timestamp with time zone)
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
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


--
-- Name: delete_youtube_channel_profile_evidence_retention_batch(timestamp with time zone, integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_youtube_channel_profile_evidence_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_received_at timestamp with time zone)
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
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


--
-- Name: delete_youtube_content_absence_slot_retention_batch(timestamp with time zone, integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_youtube_content_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_scheduled_for timestamp with time zone)
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
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


--
-- Name: delete_youtube_live_absence_slot_retention_batch(timestamp with time zone, integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.delete_youtube_live_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) RETURNS TABLE(deleted_id bigint)
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
    WITH active_old_live AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PENDING'
          AND observation.observation_kind = 'live_snapshot'
          AND COALESCE(observation.source_event_at, observation.scheduled_for) < requested_cutoff
        LIMIT 1
    ),
    processing_old_live AS (
        SELECT 1
        FROM public.source_observation_queue AS queue
        JOIN public.source_observations AS observation
          ON observation.id = queue.observation_id
        WHERE queue.status = 'PROCESSING'
          AND observation.observation_kind = 'live_snapshot'
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
$$;


--
-- Name: discard_bot_reply_outbox_manual_review(bigint, text, text, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.discard_bot_reply_outbox_manual_review(requested_outbox_id bigint, operator_actor text, operator_reason text, observed_iris_state text) RETURNS text
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $_$
DECLARE
    decided_at TIMESTAMPTZ := clock_timestamp();
    normalized_actor TEXT := btrim(operator_actor);
    normalized_reason TEXT := btrim(operator_reason);
    normalized_iris_state TEXT := btrim(observed_iris_state);
    target_id BIGINT;
    target_status TEXT;
BEGIN
    SELECT id, status
    INTO target_id, target_status
    FROM public.bot_reply_outbox
    WHERE id = requested_outbox_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RETURN 'not_found';
    END IF;
    IF target_status <> 'manual_review' THEN
        RETURN 'not_manual_review';
    END IF;
    IF normalized_actor IS NULL
        OR normalized_actor !~ '^[A-Za-z0-9._:@-]{1,64}$'
        OR normalized_reason IS NULL
        OR octet_length(normalized_reason) NOT BETWEEN 1 AND 256
        OR normalized_reason ~ '[[:cntrl:]]'
    THEN
        RETURN 'invalid_operator_metadata';
    END IF;
    IF normalized_iris_state IS NULL
        OR normalized_iris_state NOT IN (
            'queued',
            'preparing',
            'prepared',
            'sending',
            'handoff_completed',
            'failed',
            'outcome_unknown',
            'not_found'
        )
    THEN
        RETURN 'invalid_iris_state';
    END IF;

    INSERT INTO public.bot_reply_outbox_resolution_audit (
        outbox_id,
        decision,
        observed_iris_state,
        actor,
        reason,
        recorded_at
    ) VALUES (
        target_id,
        'discarded_without_replay',
        normalized_iris_state,
        normalized_actor,
        normalized_reason,
        decided_at
    );

    UPDATE public.bot_reply_outbox
    SET status = 'discarded',
        payload = NULL,
        claim_token = NULL,
        lease_until = NULL,
        last_error = 'operator discarded manual review without replay',
        updated_at = decided_at
    WHERE id = target_id;

    RETURN 'discarded';
END
$_$;


--
-- Name: enforce_bot_reply_outbox_discard_audit(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.enforce_bot_reply_outbox_discard_audit() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
    IF NEW.status <> 'discarded' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'INSERT' THEN
        RAISE EXCEPTION 'discarded reply transition requires an audited manual-review decision'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.status = 'discarded' THEN
        RETURN NEW;
    END IF;
    IF OLD.status <> 'manual_review'
        OR NEW.payload IS NOT NULL
        OR NEW.claim_token IS NOT NULL
        OR NEW.lease_until IS NOT NULL
        OR NOT EXISTS (
            SELECT 1
            FROM public.bot_reply_outbox_resolution_audit AS audit
            WHERE audit.outbox_id = NEW.id
              AND audit.decision = 'discarded_without_replay'
        )
    THEN
        RAISE EXCEPTION 'discarded reply transition requires an audited manual-review decision'
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END
$$;


--
-- Name: grant_bot_reply_outbox_manual_replay(bigint, text, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.grant_bot_reply_outbox_manual_replay(requested_outbox_id bigint, operator_actor text, operator_reason text) RETURNS text
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $_$
DECLARE
    granted_at TIMESTAMPTZ;
    normalized_actor TEXT := btrim(operator_actor);
    normalized_reason TEXT := btrim(operator_reason);
    target_id BIGINT;
    target_status TEXT;
    target_created_at TIMESTAMPTZ;
    target_replay_grants INTEGER;
    next_grant_number INTEGER;
BEGIN
    SELECT id, status, created_at, operator_replay_grants
    INTO target_id, target_status, target_created_at, target_replay_grants
    FROM public.bot_reply_outbox
    WHERE id = requested_outbox_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RETURN 'not_found';
    END IF;
    IF target_status <> 'manual_review' THEN
        RETURN 'not_manual_review';
    END IF;
    -- 잠금 대기가 끝난 시각으로 재발급 제한과 감사 시각을 함께 판정합니다.
    granted_at := clock_timestamp();
    IF granted_at >= target_created_at + interval '144 hours' THEN
        RETURN 'cutoff_expired';
    END IF;
    IF normalized_actor !~ '^[A-Za-z0-9._:@-]{1,64}$'
        OR octet_length(normalized_reason) NOT BETWEEN 1 AND 256
        OR normalized_reason ~ '[[:cntrl:]]'
    THEN
        RETURN 'invalid_operator_metadata';
    END IF;

    next_grant_number := target_replay_grants + 1;
    INSERT INTO public.bot_reply_outbox_replay_audit (
        outbox_id, grant_number, event_type, actor, reason, recorded_at
    ) VALUES (
        target_id, next_grant_number, 'granted', normalized_actor, normalized_reason, granted_at
    );

    UPDATE public.bot_reply_outbox
    SET status = 'pending',
        claim_token = NULL,
        lease_until = NULL,
        last_error = '',
        operator_replay_grants = next_grant_number,
        available_at = granted_at,
        updated_at = granted_at
    WHERE id = target_id;

    RETURN 'replayed';
END
$_$;


--
-- Name: guard_youtube_collection_runtime_bundles(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.guard_youtube_collection_runtime_bundles() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
BEGIN
    PERFORM public.lock_youtube_collection_runtime_bundles();
    RETURN NULL;
END
$$;


--
-- Name: lock_current_youtube_collection_projection(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.lock_current_youtube_collection_projection() RETURNS TABLE(generation bigint)
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
#variable_conflict use_column
BEGIN
    PERFORM 1
    FROM public.youtube_collection_projection_guard AS guard
    WHERE guard.guard_key
    FOR SHARE OF guard;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'youtube collection projection guard row is missing'
            USING ERRCODE = 'P0002';
    END IF;

    RETURN QUERY
    SELECT projection.generation
    FROM public.youtube_collection_projection_generations AS projection
    WHERE projection.status = 'CURRENT'
      AND projection.valid_until > pg_catalog.clock_timestamp();
END
$$;


--
-- Name: lock_observation_contract(text, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.lock_observation_contract(requested_provider text, requested_observation_kind text) RETURNS TABLE(current_schema_version smallint, current_generation bigint)
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
    SELECT contract.current_schema_version,
           contract.current_generation
    FROM public.observation_contract_generations AS contract
    WHERE contract.provider = requested_provider
      AND contract.observation_kind = requested_observation_kind
    FOR SHARE OF contract
$$;


--
-- Name: lock_source_observation(bigint); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.lock_source_observation(requested_observation_id bigint) RETURNS TABLE(provider text, observation_kind text, subject_key text, observation_key text, schema_version smallint, contract_generation bigint, evidence_sha256 text)
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
    SELECT observation.provider, observation.observation_kind, observation.subject_key,
           encode(observation.observation_key, 'hex'), observation.schema_version, observation.contract_generation,
           encode(observation.evidence_sha256, 'hex')
    FROM public.source_observations observation
    WHERE observation.id = requested_observation_id
    FOR SHARE OF observation
$$;


--
-- Name: lock_source_observation_identity(text, text, text, text, smallint, bigint); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.lock_source_observation_identity(requested_provider text, requested_observation_kind text, requested_subject_key text, requested_observation_key text, requested_schema_version smallint, requested_contract_generation bigint) RETURNS TABLE(id bigint, evidence_sha256 text)
    LANGUAGE sql SECURITY DEFINER ROWS 1
    SET search_path TO 'pg_catalog'
    AS $$
    SELECT observation.id, encode(observation.evidence_sha256, 'hex')
    FROM public.source_observations observation
    WHERE observation.provider = requested_provider
      AND observation.observation_kind = requested_observation_kind
      AND observation.subject_key = requested_subject_key
      AND observation.observation_key = decode(requested_observation_key, 'hex')
      AND observation.schema_version = requested_schema_version
      AND observation.contract_generation = requested_contract_generation
    FOR SHARE OF observation
$$;


--
-- Name: lock_source_observation_payload(text, smallint, bytea, jsonb); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.lock_source_observation_payload(requested_kind text, requested_version smallint, requested_digest bytea, expected_payload jsonb) RETURNS TABLE(id bigint)
    LANGUAGE plpgsql SECURITY DEFINER ROWS 1
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    stored_payload jsonb;
BEGIN
    SELECT candidate.id, candidate.payload
    INTO id, stored_payload
    FROM public.source_observation_payloads AS candidate
    WHERE candidate.observation_kind = requested_kind
      AND candidate.schema_version = requested_version
      AND candidate.canonical_profile = 'source-observation-canonical-json-v1'
      AND candidate.payload_sha256 = requested_digest
    FOR KEY SHARE OF candidate;
    IF FOUND THEN
        IF stored_payload IS DISTINCT FROM expected_payload THEN
            RAISE EXCEPTION 'source observation payload digest collision or content mismatch';
        END IF;
        RETURN NEXT;
    END IF;
END
$$;


--
-- Name: lock_youtube_collection_runtime_bundles(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.lock_youtube_collection_runtime_bundles() RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
BEGIN
    -- 기존 collector의 SHARE/UPDATE 잠금과 같은 행을 쓴다. 실제 버전 변경으로 오래된 RR snapshot도 거부한다.
    -- 같은 트랜잭션의 재진입은 행을 다시 쓰지 않는다. 기존 singleton 누락은 복구하지 않고 거부한다.
    UPDATE public.youtube_collection_projection_guard AS guard
    SET runtime_writer = pg_catalog.pg_current_xact_id()
    WHERE guard.guard_key AND guard.runtime_writer IS DISTINCT FROM pg_catalog.pg_current_xact_id();
    IF NOT FOUND AND NOT EXISTS (SELECT 1 FROM public.youtube_collection_projection_guard WHERE guard_key) THEN
        RAISE EXCEPTION 'youtube collection projection guard is missing';
    END IF;
END
$$;


--
-- Name: lock_youtube_live_runtime_state(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.lock_youtube_live_runtime_state() RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
BEGIN
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('youtube_live_runtime_global', 0));
END
$$;


--
-- Name: lock_youtube_live_runtime_state_shared(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.lock_youtube_live_runtime_state_shared() RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
BEGIN
    PERFORM pg_catalog.pg_advisory_xact_lock_shared(pg_catalog.hashtextextended('youtube_live_runtime_global', 0));
END
$$;


--
-- Name: notification_template_row_version(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.notification_template_row_version() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF (NEW.body, NEW.template_key, NEW.channel_id, NEW.format_variant, NEW.id)
        IS DISTINCT FROM (OLD.body, OLD.template_key, OLD.channel_id, OLD.format_variant, OLD.id) THEN
        NEW.row_version := OLD.row_version + 1;
    ELSE
        NEW.row_version := OLD.row_version;
    END IF;
    RETURN NEW;
END;
$$;


--
-- Name: record_alarm_dispatch_closeout(uuid, bigint, bigint[], text, text, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.record_alarm_dispatch_closeout(p_receipt_id uuid, p_addressed_delivery_id bigint, p_expected_target_ids bigint[], p_expected_sha256 text, p_operator_id text, p_reason text) RETURNS void
    LANGUAGE plpgsql
    AS $_$
DECLARE
    unit_id bigint;
    locked_count integer;
    snapshot record;
BEGIN
    IF current_setting('transaction_isolation') <> 'serializable' THEN
        RAISE EXCEPTION 'alarm dispatch closeout requires serializable transaction';
    END IF;
    IF p_receipt_id IS NULL OR p_addressed_delivery_id IS NULL OR p_addressed_delivery_id <= 0
       OR p_expected_target_ids IS NULL OR cardinality(p_expected_target_ids) NOT BETWEEN 1 AND 100
       OR p_expected_sha256 IS NULL OR p_expected_sha256 !~ '^[0-9a-f]{64}$'
       OR p_operator_id IS NULL OR length(btrim(p_operator_id)) NOT BETWEEN 1 AND 128
       OR p_operator_id <> btrim(p_operator_id) OR p_operator_id ~ '[[:cntrl:]]'
       OR p_reason IS NULL OR length(btrim(p_reason)) NOT BETWEEN 1 AND 1024
       OR p_reason <> btrim(p_reason) OR p_reason ~ '[[:cntrl:]]' THEN
        RAISE EXCEPTION 'invalid alarm dispatch closeout request';
    END IF;
    IF array_position(p_expected_target_ids, p_addressed_delivery_id) IS NULL THEN
        RAISE EXCEPTION 'addressed delivery missing from reviewed targets';
    END IF;

    -- 대상과 전체 send unit을 requeue와 같은 순서로 잠그고, 잠금 대기 뒤 재조회한다.
    SELECT d.send_unit_id INTO unit_id
    FROM public.alarm_dispatch_deliveries d WHERE d.id = p_addressed_delivery_id FOR UPDATE;
    IF NOT FOUND OR unit_id IS NULL THEN
        RAISE EXCEPTION 'addressed delivery or send unit missing';
    END IF;
    PERFORM d.id FROM public.alarm_dispatch_deliveries d
    WHERE d.send_unit_id = unit_id ORDER BY d.id LIMIT 101 FOR UPDATE OF d;
    GET DIAGNOSTICS locked_count = ROW_COUNT;
    SELECT s.send_unit_id, s.target_ids, s.target_revisions, s.status_metadata,
           s.original_sha256, s.member_count INTO snapshot
    FROM public.alarm_dispatch_closeout_snapshot(p_addressed_delivery_id) AS s;
    IF NOT FOUND OR snapshot.send_unit_id <> unit_id OR snapshot.member_count <> locked_count
       OR snapshot.member_count > 100
       OR snapshot.target_ids IS DISTINCT FROM p_expected_target_ids
       OR snapshot.original_sha256 IS DISTINCT FROM p_expected_sha256 THEN
        RAISE EXCEPTION 'reviewed closeout snapshot changed';
    END IF;
    IF EXISTS (
        SELECT 1 FROM public.alarm_dispatch_deliveries d
        WHERE d.id = ANY(snapshot.target_ids)
          AND (d.status <> 'quarantined' OR d.cancelled_at IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'closeout requires only quarantined, uncancelled targets';
    END IF;
    IF EXISTS (SELECT 1 FROM public.alarm_dispatch_closeout_receipts r WHERE r.send_unit_id = unit_id) THEN
        RAISE EXCEPTION 'send unit already has a closeout receipt';
    END IF;
    INSERT INTO public.alarm_dispatch_closeout_receipts (
        receipt_id, send_unit_id, addressed_delivery_id, target_ids,
        target_revisions, status_metadata, original_sha256, operator_id, reason
    ) VALUES (
        p_receipt_id, unit_id, p_addressed_delivery_id, snapshot.target_ids,
        snapshot.target_revisions, snapshot.status_metadata, snapshot.original_sha256,
        p_operator_id, p_reason
    );
END
$_$;


--
-- Name: record_youtube_live_review(uuid, text, text, text, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.record_youtube_live_review(p_receipt_id uuid, p_video_id text, p_expected_sha256 text, p_operator_id text, p_reason text) RETURNS void
    LANGUAGE plpgsql
    AS $_$
DECLARE
    snapshot record;
BEGIN
    IF current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'live review requires read committed transaction';
    END IF;
    IF p_receipt_id IS NULL OR p_video_id IS NULL OR length(p_video_id) NOT BETWEEN 1 AND 128
       OR p_expected_sha256 IS NULL OR p_expected_sha256 !~ '^[0-9a-f]{64}$'
       OR p_operator_id IS NULL OR length(p_operator_id) NOT BETWEEN 1 AND 128
       OR p_operator_id <> btrim(p_operator_id) OR p_operator_id ~ '[[:cntrl:]]'
       OR p_reason IS NULL OR length(p_reason) NOT BETWEEN 1 AND 1024
       OR p_reason <> btrim(p_reason) OR p_reason ~ '[[:cntrl:]]' THEN
        RAISE EXCEPTION 'invalid live review request';
    END IF;
    PERFORM public.lock_youtube_live_runtime_state();
    PERFORM video_id FROM public.youtube_live_sessions WHERE video_id = p_video_id FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'reviewed live session missing';
    END IF;
    PERFORM video_id FROM public.youtube_live_reconciliation_heads WHERE video_id = p_video_id FOR UPDATE;
    PERFORM video_id FROM public.youtube_live_pending_ends WHERE video_id = p_video_id FOR UPDATE;
    PERFORM video_id FROM public.youtube_video_availability WHERE video_id = p_video_id FOR UPDATE;
    SELECT reviewed.original_snapshot,reviewed.snapshot_sha256,reviewed.evidence_refs,reviewed.reviewable
    INTO snapshot FROM public.youtube_live_review_snapshot(p_video_id) reviewed;
    IF snapshot.snapshot_sha256 IS DISTINCT FROM p_expected_sha256 OR NOT snapshot.reviewable THEN
        RAISE EXCEPTION 'reviewed live snapshot changed or is not unresolved';
    END IF;
    INSERT INTO public.youtube_live_review_receipts
        (receipt_id,video_id,snapshot_sha256,original_snapshot,evidence_refs,disposition,operator_id,reason)
    VALUES (p_receipt_id,p_video_id,snapshot.snapshot_sha256,snapshot.original_snapshot,
            snapshot.evidence_refs,'closed_unresolved',p_operator_id,p_reason);
    PERFORM public.refresh_youtube_live_runtime_state(ARRAY[p_video_id]);
END
$_$;


--
-- Name: refresh_youtube_collection_runtime_bundle_keys(bigint[], text[], text[]); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.refresh_youtube_collection_runtime_bundle_keys(p_generations bigint[], p_kinds text[], p_subjects text[]) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
BEGIN
    PERFORM public.lock_youtube_collection_runtime_bundles();
    -- 내부 호출자는 세 배열을 같은 key 순서로 만든다. VOLATILE의 새 문장이 잠금 대기 뒤 커밋도 읽는다.
    WITH requested AS MATERIALIZED (
        SELECT projection_generation, kind COLLATE "C" AS kind, subject_key COLLATE "C" AS subject_key
        FROM ROWS FROM (pg_catalog.unnest(p_generations), pg_catalog.unnest(p_kinds), pg_catalog.unnest(p_subjects))
            AS keys(projection_generation, kind, subject_key)
    ), fresh AS MATERIALIZED (
        SELECT keys.projection_generation, keys.kind, keys.subject_key,
               CASE WHEN mapping.rpc_per_kind THEN count(target.observation_kind) ELSE 1 END AS rpc_calls,
               min(target.poll_interval_ms) AS interval_ms, min(target.created_at) AS created_at,
               min(COALESCE(target.not_before, '-infinity'::timestamptz)) AS eligible_at
        FROM requested AS keys
        JOIN public.youtube_collection_projection_generations AS generation
          ON generation.generation = keys.projection_generation AND generation.status IN ('CURRENT', 'STAGING')
        JOIN public.youtube_collection_runtime_kinds AS mapping ON mapping.kind COLLATE "C" = keys.kind
        JOIN public.youtube_collection_targets AS target
          ON target.projection_generation = keys.projection_generation
         -- 원본 PK의 기본 collation index를 쓰되 최종 identity는 반드시 bytewise로 확인한다.
         AND target.subject_key = keys.subject_key COLLATE "default"
         AND target.subject_key COLLATE "C" = keys.subject_key
         AND target.observation_kind = mapping.observation_kind
         AND target.enabled
        GROUP BY keys.projection_generation, keys.kind, keys.subject_key, mapping.rpc_per_kind
    ), removed AS (
        DELETE FROM public.youtube_collection_runtime_bundles AS bundle
        USING requested AS keys
        WHERE (bundle.projection_generation, bundle.kind, bundle.subject_key) =
              (keys.projection_generation, keys.kind, keys.subject_key)
          AND NOT EXISTS (
              SELECT 1 FROM fresh
              WHERE (fresh.projection_generation, fresh.kind, fresh.subject_key) =
                    (bundle.projection_generation, bundle.kind, bundle.subject_key)
          )
    )
    INSERT INTO public.youtube_collection_runtime_bundles AS bundle
        (projection_generation, kind, subject_key, rpc_calls, interval_ms, created_at, eligible_at)
    SELECT projection_generation, kind, subject_key, rpc_calls, interval_ms, created_at, eligible_at
    FROM fresh
    ORDER BY projection_generation, kind, subject_key
    ON CONFLICT (projection_generation, kind, subject_key) DO UPDATE SET
        rpc_calls = EXCLUDED.rpc_calls, interval_ms = EXCLUDED.interval_ms,
        created_at = EXCLUDED.created_at, eligible_at = EXCLUDED.eligible_at
    WHERE bundle IS DISTINCT FROM EXCLUDED;
END
$$;


--
-- Name: refresh_youtube_collection_runtime_bundles(bigint[]); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.refresh_youtube_collection_runtime_bundles(p_generations bigint[]) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
DECLARE
    generations bigint[];
    kinds text[];
    subjects text[];
BEGIN
    PERFORM public.lock_youtube_collection_runtime_bundles();
    -- header 전환만 generation 전체를 갱신한다. RETIRED 원본 이력은 읽지 않고 기존 파생 key만 제거한다.
    WITH keys AS (
        SELECT bundle.projection_generation, bundle.kind, bundle.subject_key
        FROM public.youtube_collection_runtime_bundles AS bundle
        WHERE bundle.projection_generation = ANY(p_generations)
        UNION
        SELECT target.projection_generation, mapping.kind COLLATE "C", target.subject_key COLLATE "C"
        FROM public.youtube_collection_projection_generations AS generation
        JOIN public.youtube_collection_targets AS target ON target.projection_generation = generation.generation
        JOIN public.youtube_collection_runtime_kinds AS mapping ON mapping.observation_kind = target.observation_kind
        WHERE generation.generation = ANY(p_generations)
          AND generation.status IN ('CURRENT', 'STAGING') AND target.enabled
    )
    SELECT array_agg(projection_generation ORDER BY projection_generation, kind, subject_key),
           array_agg(kind ORDER BY projection_generation, kind, subject_key),
           array_agg(subject_key ORDER BY projection_generation, kind, subject_key)
    INTO generations, kinds, subjects FROM keys;
    IF generations IS NOT NULL THEN
        PERFORM public.refresh_youtube_collection_runtime_bundle_keys(generations, kinds, subjects);
    END IF;
END
$$;


--
-- Name: refresh_youtube_collection_runtime_bundles_statement(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
DECLARE
    generations bigint[];
    kinds text[];
    subjects text[];
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        -- 두 원본 중 어느 쪽이 비어도 유효 bundle은 없다. TRUNCATE에는 transition table이 없다.
        DELETE FROM public.youtube_collection_runtime_bundles;
        RETURN NULL;
    END IF;

    IF TG_TABLE_NAME = 'youtube_collection_projection_generations' THEN
        IF TG_OP = 'INSERT' THEN
            SELECT array_agg(generation ORDER BY generation) INTO generations
            FROM runtime_new WHERE status IN ('CURRENT', 'STAGING');
        ELSIF TG_OP = 'DELETE' THEN
            SELECT array_agg(generation ORDER BY generation) INTO generations FROM runtime_old;
        ELSE
            -- TTL·hash·eligibility heartbeat는 구조 집계를 건드리지 않는다. PK 이동은 양쪽을 포함한다.
            WITH changed AS MATERIALIZED (
                SELECT prior.generation AS old_generation, next.generation AS new_generation
                FROM runtime_old AS prior FULL JOIN runtime_new AS next USING (generation)
                WHERE prior.generation IS NULL OR next.generation IS NULL
                   OR (prior.status IN ('CURRENT', 'STAGING')) IS DISTINCT FROM (next.status IN ('CURRENT', 'STAGING'))
            )
            SELECT array_agg(generation ORDER BY generation) INTO generations
            FROM (SELECT old_generation AS generation FROM changed WHERE old_generation IS NOT NULL
                  UNION SELECT new_generation FROM changed WHERE new_generation IS NOT NULL) AS keys;
        END IF;
        IF generations IS NOT NULL THEN
            PERFORM public.refresh_youtube_collection_runtime_bundles(generations);
        END IF;
        RETURN NULL;
    END IF;

    -- target 한 행의 변경으로 generation 전체나 RETIRED 이력을 집계하지 않는다.
    IF TG_OP = 'INSERT' THEN
        WITH keys AS (
            SELECT DISTINCT target.projection_generation, mapping.kind COLLATE "C" AS kind,
                   target.subject_key COLLATE "C" AS subject_key
            FROM runtime_new AS target JOIN public.youtube_collection_runtime_kinds AS mapping
              ON mapping.observation_kind = target.observation_kind
        )
        SELECT array_agg(projection_generation ORDER BY projection_generation, kind, subject_key),
               array_agg(kind ORDER BY projection_generation, kind, subject_key),
               array_agg(subject_key ORDER BY projection_generation, kind, subject_key)
        INTO generations, kinds, subjects FROM keys;
    ELSIF TG_OP = 'DELETE' THEN
        WITH keys AS (
            SELECT DISTINCT target.projection_generation, mapping.kind COLLATE "C" AS kind,
                   target.subject_key COLLATE "C" AS subject_key
            FROM runtime_old AS target JOIN public.youtube_collection_runtime_kinds AS mapping
              ON mapping.observation_kind = target.observation_kind
        )
        SELECT array_agg(projection_generation ORDER BY projection_generation, kind, subject_key),
               array_agg(kind ORDER BY projection_generation, kind, subject_key),
               array_agg(subject_key ORDER BY projection_generation, kind, subject_key)
        INTO generations, kinds, subjects FROM keys;
    ELSE
        WITH changed AS MATERIALIZED (
            SELECT prior.projection_generation AS old_generation, prior.subject_key AS old_subject,
                   prior.observation_kind AS old_observation, next.projection_generation AS new_generation,
                   next.subject_key AS new_subject, next.observation_kind AS new_observation
            FROM runtime_old AS prior FULL JOIN runtime_new AS next
              ON prior.projection_generation = next.projection_generation
             AND prior.subject_key COLLATE "C" = next.subject_key COLLATE "C"
             AND prior.observation_kind COLLATE "C" = next.observation_kind COLLATE "C"
            WHERE prior.projection_generation IS NULL OR next.projection_generation IS NULL
               OR (prior.enabled, prior.poll_interval_ms, prior.created_at, prior.not_before)
                  IS DISTINCT FROM (next.enabled, next.poll_interval_ms, next.created_at, next.not_before)
        ), keys AS (
            SELECT DISTINCT target.projection_generation, mapping.kind COLLATE "C" AS kind,
                   target.subject_key COLLATE "C" AS subject_key
            FROM changed CROSS JOIN LATERAL (
                VALUES (old_generation, old_subject, old_observation),
                       (new_generation, new_subject, new_observation)
            ) AS target(projection_generation, subject_key, observation_kind)
            JOIN public.youtube_collection_runtime_kinds AS mapping ON mapping.observation_kind = target.observation_kind
            WHERE target.projection_generation IS NOT NULL
        )
        SELECT array_agg(projection_generation ORDER BY projection_generation, kind, subject_key),
               array_agg(kind ORDER BY projection_generation, kind, subject_key),
               array_agg(subject_key ORDER BY projection_generation, kind, subject_key)
        INTO generations, kinds, subjects FROM keys;
    END IF;
    IF generations IS NOT NULL THEN
        PERFORM public.refresh_youtube_collection_runtime_bundle_keys(generations, kinds, subjects);
    END IF;
    RETURN NULL;
END
$$;


--
-- Name: refresh_youtube_live_runtime_state(text[]); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.refresh_youtube_live_runtime_state(p_video_ids text[]) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog', 'pg_temp'
    AS $$
BEGIN
    -- 잠금 대기 뒤 문장이 새 snapshot으로 앞선 커밋을 읽어야 하므로 고정 snapshot 호출은 거부한다.
    IF pg_catalog.current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'live runtime refresh requires read committed transaction';
    END IF;
    IF p_video_ids IS NULL OR pg_catalog.cardinality(p_video_ids) = 0 THEN
        RETURN;
    END IF;
    -- 같은 영상의 갱신을 커밋까지 직렬화한다. 영상 key를 1024개 bucket으로 접어 전체 재계산도 잠금 표를 고갈시키지 않으며,
    -- 같은 bucket의 다른 영상은 커밋까지 기다릴 뿐이다. 한 문장에서 bucket 순서로 잡아 writer 사이 교착을 만들지 않는다.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtext('youtube_live_runtime_video'), ordered.bucket)
    FROM (SELECT DISTINCT (pg_catalog.hashtextextended(input.video_id, 0) & 1023)::integer AS bucket
          FROM pg_catalog.unnest(p_video_ids) AS input(video_id)
          WHERE input.video_id IS NOT NULL
          ORDER BY 1) AS ordered;

    -- VOLATILE의 문장별 snapshot으로 잠금 대기 중 커밋된 원본도 읽는다.
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
          AND receipt.semantic_value=public.youtube_live_review_source_value(
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


--
-- Name: reject_alarm_dispatch_closeout_receipt_change(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.reject_alarm_dispatch_closeout_receipt_change() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION 'alarm dispatch closeout receipts are append-only';
END
$$;


--
-- Name: reject_bot_reply_outbox_replay_audit_mutation(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
    IF TG_OP = 'DELETE'
        AND NOT EXISTS (
            SELECT 1
            FROM public.bot_reply_outbox
            WHERE id = OLD.outbox_id
        )
    THEN
        RETURN OLD;
    END IF;

    RAISE EXCEPTION 'bot_reply_outbox_replay_audit events are immutable'
        USING ERRCODE = '55000';
END
$$;


--
-- Name: reject_bot_reply_outbox_resolution_audit_mutation(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.reject_bot_reply_outbox_resolution_audit_mutation() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
    IF TG_OP = 'DELETE'
        AND NOT EXISTS (
            SELECT 1
            FROM public.bot_reply_outbox
            WHERE id = OLD.outbox_id
        )
    THEN
        RETURN OLD;
    END IF;

    RAISE EXCEPTION 'bot_reply_outbox_resolution_audit events are immutable'
        USING ERRCODE = '55000';
END
$$;


--
-- Name: reject_youtube_live_review_receipt_change(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.reject_youtube_live_review_receipt_change() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION 'live review receipts are append-only';
END
$$;


--
-- Name: require_source_observation_payload(bigint, text, smallint, text, smallint, text, bytea, jsonb); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.require_source_observation_payload(requested_id bigint, requested_kind text, requested_version smallint, actual_kind text, actual_version smallint, actual_profile text, actual_digest bytea, actual_payload jsonb) RETURNS jsonb
    LANGUAGE plpgsql STABLE SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
BEGIN
    IF actual_payload IS NULL OR actual_kind IS DISTINCT FROM requested_kind
       OR actual_version IS DISTINCT FROM requested_version
       OR actual_profile IS DISTINCT FROM 'source-observation-canonical-json-v1'
       OR actual_digest IS NULL OR octet_length(actual_digest) <> 32 THEN
        RAISE EXCEPTION 'missing or corrupt source observation payload for observation %', requested_id;
    END IF;
    RETURN actual_payload;
END
$$;


--
-- Name: scrub_bot_command_execution_terminal_summary(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.scrub_bot_command_execution_terminal_summary() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.result_summary := NEW.status;
    RETURN NEW;
END
$$;


--
-- Name: source_observation_decision_id(text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.source_observation_decision_id(requested_name text) RETURNS smallint
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    resolved smallint;
BEGIN
    FOR attempt IN 1..3 LOOP
        SELECT decision.id INTO resolved FROM public.source_observation_decisions decision WHERE decision.name = requested_name;
        IF FOUND THEN
            RETURN resolved;
        END IF;
        INSERT INTO public.source_observation_decisions (name) VALUES (requested_name)
        ON CONFLICT (name) DO NOTHING
        RETURNING id INTO resolved;
        IF resolved IS NOT NULL THEN
            RETURN resolved;
        END IF;
    END LOOP;
    RAISE EXCEPTION 'source observation decision could not be resolved' USING ERRCODE = '40001';
END
$$;


--
-- Name: source_observation_entity_kind_id(text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.source_observation_entity_kind_id(requested_name text) RETURNS smallint
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
DECLARE
    resolved smallint;
BEGIN
    FOR attempt IN 1..3 LOOP
        SELECT kind.id INTO resolved FROM public.source_observation_entity_kinds kind WHERE kind.name = requested_name;
        IF FOUND THEN
            RETURN resolved;
        END IF;
        INSERT INTO public.source_observation_entity_kinds (name) VALUES (requested_name)
        ON CONFLICT (name) DO NOTHING
        RETURNING id INTO resolved;
        IF resolved IS NOT NULL THEN
            RETURN resolved;
        END IF;
    END LOOP;
    RAISE EXCEPTION 'source observation entity kind could not be resolved' USING ERRCODE = '40001';
END
$$;


--
-- Name: source_observation_provenance_id(text, text, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.source_observation_provenance_id(requested_job_key text, requested_collector_instance text, requested_collection_job_kind text) RETURNS integer
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $$
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
$$;


--
-- Name: source_observation_retention_protected(bigint); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.source_observation_retention_protected(requested_id bigint) RETURNS boolean
    LANGUAGE sql STABLE
    SET search_path TO 'pg_catalog'
    AS $$
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
$$;


--
-- Name: store_youtube_live_review_receipt_facts(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.store_youtube_live_review_receipt_facts() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    INSERT INTO public.youtube_live_review_receipt_facts (receipt_id,video_id,recorded_at,facts)
    VALUES (NEW.receipt_id,NEW.video_id,NEW.recorded_at,
            public.youtube_live_review_receipt_semantic_facts(NEW.original_snapshot));
    RETURN NULL;
END
$$;


--
-- Name: youtube_collection_job_candidates(bigint, text[], text, text, text[], integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_collection_job_candidates(p_generation bigint, p_kinds text[], p_provider text, p_job_kind text, p_excluded text[], p_limit integer) RETURNS TABLE(is_current boolean, subject_key text, min_interval_ms bigint, max_interval_ms bigint)
    LANGUAGE sql STABLE
    SET search_path TO 'pg_catalog'
    SET plan_cache_mode TO 'force_generic_plan'
    AS $$
WITH target_bundles AS (
    SELECT target.subject_key,
           MIN(target.poll_interval_ms) AS min_interval_ms,
           MAX(target.poll_interval_ms) AS max_interval_ms,
           MAX(target.priority) AS max_priority
    FROM public.youtube_collection_targets AS target
    WHERE target.projection_generation = p_generation
      AND target.observation_kind = ANY(p_kinds)
      AND target.enabled = TRUE
    GROUP BY target.subject_key
    -- not_before는 신규 입장 판정에만 쓴다. bundle의 한 행이라도 입장 가능하면 후보다.
    HAVING bool_or(target.not_before IS NULL OR target.not_before <= statement_timestamp())
), due AS (
    SELECT target.subject_key,
           target.min_interval_ms,
           target.max_interval_ms,
           target.max_priority,
           lease.job_key,
           CASE
             WHEN lease.job_key IS NULL THEN '-infinity'::timestamptz
             -- 주기가 줄었으면 이전 긴 주기로 계산된 next_due_at 대신 현재 주기의 다음 slot을 due로 본다.
             WHEN lease.slot_state = 'IDLE' THEN LEAST(
                 lease.next_due_at,
                 lease.scheduled_for + target.min_interval_ms * INTERVAL '1 millisecond'
             )
             WHEN lease.slot_state = 'DEFERRED' THEN lease.retry_not_before
             ELSE lease.lease_expires_at
           END AS effective_due_at
    FROM target_bundles AS target
    LEFT JOIN public.youtube_collection_job_leases AS lease
      ON lease.job_key = 'collector:' || p_provider || ':' || p_job_kind || ':' || target.subject_key
    WHERE ('collector:' || p_provider || ':' || p_job_kind || ':' || target.subject_key)
          <> ALL(p_excluded)
      AND (
           lease.job_key IS NULL
        OR (lease.slot_state = 'IDLE' AND LEAST(
                lease.next_due_at,
                lease.scheduled_for + target.min_interval_ms * INTERVAL '1 millisecond'
            ) <= statement_timestamp())
        OR (lease.slot_state = 'DEFERRED' AND lease.retry_not_before <= statement_timestamp())
        OR (lease.slot_state = 'ACTIVE' AND lease.lease_expires_at <= statement_timestamp())
      )
), projection AS (
    SELECT EXISTS (
        SELECT 1
        FROM public.youtube_collection_projection_generations AS generation
        WHERE generation.generation = p_generation
          AND generation.status = 'CURRENT'
          AND generation.valid_until > statement_timestamp()
    ) AS is_current
)
SELECT projection.is_current,
       due.subject_key,
       due.min_interval_ms,
       due.max_interval_ms
FROM projection
LEFT JOIN (
    SELECT due.subject_key,
           due.min_interval_ms,
           due.max_interval_ms,
           due.job_key,
           due.max_priority,
           due.effective_due_at
    FROM due
    -- 새 수명 검토 대상의 최초 discovery가 기존 LIVE 확인을 밀어내지 않는다.
    -- 다른 job 종류의 현행 신규 대상 우선순위는 유지한다.
    ORDER BY CASE WHEN p_job_kind = 'youtubejs_video_live' THEN due.max_priority ELSE 0 END DESC,
             (due.job_key IS NOT NULL), due.max_priority DESC, due.effective_due_at ASC, due.subject_key ASC
    LIMIT p_limit + 1
) AS due ON projection.is_current;
$$;


--
-- Name: youtube_collection_job_global_candidates(bigint, text[], boolean, text, text, text[]); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_collection_job_global_candidates(p_generation bigint, p_kinds text[], p_exact_subject boolean, p_subject_key text, p_job_key text, p_excluded text[]) RETURNS TABLE(is_current boolean, subject_key text, min_interval_ms bigint, max_interval_ms bigint)
    LANGUAGE sql STABLE
    SET search_path TO 'pg_catalog'
    SET plan_cache_mode TO 'force_generic_plan'
    AS $$
WITH target_bundle AS (
    SELECT COUNT(target.subject_key) AS target_count,
           MIN(target.poll_interval_ms) AS min_interval_ms,
           MAX(target.poll_interval_ms) AS max_interval_ms,
           MAX(target.priority) AS max_priority,
           -- not_before는 신규 입장 판정에만 쓴다. bundle의 한 행이라도 입장 가능하면 후보다.
           COUNT(target.subject_key) FILTER (
               WHERE target.not_before IS NULL OR target.not_before <= statement_timestamp()
           ) AS admissible_count
    FROM public.youtube_collection_targets AS target
    WHERE target.projection_generation = p_generation
      AND target.observation_kind = ANY(p_kinds)
      AND target.enabled = TRUE
      AND (NOT p_exact_subject OR target.subject_key = p_subject_key)
), identity AS (
    SELECT p_job_key AS job_key,
           p_subject_key AS subject_key,
           target_bundle.target_count,
           target_bundle.min_interval_ms,
           target_bundle.max_interval_ms,
           target_bundle.max_priority,
           target_bundle.admissible_count
    FROM target_bundle
), due AS (
    SELECT identity.job_key,
           identity.subject_key,
           identity.target_count,
           identity.min_interval_ms,
           identity.max_interval_ms,
           identity.max_priority,
           lease.slot_state,
           lease.next_due_at,
           lease.retry_not_before,
           lease.lease_expires_at
    FROM identity
    LEFT JOIN public.youtube_collection_job_leases AS lease
      ON lease.job_key = identity.job_key
    WHERE identity.target_count > 0
      AND identity.admissible_count > 0
      AND identity.job_key <> ALL(p_excluded)
      AND (
           lease.job_key IS NULL
        -- 주기가 줄었으면 이전 긴 주기로 계산된 next_due_at 대신 현재 주기의 다음 slot을 due로 본다.
        OR (lease.slot_state = 'IDLE' AND LEAST(
                lease.next_due_at,
                lease.scheduled_for + identity.min_interval_ms * INTERVAL '1 millisecond'
            ) <= statement_timestamp())
        OR (lease.slot_state = 'DEFERRED' AND lease.retry_not_before <= statement_timestamp())
        OR (lease.slot_state = 'ACTIVE' AND lease.lease_expires_at <= statement_timestamp())
      )
), projection AS (
    SELECT EXISTS (
        SELECT 1
        FROM public.youtube_collection_projection_generations AS generation
        WHERE generation.generation = p_generation
          AND generation.status = 'CURRENT'
          AND generation.valid_until > statement_timestamp()
    ) AS is_current
)
SELECT projection.is_current,
       due.subject_key,
       due.min_interval_ms,
       due.max_interval_ms
FROM projection
LEFT JOIN due ON projection.is_current;
$$;


--
-- Name: youtube_collection_membership_valid(text[], boolean, text, integer, bigint); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_collection_membership_valid(requested_kinds text[], requested_exact_subject boolean, requested_subject_key text, requested_target_count integer, requested_generation bigint) RETURNS boolean
    LANGUAGE sql STABLE
    SET search_path TO 'pg_catalog'
    SET plan_cache_mode TO 'force_generic_plan'
    AS $$
    SELECT COALESCE(requested_target_count > 0, FALSE)
       AND COALESCE(cardinality(requested_kinds) > 0, FALSE)
       AND COALESCE(requested_generation > 0, FALSE)
       AND requested_exact_subject IS NOT NULL
       AND EXISTS (
           SELECT 1
           FROM public.youtube_collection_projection_generations AS projection
           CROSS JOIN LATERAL (
               SELECT count(target.subject_key) AS target_count,
                      count(target.subject_key) FILTER (
                          WHERE target.member_since_generation BETWEEN 1 AND requested_generation
                      ) AS member_count
               FROM public.youtube_collection_targets AS target
               WHERE target.projection_generation = projection.generation
                 AND target.observation_kind = ANY (requested_kinds)
                 AND target.enabled
                 AND (NOT requested_exact_subject OR target.subject_key = requested_subject_key)
           ) AS scope
           WHERE projection.status = 'CURRENT'
             AND projection.valid_until > statement_timestamp()
             AND scope.target_count = requested_target_count
             AND scope.member_count = requested_target_count
       )
$$;


--
-- Name: youtube_live_review_current_facts(text[]); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_live_review_current_facts(p_video_ids text[]) RETURNS TABLE(video_id text, facts jsonb, reviewable boolean)
    LANGUAGE sql STABLE
    AS $$
SELECT session.video_id,
       to_jsonb(public.youtube_live_review_source_value(
           session.video_id,session.channel_id,session.status,session.lifecycle_origin,
           session.title,session.topic_id,session.is_premiere,session.scheduled_start_time,
           session.started_at,session.ended_at,session.live_first_seen_at,head.video_id,
           head.status,head.last_upcoming_positive_at,head.last_upcoming_positive_seen_at,head.last_live_positive_at,
           head.last_live_positive_seen_at,head.last_end_evidence_at,head.last_complete_absence_at,head.last_absence_scheduled_for,
           head.first_absence_scheduled_for,head.second_absence_scheduled_for,head.consecutive_absence_slots,head.last_absence_observation_id,
           head.end_candidate_kind,head.end_candidate_observation_id,head.ended_at,head.end_reason,
           pending.video_id,pending.channel_id,pending.kind,pending.observation_id,
           pending.effective_at,pending.scheduled_for,pending.ended_at,pending.negative_eligible,
           pending.scope_covers,availability.availability)),
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


--
-- Name: youtube_live_review_current_receipt(text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_live_review_current_receipt(p_video_id text) RETURNS TABLE(reviewed_at timestamp with time zone)
    LANGUAGE sql STABLE
    AS $$
SELECT max(receipt.reviewed_at)
FROM public.youtube_live_review_current_receipts(ARRAY[p_video_id]) receipt;
$$;


--
-- Name: youtube_live_review_current_receipts(text[]); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_live_review_current_receipts(p_video_ids text[]) RETURNS TABLE(video_id text, reviewed_at timestamp with time zone)
    LANGUAGE sql STABLE
    AS $$
WITH receipts AS MATERIALIZED (
    SELECT receipt.video_id,receipt.recorded_at,receipt.facts
    FROM public.youtube_live_review_receipt_facts receipt
    WHERE receipt.video_id = ANY(p_video_ids)
), current_input AS MATERIALIZED (
    -- SubLink를 함수 인수에서 분리해 typed 사실 조회도 호출 문장 안에서 인라인되게 합니다.
    SELECT ARRAY(SELECT DISTINCT receipt.video_id FROM receipts receipt) AS video_ids
), current_facts AS MATERIALIZED (
    SELECT current.video_id, current.facts, current.reviewable
    FROM current_input input
    CROSS JOIN LATERAL public.youtube_live_review_current_facts(input.video_ids) current
)
SELECT current_facts.video_id,max(receipt.recorded_at)
FROM current_facts
JOIN receipts receipt ON receipt.video_id = current_facts.video_id AND receipt.facts = current_facts.facts
WHERE current_facts.reviewable
GROUP BY current_facts.video_id;
$$;


--
-- Name: youtube_live_review_receipt_semantic_facts(jsonb); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_live_review_receipt_semantic_facts(p_snapshot jsonb) RETURNS jsonb
    LANGUAGE sql STABLE
    AS $$
WITH receipt_snapshot AS MATERIALIZED (
    SELECT CASE WHEN jsonb_typeof(p_snapshot->'head') = 'object'
                THEN jsonb_set(p_snapshot, '{head}',
                    (p_snapshot->'head') - 'ignored_absence_scheduled_for')
                ELSE p_snapshot
           END AS snapshot
)
SELECT public.youtube_live_review_semantic_facts(snapshot) FROM receipt_snapshot;
$$;


--
-- Name: youtube_live_review_receipt_value(jsonb); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_live_review_receipt_value(p_facts jsonb) RETURNS public.youtube_live_review_value
    LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
    AS $$
DECLARE
    decoded public.youtube_live_review_value;
BEGIN
    IF jsonb_typeof(p_facts)<>'object' THEN
        RETURN NULL;
    END IF;
    IF NOT jsonb_populate_record_valid(NULL::public.youtube_live_review_value,p_facts) THEN
        RETURN NULL;
    END IF;
    decoded := jsonb_populate_record(NULL::public.youtube_live_review_value,p_facts);
    IF to_jsonb(decoded)=p_facts THEN
        RETURN decoded;
    END IF;
    RETURN NULL;
END
$$;


--
-- Name: youtube_live_review_semantic_facts(jsonb); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_live_review_semantic_facts(p_snapshot jsonb) RETURNS jsonb
    LANGUAGE sql STABLE
    AS $$
    SELECT jsonb_build_object(
        'session', CASE WHEN jsonb_typeof(p_snapshot->'session') = 'object' THEN jsonb_build_object(
            'video_id', p_snapshot#>'{session,video_id}',
            'channel_id', p_snapshot#>'{session,channel_id}',
            'status', p_snapshot#>'{session,status}',
            'lifecycle_origin', p_snapshot#>'{session,lifecycle_origin}',
            'title', p_snapshot#>'{session,title}',
            'topic_id', p_snapshot#>'{session,topic_id}',
            'is_premiere', p_snapshot#>'{session,is_premiere}',
            'scheduled_start_time', extract(epoch FROM (p_snapshot#>>'{session,scheduled_start_time}')::timestamptz),
            'started_at', extract(epoch FROM (p_snapshot#>>'{session,started_at}')::timestamptz),
            'ended_at', extract(epoch FROM (p_snapshot#>>'{session,ended_at}')::timestamptz),
            'live_first_seen_at', extract(epoch FROM (p_snapshot#>>'{session,live_first_seen_at}')::timestamptz)) END,
        'head', CASE WHEN jsonb_typeof(p_snapshot->'head') = 'object' THEN jsonb_build_object(
            'status', p_snapshot#>'{head,status}',
            'last_upcoming_positive_at', extract(epoch FROM (p_snapshot#>>'{head,last_upcoming_positive_at}')::timestamptz),
            'last_upcoming_positive_seen_at', extract(epoch FROM (p_snapshot#>>'{head,last_upcoming_positive_seen_at}')::timestamptz),
            'last_live_positive_at', extract(epoch FROM (p_snapshot#>>'{head,last_live_positive_at}')::timestamptz),
            'last_live_positive_seen_at', extract(epoch FROM (p_snapshot#>>'{head,last_live_positive_seen_at}')::timestamptz),
            'last_end_evidence_at', extract(epoch FROM (p_snapshot#>>'{head,last_end_evidence_at}')::timestamptz),
            'last_complete_absence_at', extract(epoch FROM (p_snapshot#>>'{head,last_complete_absence_at}')::timestamptz),
            'last_absence_scheduled_for', extract(epoch FROM (p_snapshot#>>'{head,last_absence_scheduled_for}')::timestamptz),
            'first_absence_scheduled_for', extract(epoch FROM (p_snapshot#>>'{head,first_absence_scheduled_for}')::timestamptz),
            'second_absence_scheduled_for', extract(epoch FROM (p_snapshot#>>'{head,second_absence_scheduled_for}')::timestamptz),
            'consecutive_absence_slots', p_snapshot#>'{head,consecutive_absence_slots}',
            'last_absence_observation_id', p_snapshot#>'{head,last_absence_observation_id}',
            'end_candidate_kind', p_snapshot#>'{head,end_candidate_kind}',
            'end_candidate_observation_id', p_snapshot#>'{head,end_candidate_observation_id}',
            'ended_at', extract(epoch FROM (p_snapshot#>>'{head,ended_at}')::timestamptz),
            'end_reason', p_snapshot#>'{head,end_reason}') END,
        'pending', CASE WHEN jsonb_typeof(p_snapshot->'pending') = 'object' THEN jsonb_build_object(
            'channel_id', p_snapshot#>'{pending,channel_id}',
            'kind', p_snapshot#>'{pending,kind}',
            'observation_id', p_snapshot#>'{pending,observation_id}',
            'effective_at', extract(epoch FROM (p_snapshot#>>'{pending,effective_at}')::timestamptz),
            'scheduled_for', extract(epoch FROM (p_snapshot#>>'{pending,scheduled_for}')::timestamptz),
            'ended_at', extract(epoch FROM (p_snapshot#>>'{pending,ended_at}')::timestamptz),
            'negative_eligible', p_snapshot#>'{pending,negative_eligible}',
            'scope_covers', p_snapshot#>'{pending,scope_covers}') END,
        -- 246의 동명 column/alias 때문에 기존 영수증에는 가용성 판정 문자열만 저장됐다.
        -- 신규 전체 row에서도 같은 수명 사실인 판정을 비교하며 진단 메타데이터로 과거 결정을 추정하지 않는다.
        'availability', CASE jsonb_typeof(p_snapshot->'availability')
            WHEN 'object' THEN p_snapshot#>'{availability,availability}'
            WHEN 'string' THEN p_snapshot->'availability'
        END)
$$;


--
-- Name: youtube_live_review_snapshot(text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_live_review_snapshot(p_video_id text) RETURNS TABLE(original_snapshot jsonb, snapshot_sha256 text, evidence_refs jsonb, reviewable boolean)
    LANGUAGE sql STABLE
    AS $$
    WITH facts AS (
        SELECT jsonb_build_object('session',to_jsonb(session),'head',to_jsonb(head),
                   'pending',to_jsonb(pending),'availability',to_jsonb(availability.*)) AS snapshot,
               jsonb_build_object('availability_observation_id',availability.observation_id,
                   'availability_evidence_sha256',availability.evidence_sha256,
                   'pending_observation_id',pending.observation_id) AS refs,
               CASE WHEN head.video_id IS NOT NULL THEN jsonb_build_object(
                   'count',cardinality(head.ignored_absence_scheduled_for),
                   'sha256',encode(sha256(convert_to(to_jsonb(head.ignored_absence_scheduled_for)::TEXT,'UTF8')),'hex'))
               END AS ignored_absence_summary,
               session.status = 'UPCOMING'
                   AND (head.video_id IS NULL OR head.status = session.status)
                   AND (session.lifecycle_origin <> 'observed' OR head.video_id IS NOT NULL)
                   -- 가용성 PUBLIC도 수명 미상일 수 있다. 현재 확인보다 새롭거나
                   -- 같은 positive가 있으면 확인된 UPCOMING을 unresolved로 닫지 않는다.
                   AND availability.video_id IS NOT NULL
                   AND NOT COALESCE(GREATEST(head.last_upcoming_positive_at,head.last_live_positive_at)
                       >= availability.effective_at,false) AS reviewable
        FROM public.youtube_live_sessions session
        LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)
        LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)
        LEFT JOIN public.youtube_video_availability availability USING (video_id)
        WHERE session.video_id = p_video_id
    )
    SELECT CASE WHEN ignored_absence_summary IS NULL THEN snapshot
                ELSE jsonb_set(snapshot,'{head}',
                    ((snapshot->'head') - 'ignored_absence_scheduled_for')
                    || jsonb_build_object('ignored_absence_summary',ignored_absence_summary))
           END,
           encode(sha256(convert_to(snapshot::TEXT,'UTF8')),'hex'), refs,
           COALESCE(reviewable,false) FROM facts
$$;


--
-- Name: youtube_live_review_source_value(text, text, text, text, text, text, boolean, timestamp with time zone, timestamp with time zone, timestamp with time zone, timestamp with time zone, text, text, timestamp with time zone, timestamp with time zone, timestamp with time zone, timestamp with time zone, timestamp with time zone, timestamp with time zone, timestamp with time zone, timestamp with time zone, timestamp with time zone, smallint, bigint, text, bigint, timestamp with time zone, text, text, text, text, bigint, timestamp with time zone, timestamp with time zone, timestamp with time zone, boolean, boolean, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_live_review_source_value(p_session_video_id text, p_session_channel_id text, p_session_status text, p_session_lifecycle_origin text, p_session_title text, p_session_topic_id text, p_session_is_premiere boolean, p_session_scheduled_start_time timestamp with time zone, p_session_started_at timestamp with time zone, p_session_ended_at timestamp with time zone, p_session_live_first_seen_at timestamp with time zone, p_head_video_id text, p_head_status text, p_head_last_upcoming_positive_at timestamp with time zone, p_head_last_upcoming_positive_seen_at timestamp with time zone, p_head_last_live_positive_at timestamp with time zone, p_head_last_live_positive_seen_at timestamp with time zone, p_head_last_end_evidence_at timestamp with time zone, p_head_last_complete_absence_at timestamp with time zone, p_head_last_absence_scheduled_for timestamp with time zone, p_head_first_absence_scheduled_for timestamp with time zone, p_head_second_absence_scheduled_for timestamp with time zone, p_head_consecutive_absence_slots smallint, p_head_last_absence_observation_id bigint, p_head_end_candidate_kind text, p_head_end_candidate_observation_id bigint, p_head_ended_at timestamp with time zone, p_head_end_reason text, p_pending_video_id text, p_pending_channel_id text, p_pending_kind text, p_pending_observation_id bigint, p_pending_effective_at timestamp with time zone, p_pending_scheduled_for timestamp with time zone, p_pending_ended_at timestamp with time zone, p_pending_negative_eligible boolean, p_pending_scope_covers boolean, p_availability_availability text) RETURNS public.youtube_live_review_value
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$
SELECT ROW(
    ROW(
        p_session_video_id,
        p_session_channel_id,
        p_session_status,
        p_session_lifecycle_origin,
        p_session_title,
        p_session_topic_id,
        p_session_is_premiere,
        extract(epoch FROM p_session_scheduled_start_time),
        extract(epoch FROM p_session_started_at),
        extract(epoch FROM p_session_ended_at),
        extract(epoch FROM p_session_live_first_seen_at))::public.youtube_live_review_session_value,
    CASE WHEN p_head_video_id IS NOT NULL THEN ROW(
        p_head_status,
        extract(epoch FROM p_head_last_upcoming_positive_at),
        extract(epoch FROM p_head_last_upcoming_positive_seen_at),
        extract(epoch FROM p_head_last_live_positive_at),
        extract(epoch FROM p_head_last_live_positive_seen_at),
        extract(epoch FROM p_head_last_end_evidence_at),
        extract(epoch FROM p_head_last_complete_absence_at),
        extract(epoch FROM p_head_last_absence_scheduled_for),
        extract(epoch FROM p_head_first_absence_scheduled_for),
        extract(epoch FROM p_head_second_absence_scheduled_for),
        p_head_consecutive_absence_slots,
        p_head_last_absence_observation_id,
        p_head_end_candidate_kind,
        p_head_end_candidate_observation_id,
        extract(epoch FROM p_head_ended_at),
        p_head_end_reason)::public.youtube_live_review_head_value END,
    CASE WHEN p_pending_video_id IS NOT NULL THEN ROW(
        p_pending_channel_id,
        p_pending_kind,
        p_pending_observation_id,
        extract(epoch FROM p_pending_effective_at),
        extract(epoch FROM p_pending_scheduled_for),
        extract(epoch FROM p_pending_ended_at),
        p_pending_negative_eligible,
        p_pending_scope_covers)::public.youtube_live_review_pending_value END,
    p_availability_availability)::public.youtube_live_review_value;
$$;


--
-- Name: youtube_schedule_collabo_talent_names_valid(text[]); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.youtube_schedule_collabo_talent_names_valid(names text[]) RETURNS boolean
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    SET search_path TO 'pg_catalog'
    AS $$
    SELECT COALESCE(pg_catalog.array_ndims(names), 1) = 1
       AND COALESCE(pg_catalog.array_lower(names, 1), 1) = 1
       AND pg_catalog.cardinality(names) <= 32
       AND NOT EXISTS (
           SELECT 1
           FROM pg_catalog.unnest(names) AS name
           WHERE name IS NULL
              OR pg_catalog.octet_length(name) < 1
              OR pg_catalog.octet_length(name) > 256
       );
$$;




--
-- Name: acl_rooms; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.acl_rooms (
    id integer NOT NULL,
    room_id character varying(100) NOT NULL,
    list_type character varying(16) DEFAULT 'whitelist'::character varying NOT NULL,
    CONSTRAINT chk_acl_rooms_list_type_vocab CHECK (((list_type)::text = ANY ((ARRAY['whitelist'::character varying, 'blacklist'::character varying])::text[]))),
    CONSTRAINT chk_acl_rooms_room_id_chat_id CHECK (
CASE
    WHEN ((room_id)::text ~ '^(0|-?[1-9][0-9]{0,18})$'::text) THEN (((room_id)::numeric >= ('-9223372036854775808'::bigint)::numeric) AND ((room_id)::numeric <= ('9223372036854775807'::bigint)::numeric))
    ELSE false
END)
);


--
-- Name: acl_rooms_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.acl_rooms_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: acl_rooms_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.acl_rooms_id_seq OWNED BY public.acl_rooms.id;


--
-- Name: acl_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.acl_settings (
    id bigint NOT NULL,
    key character varying(64) NOT NULL,
    value text
);


--
-- Name: acl_settings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.acl_settings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: acl_settings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.acl_settings_id_seq OWNED BY public.acl_settings.id;


--
-- Name: alarm_dispatch_admin_actions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm_dispatch_admin_actions (
    id bigint NOT NULL,
    delivery_id bigint,
    action text NOT NULL,
    operator_id text NOT NULL,
    reason text NOT NULL,
    from_status text DEFAULT ''::text NOT NULL,
    to_status text DEFAULT ''::text NOT NULL,
    duplicate_risk_ack boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT alarm_dispatch_admin_actions_action_check CHECK (((length(action) > 0) AND (length(action) <= 128))),
    CONSTRAINT alarm_dispatch_admin_actions_operator_check CHECK (((length(operator_id) > 0) AND (length(operator_id) <= 128))),
    CONSTRAINT alarm_dispatch_admin_actions_reason_check CHECK (((length(reason) > 0) AND (length(reason) <= 1024)))
);


--
-- Name: alarm_dispatch_admin_actions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.alarm_dispatch_admin_actions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: alarm_dispatch_admin_actions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.alarm_dispatch_admin_actions_id_seq OWNED BY public.alarm_dispatch_admin_actions.id;


--
-- Name: alarm_dispatch_closeout_receipts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm_dispatch_closeout_receipts (
    receipt_id uuid NOT NULL,
    send_unit_id bigint NOT NULL,
    addressed_delivery_id bigint NOT NULL,
    target_ids bigint[] NOT NULL,
    target_revisions jsonb NOT NULL,
    status_metadata jsonb NOT NULL,
    original_sha256 text NOT NULL,
    operator_id text NOT NULL,
    reason text NOT NULL,
    disposition text DEFAULT 'closed_without_replay'::text NOT NULL,
    recorded_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT alarm_dispatch_closeout_receipts_digest_check CHECK ((original_sha256 ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT alarm_dispatch_closeout_receipts_disposition_check CHECK ((disposition = 'closed_without_replay'::text)),
    CONSTRAINT alarm_dispatch_closeout_receipts_operator_check CHECK ((((length(operator_id) >= 1) AND (length(operator_id) <= 128)) AND (operator_id = btrim(operator_id)) AND (operator_id !~ '[[:cntrl:]]'::text))),
    CONSTRAINT alarm_dispatch_closeout_receipts_reason_check CHECK ((((length(reason) >= 1) AND (length(reason) <= 1024)) AND (reason = btrim(reason)) AND (reason !~ '[[:cntrl:]]'::text))),
    CONSTRAINT alarm_dispatch_closeout_receipts_targets_check CHECK ((((cardinality(target_ids) >= 1) AND (cardinality(target_ids) <= 100)) AND (jsonb_array_length(target_revisions) = cardinality(target_ids)) AND (jsonb_array_length(status_metadata) = cardinality(target_ids))))
);


--
-- Name: alarm_dispatch_deliveries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm_dispatch_deliveries (
    id bigint NOT NULL,
    event_id bigint NOT NULL,
    room_id character varying(100) NOT NULL,
    dedupe_key text NOT NULL,
    claim_keys text[] DEFAULT ARRAY[]::text[] NOT NULL,
    delivery_context jsonb DEFAULT '{}'::jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_by text,
    locked_at timestamp with time zone,
    lock_expires_at timestamp with time zone,
    sending_started_at timestamp with time zone,
    sent_at timestamp with time zone,
    dlq_at timestamp with time zone,
    quarantined_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    last_error_code text DEFAULT ''::text NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    dispatch_group_key text,
    send_unit_id bigint NOT NULL,
    CONSTRAINT alarm_dispatch_deliveries_attempt_check CHECK ((attempt_count >= 0)),
    CONSTRAINT alarm_dispatch_deliveries_dedupe_key_check CHECK (((length(dedupe_key) > 0) AND (length(dedupe_key) <= 768))),
    CONSTRAINT alarm_dispatch_deliveries_dispatch_group_key_check CHECK (((dispatch_group_key IS NULL) OR ((length(dispatch_group_key) > 0) AND (length(dispatch_group_key) <= 768)))),
    CONSTRAINT alarm_dispatch_deliveries_room_id_check CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100))),
    CONSTRAINT alarm_dispatch_deliveries_send_unit_pair_check CHECK (((dispatch_group_key IS NULL) = (send_unit_id IS NULL))),
    CONSTRAINT alarm_dispatch_deliveries_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'retry'::text, 'leased'::text, 'sending'::text, 'sent'::text, 'dlq'::text, 'quarantined'::text, 'cancelled'::text]))),
    CONSTRAINT chk_alarm_dispatch_deliveries_last_error_size CHECK ((octet_length(last_error) <= 8192)),
    CONSTRAINT chk_alarm_dispatch_deliveries_state_shape CHECK ((((status <> 'leased'::text) OR ((locked_by IS NOT NULL) AND (locked_at IS NOT NULL) AND (lock_expires_at IS NOT NULL))) AND ((status <> 'sending'::text) OR ((locked_by IS NOT NULL) AND (locked_at IS NOT NULL) AND (lock_expires_at IS NOT NULL) AND (sending_started_at IS NOT NULL))) AND ((status <> 'sent'::text) OR (sent_at IS NOT NULL)) AND ((status <> 'dlq'::text) OR (dlq_at IS NOT NULL)) AND ((status <> 'quarantined'::text) OR (quarantined_at IS NOT NULL)) AND ((status <> 'cancelled'::text) OR (cancelled_at IS NOT NULL))))
)
WITH (autovacuum_vacuum_scale_factor='0.02', autovacuum_vacuum_threshold='50', autovacuum_analyze_scale_factor='0.02', autovacuum_analyze_threshold='50');


--
-- Name: COLUMN alarm_dispatch_deliveries.dispatch_group_key; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.alarm_dispatch_deliveries.dispatch_group_key IS 'Canonical grouping boundary assigned before claim; NULL only for pre-migration rows.';


--
-- Name: COLUMN alarm_dispatch_deliveries.send_unit_id; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.alarm_dispatch_deliveries.send_unit_id IS 'Persisted external-send identity; retries retain the same client_request_id through this unit.';


--
-- Name: alarm_dispatch_deliveries_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.alarm_dispatch_deliveries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: alarm_dispatch_deliveries_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.alarm_dispatch_deliveries_id_seq OWNED BY public.alarm_dispatch_deliveries.id;


--
-- Name: alarm_dispatch_event_collisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm_dispatch_event_collisions (
    id bigint NOT NULL,
    existing_event_id bigint,
    event_key text NOT NULL,
    existing_payload_hash character(64) NOT NULL,
    incoming_payload_hash character(64) NOT NULL,
    alarm_type public.alarm_type NOT NULL,
    channel_id character varying(64) DEFAULT ''::character varying NOT NULL,
    stream_id character varying(64) DEFAULT ''::character varying NOT NULL,
    category text DEFAULT ''::text NOT NULL,
    payload_schema_version smallint DEFAULT 1 NOT NULL,
    payload jsonb NOT NULL,
    status text DEFAULT 'detected'::text NOT NULL,
    last_error text DEFAULT 'event_key payload_hash conflict'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT alarm_dispatch_event_collisions_event_key_check CHECK (((length(event_key) > 0) AND (length(event_key) <= 512))),
    CONSTRAINT alarm_dispatch_event_collisions_existing_payload_hash_check CHECK ((existing_payload_hash ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT alarm_dispatch_event_collisions_incoming_payload_hash_check CHECK ((incoming_payload_hash ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT alarm_dispatch_event_collisions_payload_room_agnostic_check CHECK (((NOT (payload ? 'room_id'::text)) AND (NOT (payload ? 'roomId'::text)) AND (NOT (payload ? 'room'::text)) AND (NOT (payload ? 'users'::text)) AND (NOT ((payload -> 'notification'::text) ? 'room_id'::text)) AND (NOT ((payload -> 'notification'::text) ? 'roomId'::text)) AND (NOT ((payload -> 'notification'::text) ? 'room'::text)) AND (NOT ((payload -> 'notification'::text) ? 'users'::text)))),
    CONSTRAINT alarm_dispatch_event_collisions_status_check CHECK ((status = ANY (ARRAY['detected'::text, 'acknowledged'::text, 'resolved'::text])))
);


--
-- Name: alarm_dispatch_event_collisions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.alarm_dispatch_event_collisions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: alarm_dispatch_event_collisions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.alarm_dispatch_event_collisions_id_seq OWNED BY public.alarm_dispatch_event_collisions.id;


--
-- Name: alarm_dispatch_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm_dispatch_events (
    id bigint NOT NULL,
    event_key text NOT NULL,
    payload_hash character(64) NOT NULL,
    alarm_type public.alarm_type NOT NULL,
    channel_id character varying(64) DEFAULT ''::character varying NOT NULL,
    stream_id character varying(64) DEFAULT ''::character varying NOT NULL,
    category text DEFAULT ''::text NOT NULL,
    payload_schema_version smallint DEFAULT 1 NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT alarm_dispatch_events_event_key_check CHECK (((length(event_key) > 0) AND (length(event_key) <= 512))),
    CONSTRAINT alarm_dispatch_events_payload_hash_check CHECK ((payload_hash ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT alarm_dispatch_events_payload_notification_room_agnostic_check CHECK (((NOT (payload ? 'room_id'::text)) AND (NOT (payload ? 'roomId'::text)) AND (NOT (payload ? 'room'::text)) AND (NOT (payload ? 'users'::text)) AND (NOT ((payload -> 'notification'::text) ? 'room_id'::text)) AND (NOT ((payload -> 'notification'::text) ? 'roomId'::text)) AND (NOT ((payload -> 'notification'::text) ? 'room'::text)) AND (NOT ((payload -> 'notification'::text) ? 'users'::text))))
);
ALTER TABLE ONLY public.alarm_dispatch_events ALTER COLUMN payload SET COMPRESSION lz4;


--
-- Name: alarm_dispatch_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.alarm_dispatch_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: alarm_dispatch_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.alarm_dispatch_events_id_seq OWNED BY public.alarm_dispatch_events.id;


--
-- Name: alarm_dispatch_send_units; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm_dispatch_send_units (
    id bigint NOT NULL,
    unit_key character(64) NOT NULL,
    dispatch_group_key text NOT NULL,
    room_id character varying(100) NOT NULL,
    client_request_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    request_body text,
    request_route text,
    request_body_hash text,
    request_delivery_ids bigint[],
    base_client_request_id text,
    request_generation integer DEFAULT 0 NOT NULL,
    handoff_request_id text,
    handoff_next_check_at timestamp with time zone,
    handoff_checked_at timestamp with time zone,
    CONSTRAINT alarm_dispatch_send_units_client_request_id_check CHECK ((client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'::text)),
    CONSTRAINT alarm_dispatch_send_units_group_key_check CHECK (((length(dispatch_group_key) > 0) AND (length(dispatch_group_key) <= 768))),
    CONSTRAINT alarm_dispatch_send_units_room_id_check CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100))),
    CONSTRAINT alarm_dispatch_send_units_unit_key_check CHECK ((unit_key ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT chk_alarm_send_request_shape CHECK ((((request_generation >= 0) AND (request_generation <= 2)) AND (((request_body IS NULL) AND (request_route IS NULL) AND (request_body_hash IS NULL) AND (request_delivery_ids IS NULL) AND (base_client_request_id IS NULL) AND (request_generation = 0)) OR ((request_body IS NOT NULL) AND (request_route IS NOT NULL) AND (request_route = ANY (ARRAY['text'::text, 'markdown'::text, 'richtext'::text])) AND (request_body_hash IS NOT NULL) AND (request_body_hash ~ '^[0-9a-f]{64}$'::text) AND (request_delivery_ids IS NOT NULL) AND ((cardinality(request_delivery_ids) >= 1) AND (cardinality(request_delivery_ids) <= 10)) AND (base_client_request_id IS NOT NULL) AND (base_client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'::text))))),
    CONSTRAINT chk_alarm_send_unit_handoff_shape CHECK ((((handoff_request_id IS NULL) OR ((length(handoff_request_id) >= 1) AND (length(handoff_request_id) <= 256))) AND ((handoff_request_id IS NULL) OR (request_body_hash IS NOT NULL)) AND ((handoff_next_check_at IS NULL) OR ((handoff_request_id IS NOT NULL) AND (handoff_checked_at IS NULL))) AND ((handoff_checked_at IS NULL) OR (handoff_request_id IS NOT NULL))))
);


--
-- Name: TABLE alarm_dispatch_send_units; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.alarm_dispatch_send_units IS 'Immutable alarm dispatch send identity shared by all deliveries in one external request.';


--
-- Name: alarm_dispatch_send_units_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.alarm_dispatch_send_units_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: alarm_dispatch_send_units_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.alarm_dispatch_send_units_id_seq OWNED BY public.alarm_dispatch_send_units.id;


--
-- Name: alarm_room_display_names; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm_room_display_names (
    room_id character varying(100) NOT NULL,
    display_name character varying(255) NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_alarm_room_display_names_display_name_nonblank CHECK ((btrim((display_name)::text) <> ''::text))
);


--
-- Name: alarm_upcoming_candidates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarm_upcoming_candidates (
    dedupe_key text NOT NULL,
    event_key text NOT NULL,
    payload_hash text NOT NULL,
    channel_id character varying(64) NOT NULL,
    stream_id text NOT NULL,
    room_id character varying(100) NOT NULL,
    scheduled_at timestamp with time zone NOT NULL,
    notification jsonb NOT NULL,
    selected_at timestamp with time zone NOT NULL,
    checked_at timestamp with time zone NOT NULL,
    terminal_at timestamp with time zone,
    outcome text DEFAULT 'pending'::text NOT NULL,
    CONSTRAINT chk_alarm_upcoming_candidates_outcome_vocab CHECK ((outcome = ANY (ARRAY['pending'::text, 'accepted'::text, 'rejected_collision'::text, 'rejected_terminal'::text, 'expired'::text, 'schedule_changed'::text, 'stream_ended'::text, 'subscription_removed'::text]))),
    CONSTRAINT chk_alarm_upcoming_candidates_terminal CHECK (((outcome = 'pending'::text) = (terminal_at IS NULL)))
);
ALTER TABLE ONLY public.alarm_upcoming_candidates ALTER COLUMN notification SET COMPRESSION lz4;


--
-- Name: alarms; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarms (
    id integer NOT NULL,
    room_id character varying(100) NOT NULL,
    user_id character varying(64) NOT NULL,
    channel_id character varying(64) NOT NULL,
    room_name character varying(255),
    user_name character varying(200),
    created_at timestamp with time zone DEFAULT now(),
    alarm_types public.alarm_type[] DEFAULT ARRAY['LIVE'::public.alarm_type] NOT NULL,
    host_id text DEFAULT ''::text NOT NULL,
    room_name_updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_alarms_host_id_vocab CHECK (((host_id = ''::text) OR (((channel_id)::text = 'UC3OH5FKQ3qtl4uRme_vZTgA'::text) AND (host_id = ANY (ARRAY['kiyosumi-lyra'::text, 'reimei-mira'::text, 'yoinagi-neon'::text])))))
);


--
-- Name: alarms_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.alarms_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: alarms_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.alarms_id_seq OWNED BY public.alarms.id;


--
-- Name: bot_command_executions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_command_executions (
    id bigint NOT NULL,
    message_id text NOT NULL,
    command_kind text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'claimed'::text NOT NULL,
    claim_token text NOT NULL,
    result_summary text DEFAULT ''::text NOT NULL,
    claimed_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_bot_command_executions_claim_token_size CHECK (((length(claim_token) > 0) AND (length(claim_token) <= 256))),
    CONSTRAINT chk_bot_command_executions_command_kind_size CHECK ((length(command_kind) <= 128)),
    CONSTRAINT chk_bot_command_executions_message_id_size CHECK (((length(message_id) > 0) AND (length(message_id) <= 512))),
    CONSTRAINT chk_bot_command_executions_result_summary_size CHECK ((octet_length(result_summary) <= 2048)),
    CONSTRAINT chk_bot_command_executions_state_shape CHECK (((status = 'claimed'::text) OR (completed_at IS NOT NULL))),
    CONSTRAINT chk_bot_command_executions_status_vocab CHECK ((status = ANY (ARRAY['claimed'::text, 'succeeded'::text, 'failed'::text, 'outcome_unknown'::text]))),
    CONSTRAINT chk_bot_command_executions_terminal_summary_scrubbed CHECK (((status <> ALL (ARRAY['succeeded'::text, 'failed'::text, 'outcome_unknown'::text])) OR (result_summary = status)))
);


--
-- Name: bot_command_executions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_command_executions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: bot_command_executions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_command_executions_id_seq OWNED BY public.bot_command_executions.id;


--
-- Name: bot_reply_outbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_reply_outbox (
    id bigint NOT NULL,
    message_id text NOT NULL,
    phase text NOT NULL,
    ordinal bigint NOT NULL,
    room_id text NOT NULL,
    payload jsonb,
    payload_hash character(64) NOT NULL,
    client_request_id text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    first_attempt_at timestamp with time zone,
    iris_request_id text DEFAULT ''::text NOT NULL,
    claim_token text,
    lease_until timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    operator_replay_grants integer DEFAULT 0 NOT NULL,
    accepted_at timestamp with time zone,
    handoff_next_check_at timestamp with time zone,
    CONSTRAINT chk_bot_reply_outbox_attempts CHECK ((attempts >= 0)),
    CONSTRAINT chk_bot_reply_outbox_client_request_id CHECK ((client_request_id ~ '^[A-Za-z0-9._:-]{8,160}$'::text)),
    CONSTRAINT chk_bot_reply_outbox_iris_request_id_size CHECK ((length(iris_request_id) <= 256)),
    CONSTRAINT chk_bot_reply_outbox_last_error_size CHECK ((octet_length(last_error) <= 8192)),
    CONSTRAINT chk_bot_reply_outbox_message_id_size CHECK (((length(message_id) > 0) AND (length(message_id) <= 512))),
    CONSTRAINT chk_bot_reply_outbox_operator_replay_grants CHECK ((operator_replay_grants >= 0)),
    CONSTRAINT chk_bot_reply_outbox_ordinal CHECK ((ordinal >= 0)),
    CONSTRAINT chk_bot_reply_outbox_payload_hash CHECK ((payload_hash ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT chk_bot_reply_outbox_phase_size CHECK (((length(phase) > 0) AND (length(phase) <= 32))),
    CONSTRAINT chk_bot_reply_outbox_room_id_size CHECK (((length(room_id) > 0) AND (length(room_id) <= 256))),
    CONSTRAINT chk_bot_reply_outbox_state_shape CHECK ((((status <> 'submitting'::text) OR ((claim_token IS NOT NULL) AND (lease_until IS NOT NULL) AND (first_attempt_at IS NOT NULL))) AND ((status <> 'accepted'::text) OR ((length(iris_request_id) > 0) AND (claim_token IS NULL) AND (lease_until IS NULL) AND (accepted_at IS NOT NULL) AND (handoff_next_check_at IS NOT NULL))) AND ((status = 'accepted'::text) OR (handoff_next_check_at IS NULL)) AND ((status = ANY (ARRAY['handoff_completed'::text, 'dead'::text, 'permanent_conflict'::text, 'discarded'::text])) OR (payload IS NOT NULL)))),
    CONSTRAINT chk_bot_reply_outbox_status_vocab CHECK ((status = ANY (ARRAY['pending'::text, 'submitting'::text, 'accepted'::text, 'handoff_completed'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text, 'dead'::text, 'permanent_conflict'::text, 'manual_review'::text, 'discarded'::text])))
);
ALTER TABLE ONLY public.bot_reply_outbox ALTER COLUMN payload SET COMPRESSION lz4;


--
-- Name: bot_reply_outbox_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_reply_outbox_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: bot_reply_outbox_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_reply_outbox_id_seq OWNED BY public.bot_reply_outbox.id;


--
-- Name: bot_reply_outbox_replay_audit; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_reply_outbox_replay_audit (
    id bigint NOT NULL,
    outbox_id bigint NOT NULL,
    grant_number integer NOT NULL,
    event_type text NOT NULL,
    actor text NOT NULL,
    reason text NOT NULL,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_bot_reply_outbox_replay_audit_actor CHECK ((actor ~ '^[A-Za-z0-9._:@-]{1,64}$'::text)),
    CONSTRAINT chk_bot_reply_outbox_replay_audit_event_type CHECK ((event_type = ANY (ARRAY['granted'::text, 'replayed'::text]))),
    CONSTRAINT chk_bot_reply_outbox_replay_audit_grant_number CHECK ((grant_number > 0)),
    CONSTRAINT chk_bot_reply_outbox_replay_audit_reason CHECK ((((octet_length(reason) >= 1) AND (octet_length(reason) <= 256)) AND (reason !~ '[[:cntrl:]]'::text)))
);


--
-- Name: bot_reply_outbox_replay_audit_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_reply_outbox_replay_audit_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: bot_reply_outbox_replay_audit_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_reply_outbox_replay_audit_id_seq OWNED BY public.bot_reply_outbox_replay_audit.id;


--
-- Name: bot_reply_outbox_resolution_audit; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_reply_outbox_resolution_audit (
    id bigint NOT NULL,
    outbox_id bigint NOT NULL,
    decision text NOT NULL,
    observed_iris_state text NOT NULL,
    actor text NOT NULL,
    reason text NOT NULL,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_bot_reply_outbox_resolution_audit_actor CHECK ((actor ~ '^[A-Za-z0-9._:@-]{1,64}$'::text)),
    CONSTRAINT chk_bot_reply_outbox_resolution_audit_decision CHECK ((decision = 'discarded_without_replay'::text)),
    CONSTRAINT chk_bot_reply_outbox_resolution_audit_iris_state CHECK ((observed_iris_state = ANY (ARRAY['queued'::text, 'preparing'::text, 'prepared'::text, 'sending'::text, 'handoff_completed'::text, 'failed'::text, 'outcome_unknown'::text, 'not_found'::text]))),
    CONSTRAINT chk_bot_reply_outbox_resolution_audit_reason CHECK ((((octet_length(reason) >= 1) AND (octet_length(reason) <= 256)) AND (reason !~ '[[:cntrl:]]'::text)))
);


--
-- Name: bot_reply_outbox_resolution_audit_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_reply_outbox_resolution_audit_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: bot_reply_outbox_resolution_audit_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_reply_outbox_resolution_audit_id_seq OWNED BY public.bot_reply_outbox_resolution_audit.id;


--
-- Name: bot_webhook_heads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_webhook_heads (
    ordering_key text NOT NULL,
    message_id text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_bot_webhook_heads_ordering_key_size CHECK (((length(ordering_key) > 0) AND (length(ordering_key) <= 512)))
);


--
-- Name: bot_webhook_inbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bot_webhook_inbox (
    id bigint NOT NULL,
    message_id text NOT NULL,
    room_id text NOT NULL,
    ordering_key text NOT NULL,
    payload jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    claim_token text,
    lease_until timestamp with time zone,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    terminal_reason text DEFAULT ''::text NOT NULL,
    terminal_at timestamp with time zone,
    last_error text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_bot_webhook_inbox_attempts CHECK ((attempts >= 0)),
    CONSTRAINT chk_bot_webhook_inbox_last_error_size CHECK ((octet_length(last_error) <= 8192)),
    CONSTRAINT chk_bot_webhook_inbox_message_id_size CHECK (((length(message_id) > 0) AND (length(message_id) <= 512))),
    CONSTRAINT chk_bot_webhook_inbox_ordering_key_size CHECK (((length(ordering_key) > 0) AND (length(ordering_key) <= 512))),
    CONSTRAINT chk_bot_webhook_inbox_room_id_size CHECK (((length(room_id) > 0) AND (length(room_id) <= 256))),
    CONSTRAINT chk_bot_webhook_inbox_state_shape CHECK ((((status <> 'processing'::text) OR ((claim_token IS NOT NULL) AND (lease_until IS NOT NULL))) AND ((status <> 'dead'::text) OR ((terminal_at IS NOT NULL) AND (length(terminal_reason) > 0))))),
    CONSTRAINT chk_bot_webhook_inbox_status_vocab CHECK ((status = ANY (ARRAY['pending'::text, 'processing'::text, 'retry'::text, 'dead'::text, 'succeeded'::text]))),
    CONSTRAINT chk_bot_webhook_inbox_terminal_payload_scrubbed CHECK (((status <> ALL (ARRAY['dead'::text, 'succeeded'::text])) OR (payload = '{}'::jsonb))),
    CONSTRAINT chk_bot_webhook_inbox_terminal_reason_size CHECK ((length(terminal_reason) <= 512))
);
ALTER TABLE ONLY public.bot_webhook_inbox ALTER COLUMN payload SET COMPRESSION lz4;


--
-- Name: bot_webhook_inbox_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.bot_webhook_inbox_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: bot_webhook_inbox_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.bot_webhook_inbox_id_seq OWNED BY public.bot_webhook_inbox.id;


--
-- Name: kakao_rooms; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.kakao_rooms (
    room_id character varying(100) NOT NULL,
    room_type character varying(64) DEFAULT ''::character varying NOT NULL,
    room_link_id character varying(128) DEFAULT ''::character varying NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT kakao_rooms_room_id_len CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100))),
    CONSTRAINT kakao_rooms_room_link_id_len CHECK ((length((room_link_id)::text) <= 128)),
    CONSTRAINT kakao_rooms_room_type_len CHECK ((length((room_type)::text) <= 64))
);


--
-- Name: major_event_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.major_event_subscriptions (
    id integer NOT NULL,
    room_id character varying(100) NOT NULL,
    room_name character varying(255),
    created_at timestamp with time zone DEFAULT now()
);


--
-- Name: major_event_subscriptions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.major_event_subscriptions_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: major_event_subscriptions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.major_event_subscriptions_id_seq OWNED BY public.major_event_subscriptions.id;


--
-- Name: major_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.major_events (
    id integer NOT NULL,
    external_id character varying(500) NOT NULL,
    type character varying(20) DEFAULT 'event'::character varying NOT NULL,
    title character varying(500) NOT NULL,
    link character varying(1000) NOT NULL,
    description text,
    members text[],
    pub_date timestamp with time zone,
    event_start_date date,
    event_end_date date,
    status text DEFAULT 'active'::character varying NOT NULL,
    notified_at timestamp with time zone,
    notified_week character varying(10),
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    notified_month character varying(10),
    link_status character varying(20) DEFAULT 'unchecked'::character varying NOT NULL,
    link_checked_at timestamp with time zone,
    CONSTRAINT chk_major_events_status_vocab CHECK ((status = ANY (ARRAY['active'::text, 'ended'::text, 'canceled'::text])))
);


--
-- Name: major_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.major_events_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: major_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.major_events_id_seq OWNED BY public.major_events.id;


--
-- Name: member_news_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.member_news_subscriptions (
    id integer NOT NULL,
    room_id character varying(100) NOT NULL,
    room_name character varying(255),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: member_news_subscriptions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.member_news_subscriptions_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: member_news_subscriptions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.member_news_subscriptions_id_seq OWNED BY public.member_news_subscriptions.id;


--
-- Name: members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.members (
    id integer NOT NULL,
    slug character varying(100) NOT NULL,
    channel_id character varying(64),
    english_name character varying(200) NOT NULL,
    japanese_name character varying(200),
    korean_name character varying(200),
    status text DEFAULT 'active'::character varying NOT NULL,
    is_graduated boolean DEFAULT false NOT NULL,
    aliases jsonb DEFAULT '{"ja": [], "ko": []}'::jsonb NOT NULL,
    photo text,
    photo_updated_at timestamp with time zone,
    org character varying(50) NOT NULL,
    suborg character varying(100),
    sync_source character varying(20) NOT NULL,
    chzzk_channel_id character varying(32),
    twitch_user_id character varying(50),
    short_korean_name character varying(64),
    birthday date,
    debut_date date,
    units text[] DEFAULT '{}'::text[] NOT NULL,
    official_link text,
    CONSTRAINT chk_members_aliases_shape CHECK (((jsonb_typeof(aliases) = 'object'::text) AND (aliases ? 'ko'::text) AND (aliases ? 'ja'::text) AND (jsonb_typeof((aliases -> 'ko'::text)) = 'array'::text) AND (jsonb_typeof((aliases -> 'ja'::text)) = 'array'::text))),
    CONSTRAINT chk_members_graduated_sync CHECK ((is_graduated = (status = 'graduated'::text))),
    CONSTRAINT chk_members_status_vocab CHECK ((status = ANY (ARRAY[('active'::character varying)::text, ('graduated'::character varying)::text])))
);


--
-- Name: COLUMN members.units; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.members.units IS '기수·유닛 표시명. 복수 소속을 보존하며 빈 배열은 미등록이다.';


--
-- Name: COLUMN members.official_link; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.members.official_link IS '확인된 공식 멤버 페이지 URL. 없으면 NULL이다.';


--
-- Name: members_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.members_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: members_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.members_id_seq OWNED BY public.members.id;


--
-- Name: message_strings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.message_strings (
    id bigint NOT NULL,
    namespace character varying(32) NOT NULL,
    key character varying(64) NOT NULL,
    value text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: message_strings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.message_strings_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: message_strings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.message_strings_id_seq OWNED BY public.message_strings.id;


--
-- Name: notification_delivery_outbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.notification_delivery_outbox (
    id bigint NOT NULL,
    kind text NOT NULL,
    period_key character varying(20) NOT NULL,
    room_id character varying(100) NOT NULL,
    content_id character varying(200) NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    status text DEFAULT 'PENDING'::character varying NOT NULL,
    attempt_count bigint DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_at timestamp with time zone,
    sent_at timestamp with time zone,
    error text,
    locked_by text,
    lock_expires_at timestamp with time zone,
    sending_started_at timestamp with time zone,
    handoff_request_id text,
    handoff_next_check_at timestamp with time zone,
    handoff_checked_at timestamp with time zone,
    CONSTRAINT chk_notification_delivery_outbox_handoff_shape CHECK ((((handoff_request_id IS NULL) OR ((length(handoff_request_id) >= 1) AND (length(handoff_request_id) <= 256))) AND ((handoff_next_check_at IS NULL) OR ((status = 'SENT'::text) AND (handoff_request_id IS NOT NULL) AND (handoff_checked_at IS NULL))) AND ((handoff_checked_at IS NULL) OR (handoff_request_id IS NOT NULL)) AND ((status <> 'SENT'::text) OR (handoff_request_id IS NULL) OR (handoff_checked_at IS NOT NULL) OR (handoff_next_check_at IS NOT NULL)))),
    CONSTRAINT chk_notification_delivery_outbox_kind_vocab CHECK ((kind = ANY (ARRAY['MAJOR_EVENT_WEEKLY'::text, 'MAJOR_EVENT_MONTHLY'::text, 'MEMBER_NEWS_WEEKLY'::text, 'MEMBER_NEWS_MONTHLY'::text]))),
    CONSTRAINT chk_notification_delivery_outbox_status_vocab CHECK ((status = ANY (ARRAY['PENDING'::text, 'SENDING'::text, 'SENT'::text, 'FAILED'::text, 'QUARANTINED'::text])))
)
WITH (autovacuum_vacuum_scale_factor='0.02', autovacuum_vacuum_threshold='50', autovacuum_analyze_scale_factor='0.02', autovacuum_analyze_threshold='50');


--
-- Name: notification_delivery_outbox_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.notification_delivery_outbox_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: notification_delivery_outbox_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.notification_delivery_outbox_id_seq OWNED BY public.notification_delivery_outbox.id;


--
-- Name: notification_template_revisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.notification_template_revisions (
    id bigint NOT NULL,
    template_id bigint NOT NULL,
    body text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: notification_template_revisions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.notification_template_revisions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: notification_template_revisions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.notification_template_revisions_id_seq OWNED BY public.notification_template_revisions.id;


--
-- Name: notification_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.notification_templates (
    id bigint NOT NULL,
    template_key character varying(50) NOT NULL,
    channel_id character varying(64),
    body text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version bigint DEFAULT 1 NOT NULL,
    format_variant text DEFAULT 'plain'::text NOT NULL,
    CONSTRAINT notification_templates_format_variant_check CHECK ((format_variant = ANY (ARRAY['plain'::text, 'richtext'::text])))
);


--
-- Name: notification_templates_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.notification_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: notification_templates_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.notification_templates_id_seq OWNED BY public.notification_templates.id;


--
-- Name: observation_contract_generations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.observation_contract_generations (
    provider text NOT NULL,
    observation_kind text NOT NULL,
    current_schema_version smallint CONSTRAINT observation_contract_generation_current_schema_version_not_null NOT NULL,
    current_generation bigint NOT NULL,
    updated_by text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_observation_contract_kind_vocab CHECK ((observation_kind = ANY (ARRAY['community_page'::text, 'video_list'::text, 'shorts_list'::text, 'live_snapshot'::text, 'channel_profile'::text, 'channel_photo'::text, 'schedule_snapshot'::text, 'channel_live_check'::text, 'video_live_check'::text]))),
    CONSTRAINT chk_observation_contract_provider_vocab CHECK ((provider = ANY (ARRAY['holodex'::text, 'youtubejs'::text, 'hololive_official'::text]))),
    CONSTRAINT chk_observation_contract_updated_by CHECK (((length(updated_by) >= 1) AND (length(updated_by) <= 128))),
    CONSTRAINT observation_contract_generations_current_generation_check CHECK ((current_generation > 0)),
    CONSTRAINT observation_contract_generations_current_schema_version_check CHECK ((current_schema_version > 0))
);


--
-- Name: TABLE observation_contract_generations; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.observation_contract_generations IS 'Current publish contract fence for each provider and observation kind.';


--
-- Name: source_collection_checkpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_collection_checkpoints (
    provider text NOT NULL,
    observation_kind text NOT NULL,
    subject_key text NOT NULL,
    scope_sha256 text NOT NULL,
    contract_generation bigint NOT NULL,
    last_observation_key text NOT NULL,
    last_evidence_sha256 text NOT NULL,
    last_scheduled_for timestamp with time zone NOT NULL,
    last_success_at timestamp with time zone NOT NULL,
    collection_latency_ms bigint NOT NULL,
    continuity text NOT NULL,
    cursor jsonb,
    last_error_code text,
    last_error_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_source_checkpoint_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(last_observation_key) >= 1) AND (length(last_observation_key) <= 512)) AND ((last_error_code IS NULL) OR ((length(last_error_code) >= 1) AND (length(last_error_code) <= 128))))),
    CONSTRAINT chk_source_checkpoint_continuity_vocab CHECK ((continuity = ANY (ARRAY['CONTIGUOUS'::text, 'GAP_UNRESOLVED'::text, 'NOT_APPLICABLE'::text]))),
    CONSTRAINT chk_source_checkpoint_cursor CHECK (((cursor IS NULL) OR ((jsonb_typeof(cursor) = 'object'::text) AND (octet_length((cursor)::text) <= 16384)))),
    CONSTRAINT chk_source_checkpoint_error_shape CHECK (((last_error_code IS NULL) = (last_error_at IS NULL))),
    CONSTRAINT chk_source_checkpoint_hashes CHECK (((scope_sha256 ~ '^[0-9a-f]{64}$'::text) AND (last_evidence_sha256 ~ '^[0-9a-f]{64}$'::text))),
    CONSTRAINT source_collection_checkpoints_collection_latency_ms_check CHECK ((collection_latency_ms >= 0)),
    CONSTRAINT source_collection_checkpoints_contract_generation_check CHECK ((contract_generation > 0))
);


--
-- Name: TABLE source_collection_checkpoints; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.source_collection_checkpoints IS 'Collector continuity and health metadata; not content deduplication authority.';


--
-- Name: source_observation_applications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_applications (
    id bigint NOT NULL,
    observation_id bigint NOT NULL,
    entity_key text NOT NULL,
    applied_at timestamp with time zone DEFAULT now() NOT NULL,
    entity_kind_id smallint NOT NULL,
    decision_id smallint NOT NULL,
    CONSTRAINT chk_source_observation_application_bounds CHECK (((length(entity_key) >= 1) AND (length(entity_key) <= 256)))
)
WITH (autovacuum_vacuum_scale_factor='0.05', autovacuum_vacuum_threshold='500');


--
-- Name: source_observation_applications_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.source_observation_applications_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: source_observation_applications_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.source_observation_applications_id_seq OWNED BY public.source_observation_applications.id;


--
-- Name: source_observation_collisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_collisions (
    id bigint NOT NULL,
    existing_observation_id bigint,
    provider text NOT NULL,
    observation_kind text NOT NULL,
    subject_key text NOT NULL,
    observation_key text NOT NULL,
    schema_version smallint NOT NULL,
    contract_generation bigint NOT NULL,
    existing_evidence_sha256 text NOT NULL,
    attempted_evidence_sha256 text CONSTRAINT source_observation_collision_attempted_evidence_sha256_not_null NOT NULL,
    attempted_payload_sha256 text NOT NULL,
    collector_instance text NOT NULL,
    job_key text NOT NULL,
    fence_epoch bigint NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_source_observation_collision_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(observation_key) >= 1) AND (length(observation_key) <= 512)) AND ((length(collector_instance) >= 1) AND (length(collector_instance) <= 128)) AND ((length(job_key) >= 1) AND (length(job_key) <= 512)))),
    CONSTRAINT chk_source_observation_collision_hashes CHECK (((existing_evidence_sha256 ~ '^[0-9a-f]{64}$'::text) AND (attempted_evidence_sha256 ~ '^[0-9a-f]{64}$'::text) AND (attempted_payload_sha256 ~ '^[0-9a-f]{64}$'::text))),
    CONSTRAINT source_observation_collisions_contract_generation_check CHECK ((contract_generation > 0)),
    CONSTRAINT source_observation_collisions_fence_epoch_check CHECK ((fence_epoch > 0)),
    CONSTRAINT source_observation_collisions_schema_version_check CHECK ((schema_version > 0))
);


--
-- Name: TABLE source_observation_collisions; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.source_observation_collisions IS 'Bounded audit for conflicting semantic evidence at one observation identity.';


--
-- Name: source_observation_collisions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.source_observation_collisions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: source_observation_collisions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.source_observation_collisions_id_seq OWNED BY public.source_observation_collisions.id;


--
-- Name: source_observation_consumer_offsets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_consumer_offsets (
    consumer_name text NOT NULL,
    observation_kind text NOT NULL,
    last_processed_id bigint DEFAULT 0 NOT NULL,
    last_effective_at timestamp with time zone,
    last_processed_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_source_observation_consumer_offset_bounds CHECK (((length(consumer_name) >= 1) AND (length(consumer_name) <= 128))),
    CONSTRAINT chk_source_observation_consumer_offset_kind_vocab CHECK ((observation_kind = ANY (ARRAY['community_page'::text, 'video_list'::text, 'shorts_list'::text, 'live_snapshot'::text, 'channel_profile'::text, 'channel_photo'::text, 'schedule_snapshot'::text, 'channel_live_check'::text, 'video_live_check'::text]))),
    CONSTRAINT source_observation_consumer_offsets_last_processed_id_check CHECK ((last_processed_id >= 0))
);


--
-- Name: source_observation_decisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_decisions (
    id smallint NOT NULL,
    name text NOT NULL,
    CONSTRAINT chk_source_observation_decision_bounds CHECK (((length(name) >= 1) AND (length(name) <= 128)))
);


--
-- Name: source_observation_decisions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.source_observation_decisions ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.source_observation_decisions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: source_observation_entity_kinds; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_entity_kinds (
    id smallint NOT NULL,
    name text NOT NULL,
    CONSTRAINT chk_source_observation_entity_kind_bounds CHECK (((length(name) >= 1) AND (length(name) <= 64)))
);


--
-- Name: source_observation_entity_kinds_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.source_observation_entity_kinds ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.source_observation_entity_kinds_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: source_observation_payload_gc_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_payload_gc_state (
    singleton boolean DEFAULT true NOT NULL,
    cursor_id bigint DEFAULT 0 NOT NULL,
    CONSTRAINT source_observation_payload_gc_state_cursor_id_check CHECK ((cursor_id >= 0)),
    CONSTRAINT source_observation_payload_gc_state_singleton_check CHECK (singleton)
);


--
-- Name: source_observation_payloads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_payloads (
    id bigint NOT NULL,
    observation_kind text NOT NULL,
    schema_version smallint NOT NULL,
    canonical_profile text NOT NULL,
    payload_sha256 bytea NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT source_observation_payloads_canonical_profile_check CHECK ((canonical_profile = 'source-observation-canonical-json-v1'::text)),
    CONSTRAINT source_observation_payloads_payload_check CHECK (((jsonb_typeof(payload) = 'object'::text) AND (octet_length((payload)::text) <= 1048576))),
    CONSTRAINT source_observation_payloads_payload_sha256_check CHECK ((octet_length(payload_sha256) = 32)),
    CONSTRAINT source_observation_payloads_schema_version_check CHECK ((schema_version > 0))
);
ALTER TABLE ONLY public.source_observation_payloads ALTER COLUMN payload SET COMPRESSION lz4;


--
-- Name: source_observation_payloads_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.source_observation_payloads ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.source_observation_payloads_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: source_observation_provenance_gc_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_provenance_gc_state (
    singleton boolean DEFAULT true NOT NULL,
    cursor_id integer DEFAULT 0 NOT NULL,
    CONSTRAINT source_observation_provenance_gc_state_cursor_id_check CHECK ((cursor_id >= 0)),
    CONSTRAINT source_observation_provenance_gc_state_singleton_check CHECK (singleton)
);


--
-- Name: source_observation_provenances; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_provenances (
    id integer NOT NULL,
    job_key text NOT NULL,
    collector_instance text NOT NULL,
    collection_job_kind text NOT NULL,
    CONSTRAINT chk_source_observation_provenance_bounds CHECK ((((length(job_key) >= 1) AND (length(job_key) <= 512)) AND ((length(collector_instance) >= 1) AND (length(collector_instance) <= 128)) AND ((length(collection_job_kind) >= 1) AND (length(collection_job_kind) <= 128))))
);


--
-- Name: source_observation_provenances_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.source_observation_provenances ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.source_observation_provenances_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: source_observation_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_queue (
    observation_id bigint NOT NULL,
    status text DEFAULT 'PENDING'::text NOT NULL,
    attempt_count smallint DEFAULT 0 NOT NULL,
    available_at timestamp with time zone DEFAULT now() NOT NULL,
    lease_owner text,
    lease_token text,
    lease_expires_at timestamp with time zone,
    processed_at timestamp with time zone,
    dead_lettered_at timestamp with time zone,
    last_error_code text,
    last_error_detail text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_source_observation_queue_bounds CHECK ((((lease_owner IS NULL) OR ((length(lease_owner) >= 1) AND (length(lease_owner) <= 128))) AND ((lease_token IS NULL) OR (lease_token ~ '^[0-9a-f]{64}$'::text)) AND ((last_error_code IS NULL) OR ((length(last_error_code) >= 1) AND (length(last_error_code) <= 128))) AND ((last_error_detail IS NULL) OR (length(last_error_detail) <= 2048)))),
    CONSTRAINT chk_source_observation_queue_lease_shape CHECK ((((status = 'PROCESSING'::text) AND (lease_owner IS NOT NULL) AND (lease_token IS NOT NULL) AND (lease_expires_at IS NOT NULL)) OR ((status <> 'PROCESSING'::text) AND (lease_owner IS NULL) AND (lease_token IS NULL) AND (lease_expires_at IS NULL)))),
    CONSTRAINT chk_source_observation_queue_status_vocab CHECK ((status = ANY (ARRAY['PENDING'::text, 'PROCESSING'::text, 'PROCESSED'::text, 'DEAD_LETTER'::text]))),
    CONSTRAINT chk_source_observation_queue_terminal_shape CHECK ((((status = 'PROCESSED'::text) = (processed_at IS NOT NULL)) AND ((status = 'DEAD_LETTER'::text) = (dead_lettered_at IS NOT NULL)))),
    CONSTRAINT source_observation_queue_attempt_count_check CHECK (((attempt_count >= 0) AND (attempt_count <= 64)))
);


--
-- Name: TABLE source_observation_queue; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.source_observation_queue IS 'Mutable processing state for immutable source observations.';


--
-- Name: source_observation_replay_epoch; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_replay_epoch (
    singleton boolean DEFAULT true NOT NULL,
    cutoff_received_at timestamp with time zone NOT NULL,
    activated_by text NOT NULL,
    reason text NOT NULL,
    CONSTRAINT chk_source_observation_replay_epoch_attribution CHECK ((((length(btrim(activated_by)) >= 1) AND (length(btrim(activated_by)) <= 128)) AND (activated_by = btrim(activated_by)) AND ((length(btrim(reason)) >= 1) AND (length(btrim(reason)) <= 1024)) AND (reason = btrim(reason)))),
    CONSTRAINT chk_source_observation_replay_epoch_singleton CHECK (singleton)
);


--
-- Name: source_observation_retention_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_retention_state (
    singleton boolean DEFAULT true NOT NULL,
    cursor_received_at timestamp with time zone DEFAULT '-infinity'::timestamp with time zone NOT NULL,
    cursor_id bigint DEFAULT 0 NOT NULL,
    upper_received_at timestamp with time zone,
    upper_id bigint,
    CONSTRAINT source_observation_retention_state_check CHECK (((upper_received_at IS NULL) = (upper_id IS NULL))),
    CONSTRAINT source_observation_retention_state_cursor_id_check CHECK ((cursor_id >= 0)),
    CONSTRAINT source_observation_retention_state_singleton_check CHECK (singleton)
);


--
-- Name: source_observation_subject_heads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observation_subject_heads (
    provider text NOT NULL,
    observation_kind text NOT NULL,
    subject_key text NOT NULL,
    source_observation_id bigint NOT NULL,
    evidence_sha256 text NOT NULL,
    effective_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_source_observation_subject_head_bounds CHECK (((length(subject_key) >= 1) AND (length(subject_key) <= 256))),
    CONSTRAINT chk_source_observation_subject_head_hash CHECK ((evidence_sha256 ~ '^[0-9a-f]{64}$'::text))
);


--
-- Name: source_observations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_observations (
    id bigint NOT NULL,
    provider text NOT NULL,
    observation_kind text NOT NULL,
    subject_key text NOT NULL,
    observation_key bytea NOT NULL,
    schema_version smallint NOT NULL,
    contract_generation bigint NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    source_event_at timestamp with time zone,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    scope_sha256 bytea NOT NULL,
    completeness text NOT NULL,
    continuity text NOT NULL,
    evidence_sha256 bytea NOT NULL,
    fence_epoch bigint NOT NULL,
    projection_generation bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    payload_id bigint NOT NULL,
    provenance_id integer NOT NULL,
    CONSTRAINT chk_source_observation_completeness_vocab CHECK ((completeness = ANY (ARRAY['COMPLETE'::text, 'PARTIAL'::text, 'UNKNOWN'::text]))),
    CONSTRAINT chk_source_observation_continuity_vocab CHECK ((continuity = ANY (ARRAY['CONTIGUOUS'::text, 'GAP_UNRESOLVED'::text, 'NOT_APPLICABLE'::text]))),
    CONSTRAINT chk_source_observation_hashes CHECK (((octet_length(scope_sha256) = 32) AND (octet_length(evidence_sha256) = 32))),
    CONSTRAINT chk_source_observation_key CHECK ((octet_length(observation_key) = 32)),
    CONSTRAINT chk_source_observation_text_bounds CHECK (((length(subject_key) >= 1) AND (length(subject_key) <= 256))),
    CONSTRAINT source_observations_contract_generation_check CHECK ((contract_generation > 0)),
    CONSTRAINT source_observations_fence_epoch_check CHECK ((fence_epoch > 0)),
    CONSTRAINT source_observations_projection_generation_check CHECK ((projection_generation > 0)),
    CONSTRAINT source_observations_schema_version_check CHECK ((schema_version > 0))
)
WITH (autovacuum_vacuum_scale_factor='0.05', autovacuum_vacuum_threshold='500', toast.autovacuum_vacuum_scale_factor='0.05', toast.autovacuum_vacuum_threshold='500');


--
-- Name: TABLE source_observations; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.source_observations IS 'Immutable normalized source evidence.';


--
-- Name: source_observations_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.source_observations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: source_observations_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.source_observations_id_seq OWNED BY public.source_observations.id;


--
-- Name: source_reconciliation_conflicts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_reconciliation_conflicts (
    id bigint NOT NULL,
    observation_id bigint,
    provider text NOT NULL,
    observation_kind text NOT NULL,
    subject_key text NOT NULL,
    observation_key text NOT NULL,
    evidence_sha256 text NOT NULL,
    entity_kind text NOT NULL,
    entity_key text NOT NULL,
    field_name text NOT NULL,
    effective_at timestamp with time zone NOT NULL,
    existing_value_sha256 text NOT NULL,
    attempted_value_sha256 text NOT NULL,
    decision text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_source_reconciliation_conflict_bounds CHECK ((((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((length(observation_key) >= 1) AND (length(observation_key) <= 512)) AND ((length(entity_kind) >= 1) AND (length(entity_kind) <= 64)) AND ((length(entity_key) >= 1) AND (length(entity_key) <= 256)) AND ((length(field_name) >= 1) AND (length(field_name) <= 128)))),
    CONSTRAINT chk_source_reconciliation_conflict_hashes CHECK (((evidence_sha256 ~ '^[0-9a-f]{64}$'::text) AND (existing_value_sha256 ~ '^[0-9a-f]{64}$'::text) AND (attempted_value_sha256 ~ '^[0-9a-f]{64}$'::text))),
    CONSTRAINT source_reconciliation_conflicts_decision_check CHECK ((decision = ANY (ARRAY['KEEP_EXISTING'::text, 'UNRESOLVED'::text])))
);


--
-- Name: source_reconciliation_conflicts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.source_reconciliation_conflicts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: source_reconciliation_conflicts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.source_reconciliation_conflicts_id_seq OWNED BY public.source_reconciliation_conflicts.id;


--
-- Name: x_space_session; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.x_space_session (
    id integer NOT NULL,
    revision bigint DEFAULT 0 NOT NULL,
    active_revision bigint DEFAULT 0 NOT NULL,
    active_ciphertext bytea,
    candidate_ciphertext bytea,
    state text DEFAULT 'unconfigured'::text NOT NULL,
    candidate_state text DEFAULT 'idle'::text NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    candidate_error text DEFAULT ''::text NOT NULL,
    last_checked_at timestamp with time zone,
    last_success_at timestamp with time zone,
    next_check_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_x_space_session_candidate_state_vocab CHECK ((candidate_state = ANY (ARRAY['idle'::text, 'pending'::text, 'accepted'::text, 'rejected'::text]))),
    CONSTRAINT chk_x_space_session_state_vocab CHECK ((state = ANY (ARRAY['unconfigured'::text, 'connected'::text, 'auth_required'::text, 'rate_limited'::text, 'error'::text]))),
    CONSTRAINT x_space_session_id_check CHECK ((id = 1))
);


--
-- Name: x_space_starts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.x_space_starts (
    space_id character varying(64) NOT NULL,
    payload jsonb NOT NULL,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: youtube_channel_live_checks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_channel_live_checks (
    channel_id character varying(64) NOT NULL,
    provider text NOT NULL,
    outcome text NOT NULL,
    selected_video_id character varying(20),
    channel_identity_confirmed boolean NOT NULL,
    unknown_reason text,
    observation_id bigint,
    evidence_sha256 text NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    effective_at timestamp with time zone NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_channel_live_checks_effective_clock CHECK ((effective_at = scheduled_for)),
    CONSTRAINT chk_youtube_channel_live_checks_hash CHECK ((evidence_sha256 ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT chk_youtube_channel_live_checks_identity CHECK ((((length((channel_id)::text) >= 1) AND (length((channel_id)::text) <= 64)) AND ((selected_video_id IS NULL) OR ((length((selected_video_id)::text) >= 1) AND (length((selected_video_id)::text) <= 20))))),
    CONSTRAINT chk_youtube_channel_live_checks_outcome_shape CHECK ((((outcome = 'UNKNOWN'::text) = (unknown_reason IS NOT NULL)) AND ((outcome = 'UNKNOWN'::text) OR channel_identity_confirmed) AND ((outcome <> 'CHANNEL_PAGE'::text) OR (selected_video_id IS NULL)) AND ((outcome <> ALL (ARRAY['LIVE_VIDEO'::text, 'UPCOMING_VIDEO'::text])) OR (selected_video_id IS NOT NULL)) AND ((unknown_reason IS NULL) OR (unknown_reason <> ALL (ARRAY['identity_missing'::text, 'identity_mismatch'::text])) OR (NOT channel_identity_confirmed)))),
    CONSTRAINT chk_youtube_channel_live_checks_outcome_vocab CHECK ((outcome = ANY (ARRAY['LIVE_VIDEO'::text, 'UPCOMING_VIDEO'::text, 'CHANNEL_PAGE'::text, 'UNKNOWN'::text]))),
    CONSTRAINT chk_youtube_channel_live_checks_provider CHECK ((provider = 'youtubejs'::text)),
    CONSTRAINT chk_youtube_channel_live_checks_unknown_reason_vocab CHECK (((unknown_reason IS NULL) OR (unknown_reason = ANY (ARRAY['identity_missing'::text, 'identity_mismatch'::text, 'contradictory_fields'::text, 'structure_unrecognized'::text, 'not_waiting_state'::text, 'login_required_unclassified'::text, 'error_unclassified'::text, 'request_failed'::text]))))
);


--
-- Name: youtube_channel_photo_heads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_channel_photo_heads (
    channel_id text NOT NULL,
    kind text NOT NULL,
    identity text DEFAULT ''::text NOT NULL,
    url text DEFAULT ''::text NOT NULL,
    width integer DEFAULT 0 NOT NULL,
    height integer DEFAULT 0 NOT NULL,
    effective_at timestamp with time zone,
    candidate_identity text DEFAULT ''::text NOT NULL,
    candidate_url text DEFAULT ''::text NOT NULL,
    candidate_width integer DEFAULT 0 NOT NULL,
    candidate_height integer DEFAULT 0 NOT NULL,
    candidate_slots smallint DEFAULT 0 NOT NULL,
    candidate_first_scheduled_for timestamp with time zone,
    candidate_last_scheduled_for timestamp with time zone,
    candidate_first_received_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_photo_head_bounds CHECK (((length(identity) <= 520) AND (length(url) <= 2048) AND (length(candidate_identity) <= 520) AND (length(candidate_url) <= 2048) AND ((width >= 0) AND (width <= 20000)) AND ((height >= 0) AND (height <= 20000)) AND ((candidate_width >= 0) AND (candidate_width <= 20000)) AND ((candidate_height >= 0) AND (candidate_height <= 20000)) AND ((candidate_slots >= 0) AND (candidate_slots <= 32767)))),
    CONSTRAINT chk_youtube_photo_head_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64))),
    CONSTRAINT chk_youtube_photo_head_kind CHECK ((kind = ANY (ARRAY['avatar'::text, 'banner'::text])))
);


--
-- Name: youtube_channel_photo_variants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_channel_photo_variants (
    channel_id text NOT NULL,
    kind text NOT NULL,
    provider text NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    url text NOT NULL,
    width integer DEFAULT 0 NOT NULL,
    height integer DEFAULT 0 NOT NULL,
    stable_media_id text DEFAULT ''::text NOT NULL,
    content_fingerprint text DEFAULT ''::text NOT NULL,
    observation_id bigint,
    effective_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone NOT NULL,
    CONSTRAINT chk_youtube_photo_variant_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64))),
    CONSTRAINT chk_youtube_photo_variant_dims CHECK ((((width >= 0) AND (width <= 20000)) AND ((height >= 0) AND (height <= 20000)))),
    CONSTRAINT chk_youtube_photo_variant_identity CHECK (((length(stable_media_id) <= 512) AND ((content_fingerprint = ''::text) OR (content_fingerprint ~ '^[0-9a-f]{64}$'::text)))),
    CONSTRAINT chk_youtube_photo_variant_kind CHECK ((kind = ANY (ARRAY['avatar'::text, 'banner'::text]))),
    CONSTRAINT chk_youtube_photo_variant_provider CHECK ((provider = ANY (ARRAY['youtubejs'::text, 'holodex'::text, 'hololive_official'::text]))),
    CONSTRAINT chk_youtube_photo_variant_url CHECK ((((length(url) >= 8) AND (length(url) <= 2048)) AND (url ~~ 'https://%'::text)))
);


--
-- Name: youtube_channel_profile_evidence; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_channel_profile_evidence (
    channel_id text NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    provider text NOT NULL,
    observation_id bigint,
    handle_present boolean NOT NULL,
    handle text DEFAULT ''::text NOT NULL,
    description_present boolean NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    country_present boolean NOT NULL,
    country text DEFAULT ''::text NOT NULL,
    joined_date_present boolean NOT NULL,
    joined_date text DEFAULT ''::text NOT NULL,
    complete boolean NOT NULL,
    effective_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone NOT NULL,
    CONSTRAINT chk_youtube_profile_evidence_bounds CHECK (((length(handle) <= 256) AND (octet_length(description) <= 4096) AND (length(country) <= 50) AND (length(joined_date) <= 256))),
    CONSTRAINT chk_youtube_profile_evidence_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64))),
    CONSTRAINT chk_youtube_profile_evidence_provider CHECK ((provider = ANY (ARRAY['youtubejs'::text, 'holodex'::text, 'hololive_official'::text])))
);


--
-- Name: youtube_channel_profile_heads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_channel_profile_heads (
    channel_id text NOT NULL,
    handle_set boolean DEFAULT false NOT NULL,
    handle text DEFAULT ''::text NOT NULL,
    handle_effective_at timestamp with time zone,
    description_set boolean DEFAULT false NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    description_effective_at timestamp with time zone,
    description_empty_slots smallint DEFAULT 0 NOT NULL,
    description_empty_first_scheduled_for timestamp with time zone,
    description_empty_last_scheduled_for timestamp with time zone,
    description_empty_first_received_at timestamp with time zone,
    country_set boolean DEFAULT false NOT NULL,
    country text DEFAULT ''::text NOT NULL,
    country_effective_at timestamp with time zone,
    country_empty_slots smallint DEFAULT 0 NOT NULL,
    country_empty_first_scheduled_for timestamp with time zone,
    country_empty_last_scheduled_for timestamp with time zone,
    country_empty_first_received_at timestamp with time zone,
    joined_date_set boolean DEFAULT false NOT NULL,
    joined_date text DEFAULT ''::text NOT NULL,
    joined_date_effective_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_profile_head_bounds CHECK (((length(handle) <= 256) AND (octet_length(description) <= 4096) AND (length(country) <= 50) AND (length(joined_date) <= 256) AND ((description_empty_slots >= 0) AND (description_empty_slots <= 32767)) AND ((country_empty_slots >= 0) AND (country_empty_slots <= 32767)))),
    CONSTRAINT chk_youtube_profile_head_channel CHECK (((length(channel_id) >= 1) AND (length(channel_id) <= 64)))
);


--
-- Name: youtube_channel_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_channel_profiles (
    channel_id character varying(64) NOT NULL,
    avatar jsonb,
    banner jsonb,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: youtube_collection_job_leases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_collection_job_leases (
    job_key text NOT NULL,
    provider text NOT NULL,
    job_class text NOT NULL,
    collection_job_kind text NOT NULL,
    subject_key text NOT NULL,
    projection_generation bigint NOT NULL,
    poll_interval_ms bigint NOT NULL,
    slot_state text DEFAULT 'IDLE'::text NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    next_due_at timestamp with time zone NOT NULL,
    retry_not_before timestamp with time zone,
    fence_epoch bigint DEFAULT 0 NOT NULL,
    owner_instance text,
    lease_expires_at timestamp with time zone,
    last_completed_at timestamp with time zone,
    last_error_code text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_failure_code text,
    last_failure_class text,
    last_failure_detail text,
    last_failure_at timestamp with time zone,
    membership_kinds text[] DEFAULT '{}'::text[] NOT NULL,
    membership_exact_subject boolean DEFAULT false NOT NULL,
    membership_target_count integer DEFAULT 0 NOT NULL,
    CONSTRAINT chk_youtube_collection_job_identity CHECK ((((length(job_key) >= 1) AND (length(job_key) <= 512)) AND ((length(collection_job_kind) >= 1) AND (length(collection_job_kind) <= 128)) AND ((length(subject_key) >= 1) AND (length(subject_key) <= 256)) AND ((owner_instance IS NULL) OR ((length(owner_instance) >= 1) AND (length(owner_instance) <= 128))) AND ((last_error_code IS NULL) OR ((length(last_error_code) >= 1) AND (length(last_error_code) <= 128))))),
    CONSTRAINT chk_youtube_collection_job_last_failure_shape CHECK ((((last_failure_code IS NULL) AND (last_failure_class IS NULL) AND (last_failure_detail IS NULL) AND (last_failure_at IS NULL)) OR ((last_failure_code IS NOT NULL) AND ((length(last_failure_code) >= 1) AND (length(last_failure_code) <= 128)) AND (last_failure_class IS NOT NULL) AND (last_failure_class ~ '^[A-Za-z][A-Za-z0-9_]{0,63}$'::text) AND (last_failure_detail IS NOT NULL) AND (octet_length(last_failure_detail) <= 2048) AND (last_failure_at IS NOT NULL)))),
    CONSTRAINT chk_youtube_collection_job_membership_count CHECK ((membership_target_count >= 0)),
    CONSTRAINT chk_youtube_collection_job_provider_vocab CHECK ((provider = ANY (ARRAY['holodex'::text, 'youtubejs'::text, 'hololive_official'::text]))),
    CONSTRAINT chk_youtube_collection_job_slot_shape CHECK ((((slot_state = 'IDLE'::text) AND (owner_instance IS NULL) AND (lease_expires_at IS NULL) AND (retry_not_before IS NULL)) OR ((slot_state = 'ACTIVE'::text) AND (owner_instance IS NOT NULL) AND (lease_expires_at IS NOT NULL) AND (retry_not_before IS NULL)) OR ((slot_state = 'DEFERRED'::text) AND (owner_instance IS NULL) AND (lease_expires_at IS NULL) AND (retry_not_before IS NOT NULL)))),
    CONSTRAINT youtube_collection_job_leases_fence_epoch_check CHECK ((fence_epoch >= 0)),
    CONSTRAINT youtube_collection_job_leases_job_class_check CHECK ((job_class = ANY (ARRAY['GLOBAL'::text, 'SUBJECT'::text]))),
    CONSTRAINT youtube_collection_job_leases_poll_interval_ms_check CHECK (((poll_interval_ms >= 1000) AND (poll_interval_ms <= 86400000))),
    CONSTRAINT youtube_collection_job_leases_slot_state_check CHECK ((slot_state = ANY (ARRAY['IDLE'::text, 'ACTIVE'::text, 'DEFERRED'::text])))
);


--
-- Name: youtube_collection_projection_generations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_collection_projection_generations (
    generation bigint NOT NULL,
    status text NOT NULL,
    row_count integer NOT NULL,
    projection_sha256 text CONSTRAINT youtube_collection_projection_genera_projection_sha256_not_null NOT NULL,
    valid_until timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    activated_at timestamp with time zone,
    eligibility_version bigint DEFAULT 1 CONSTRAINT youtube_collection_projection_gene_eligibility_version_not_null NOT NULL,
    validity_refreshed_at timestamp with time zone,
    CONSTRAINT chk_youtube_collection_projection_activation_shape CHECK ((((status = 'STAGING'::text) AND (activated_at IS NULL)) OR ((status = ANY (ARRAY['CURRENT'::text, 'RETIRED'::text])) AND (activated_at IS NOT NULL)))),
    CONSTRAINT chk_youtube_projection_eligibility_version CHECK ((eligibility_version > 0)),
    CONSTRAINT youtube_collection_projection_generatio_projection_sha256_check CHECK ((projection_sha256 ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT youtube_collection_projection_generations_row_count_check CHECK ((row_count >= 0)),
    CONSTRAINT youtube_collection_projection_generations_status_check CHECK ((status = ANY (ARRAY['STAGING'::text, 'CURRENT'::text, 'RETIRED'::text])))
);


--
-- Name: youtube_collection_projection_generations_generation_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.youtube_collection_projection_generations ALTER COLUMN generation ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.youtube_collection_projection_generations_generation_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: youtube_collection_projection_guard; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_collection_projection_guard (
    guard_key boolean DEFAULT true NOT NULL,
    runtime_writer xid8,
    CONSTRAINT chk_youtube_collection_projection_guard_singleton CHECK (guard_key)
);


--
-- Name: youtube_collection_runtime_bundles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_collection_runtime_bundles (
    projection_generation bigint CONSTRAINT youtube_collection_runtime_bundl_projection_generation_not_null NOT NULL,
    kind text NOT NULL COLLATE pg_catalog."C",
    subject_key text NOT NULL COLLATE pg_catalog."C",
    rpc_calls bigint NOT NULL,
    interval_ms bigint NOT NULL,
    created_at timestamp with time zone NOT NULL,
    eligible_at timestamp with time zone NOT NULL
);


--
-- Name: TABLE youtube_collection_runtime_bundles; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.youtube_collection_runtime_bundles IS 'CURRENT/STAGING의 enabled target을 bytewise bundle로 집계한 동기 모델. 유효기간과 lease는 복사하지 않는다. writer는 builder/header/target 잠금 전에 기존 projection guard를 lock_youtube_collection_runtime_bundles()로 획득한다.';


--
-- Name: youtube_collection_runtime_kinds; Type: VIEW; Schema: public; Owner: -
--

CREATE VIEW public.youtube_collection_runtime_kinds AS
 SELECT column1 AS kind,
    column2 AS observation_kind,
    column3 AS rpc_per_kind
   FROM (VALUES ('youtubejs_channel_live'::text,'live_snapshot'::text,false), ('youtubejs_channel_live_check'::text,'channel_live_check'::text,false), ('youtubejs_video_live'::text,'video_live_check'::text,false), ('community_collect'::text,'community_page'::text,false), ('youtubejs_content'::text,'video_list'::text,true), ('youtubejs_content'::text,'shorts_list'::text,true), ('youtubejs_channel_metadata'::text,'channel_profile'::text,false), ('youtubejs_channel_metadata'::text,'channel_photo'::text,false)) "*VALUES*";


--
-- Name: youtube_collection_target_reasons; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_collection_target_reasons (
    projection_generation bigint CONSTRAINT youtube_collection_target_reason_projection_generation_not_null NOT NULL,
    subject_key text NOT NULL,
    observation_kind text NOT NULL,
    reason_kind text NOT NULL,
    reason_key text NOT NULL,
    CONSTRAINT chk_youtube_collection_target_reason_bounds CHECK ((((length(reason_kind) >= 1) AND (length(reason_kind) <= 128)) AND ((length(reason_key) >= 1) AND (length(reason_key) <= 512))))
);


--
-- Name: youtube_collection_targets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_collection_targets (
    projection_generation bigint NOT NULL,
    subject_key text NOT NULL,
    observation_kind text NOT NULL,
    priority smallint NOT NULL,
    poll_interval_ms bigint NOT NULL,
    enabled boolean NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    member_since_generation bigint,
    not_before timestamp with time zone,
    CONSTRAINT chk_youtube_collection_target_kind_vocab CHECK ((observation_kind = ANY (ARRAY['community_page'::text, 'video_list'::text, 'shorts_list'::text, 'live_snapshot'::text, 'channel_profile'::text, 'channel_photo'::text, 'schedule_snapshot'::text, 'channel_live_check'::text, 'video_live_check'::text]))),
    CONSTRAINT chk_youtube_collection_target_member_since CHECK (((member_since_generation IS NULL) OR ((member_since_generation >= 1) AND (member_since_generation <= projection_generation)))),
    CONSTRAINT chk_youtube_collection_target_subject CHECK (((length(subject_key) >= 1) AND (length(subject_key) <= 256))),
    CONSTRAINT youtube_collection_targets_poll_interval_ms_check CHECK (((poll_interval_ms >= 1000) AND (poll_interval_ms <= 86400000))),
    CONSTRAINT youtube_collection_targets_priority_check CHECK (((priority >= 0) AND (priority <= 100)))
);


--
-- Name: youtube_community_posts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_community_posts (
    post_id character varying(50) NOT NULL,
    channel_id character varying(64) NOT NULL,
    author_name character varying(200),
    author_photo jsonb,
    content_text text,
    published_text character varying(100),
    like_count bigint DEFAULT 0,
    comment_count bigint DEFAULT 0,
    images jsonb,
    attached_video character varying(20),
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone
);


--
-- Name: youtube_community_shorts_alarm_states; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_community_shorts_alarm_states (
    kind text NOT NULL,
    post_id character varying(50) NOT NULL,
    content_id character varying(50) NOT NULL,
    channel_id character varying(64) NOT NULL,
    actual_published_at timestamp with time zone,
    detected_at timestamp with time zone NOT NULL,
    alarm_sent_at timestamp with time zone,
    delivery_status text DEFAULT 'DETECTED'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_community_shorts_alarm_states_delivery_status_vocab CHECK ((delivery_status = ANY (ARRAY['DETECTED'::text, 'SENT'::text]))),
    CONSTRAINT chk_youtube_community_shorts_alarm_states_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text])))
)
WITH (autovacuum_vacuum_scale_factor='0.02', autovacuum_vacuum_threshold='50', autovacuum_analyze_scale_factor='0.02', autovacuum_analyze_threshold='50');


--
-- Name: youtube_community_shorts_source_posts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_community_shorts_source_posts (
    kind text NOT NULL,
    post_id character varying(50) NOT NULL,
    channel_id character varying(64) NOT NULL,
    actual_published_at timestamp with time zone,
    detected_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_community_shorts_source_posts_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text])))
);


--
-- Name: youtube_content_absence_slots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_content_absence_slots (
    channel_id character varying(50) NOT NULL,
    observation_kind text NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    observation_id bigint,
    evidence_sha256 bytea NOT NULL,
    effective_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone NOT NULL,
    scope_sha256 bytea NOT NULL,
    coverage jsonb NOT NULL,
    CONSTRAINT chk_youtube_content_absence_bounds CHECK (((length((channel_id)::text) >= 1) AND (length((channel_id)::text) <= 50))),
    CONSTRAINT chk_youtube_content_absence_coverage CHECK (((jsonb_typeof(coverage) = 'object'::text) AND (octet_length((coverage)::text) <= 8192))),
    CONSTRAINT chk_youtube_content_absence_hashes CHECK (((octet_length(scope_sha256) = 32) AND (octet_length(evidence_sha256) = 32))),
    CONSTRAINT chk_youtube_content_absence_kind CHECK ((observation_kind = ANY (ARRAY['video_list'::text, 'shorts_list'::text])))
);
ALTER TABLE ONLY public.youtube_content_absence_slots ALTER COLUMN coverage SET COMPRESSION lz4;


--
-- Name: youtube_content_alarm_tracking; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_content_alarm_tracking (
    kind text NOT NULL,
    content_id character varying(50) NOT NULL,
    channel_id character varying(64) NOT NULL,
    actual_published_at timestamp with time zone,
    detected_at timestamp with time zone NOT NULL,
    alarm_sent_at timestamp with time zone,
    alarm_latency_millis bigint,
    alarm_latency_exceeded boolean,
    delivery_status text DEFAULT 'PENDING'::character varying NOT NULL,
    latency_classification_status character varying(40),
    delay_source character varying(40),
    internal_delay_cause character varying(40),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    canonical_content_id character varying(50) NOT NULL,
    CONSTRAINT chk_youtube_content_alarm_tracking_delivery_status_vocab CHECK ((delivery_status = ANY (ARRAY[('PENDING'::character varying)::text, ('SENT'::character varying)::text]))),
    CONSTRAINT chk_youtube_content_alarm_tracking_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text])))
)
WITH (autovacuum_vacuum_scale_factor='0.05', autovacuum_vacuum_threshold='100', autovacuum_analyze_scale_factor='0.05', autovacuum_analyze_threshold='100');


--
-- Name: youtube_content_channel_heads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_content_channel_heads (
    channel_id character varying(50) NOT NULL,
    observation_kind text NOT NULL,
    earliest_complete_effective_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    earliest_baseline_effective_at timestamp with time zone,
    CONSTRAINT chk_youtube_content_channel_head_bounds CHECK (((length((channel_id)::text) >= 1) AND (length((channel_id)::text) <= 50))),
    CONSTRAINT chk_youtube_content_channel_head_kind CHECK ((observation_kind = ANY (ARRAY['video_list'::text, 'shorts_list'::text])))
);


--
-- Name: youtube_content_evidence_clocks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_content_evidence_clocks (
    video_id character varying(20) NOT NULL,
    first_positive_effective_at timestamp with time zone CONSTRAINT youtube_content_evidence_cl_first_positive_effective_a_not_null NOT NULL,
    last_positive_effective_at timestamp with time zone CONSTRAINT youtube_content_evidence_cl_last_positive_effective_at_not_null NOT NULL,
    last_positive_received_at timestamp with time zone CONSTRAINT youtube_content_evidence_clo_last_positive_received_at_not_null NOT NULL,
    last_positive_value_sha256 text CONSTRAINT youtube_content_evidence_cl_last_positive_value_sha256_not_null NOT NULL,
    last_positive_scope_sha256 text CONSTRAINT youtube_content_evidence_cl_last_positive_scope_sha256_not_null NOT NULL,
    last_positive_coverage jsonb NOT NULL,
    last_negative_effective_at timestamp with time zone,
    last_negative_received_at timestamp with time zone,
    first_absence_scheduled_for timestamp with time zone,
    second_absence_scheduled_for timestamp with time zone,
    last_absence_observation_id bigint,
    missing_since_effective_at timestamp with time zone,
    consecutive_absence_slots smallint DEFAULT 0 CONSTRAINT youtube_content_evidence_clo_consecutive_absence_slots_not_null NOT NULL,
    withdrawn_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    novelty_pending boolean DEFAULT false NOT NULL,
    CONSTRAINT chk_youtube_content_clock_coverage CHECK (((jsonb_typeof(last_positive_coverage) = 'object'::text) AND (octet_length((last_positive_coverage)::text) <= 8192))),
    CONSTRAINT chk_youtube_content_clock_hashes CHECK (((last_positive_value_sha256 ~ '^[0-9a-f]{64}$'::text) AND (last_positive_scope_sha256 ~ '^[0-9a-f]{64}$'::text))),
    CONSTRAINT chk_youtube_content_clock_video_id CHECK (((length((video_id)::text) >= 1) AND (length((video_id)::text) <= 20))),
    CONSTRAINT youtube_content_evidence_clocks_consecutive_absence_slots_check CHECK (((consecutive_absence_slots >= 0) AND (consecutive_absence_slots <= 32767)))
);


--
-- Name: youtube_content_watermarks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_content_watermarks (
    channel_id character varying(64) NOT NULL,
    watermark_type character varying(20) NOT NULL,
    initialized boolean DEFAULT false NOT NULL,
    last_content_id character varying(50),
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_content_watermarks_watermark_type_vocab CHECK (((watermark_type)::text = ANY ((ARRAY['VIDEO'::character varying, 'SHORT'::character varying, 'COMMUNITY_POST'::character varying])::text[])))
);


--
-- Name: youtube_live_absence_slots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_live_absence_slots (
    observation_id bigint NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    evidence_sha256 bytea NOT NULL,
    effective_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone NOT NULL,
    scope_sha256 bytea NOT NULL,
    coverage jsonb NOT NULL,
    CONSTRAINT chk_youtube_live_absence_hashes CHECK (((octet_length(scope_sha256) = 32) AND (octet_length(evidence_sha256) = 32))),
    CONSTRAINT chk_youtube_live_absence_slots_coverage CHECK (((jsonb_typeof(coverage) = 'object'::text) AND (jsonb_typeof((coverage -> 'requested_channel_ids'::text)) = 'array'::text)))
);
ALTER TABLE ONLY public.youtube_live_absence_slots ALTER COLUMN coverage SET COMPRESSION lz4;


--
-- Name: youtube_live_pending_ends; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_live_pending_ends (
    video_id text NOT NULL,
    channel_id character varying(64) NOT NULL,
    kind text NOT NULL,
    observation_id bigint NOT NULL,
    effective_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    ended_at timestamp with time zone,
    negative_eligible boolean NOT NULL,
    scope_covers boolean NOT NULL,
    CONSTRAINT chk_youtube_live_pending_ends_kind_vocab CHECK ((kind = ANY (ARRAY['EXPLICIT_END'::text, 'EXPLICIT_CANCEL'::text, 'SCOPED_ABSENCE'::text]))),
    CONSTRAINT chk_youtube_live_pending_ends_video_id CHECK (((length(video_id) >= 1) AND (length(video_id) <= 128)))
);


--
-- Name: youtube_live_reconciliation_heads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_live_reconciliation_heads (
    video_id text NOT NULL,
    status text NOT NULL,
    last_upcoming_positive_at timestamp with time zone,
    last_upcoming_positive_seen_at timestamp with time zone,
    last_live_positive_at timestamp with time zone,
    last_live_positive_seen_at timestamp with time zone,
    last_end_evidence_at timestamp with time zone,
    last_complete_absence_at timestamp with time zone,
    last_absence_scheduled_for timestamp with time zone,
    consecutive_absence_slots smallint DEFAULT 0 CONSTRAINT youtube_live_reconciliation__consecutive_absence_slots_not_null NOT NULL,
    end_candidate_kind text,
    end_candidate_observation_id bigint,
    next_end_check_at timestamp with time zone,
    ended_at timestamp with time zone,
    end_reason text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    first_absence_scheduled_for timestamp with time zone,
    second_absence_scheduled_for timestamp with time zone,
    last_absence_observation_id bigint DEFAULT 0 CONSTRAINT youtube_live_reconciliation_last_absence_observation_i_not_null NOT NULL,
    ignored_absence_scheduled_for timestamp with time zone[] DEFAULT '{}'::timestamp with time zone[] CONSTRAINT youtube_live_reconciliation_ignored_absence_scheduled__not_null NOT NULL,
    unresolvable_since timestamp with time zone,
    CONSTRAINT chk_youtube_live_head_candidate_shape CHECK ((((end_candidate_kind IS NULL) AND (end_candidate_observation_id IS NULL) AND (next_end_check_at IS NULL)) OR ((end_candidate_kind IS NOT NULL) AND (end_candidate_observation_id IS NOT NULL) AND (next_end_check_at IS NOT NULL)))),
    CONSTRAINT chk_youtube_live_head_video_id CHECK (((length(video_id) >= 1) AND (length(video_id) <= 128))),
    CONSTRAINT chk_youtube_live_reconciliation_heads_end_reason_vocab CHECK ((end_reason = ANY (ARRAY['EXPLICIT_END'::text, 'CANCELLED_BEFORE_LIVE'::text, 'SCOPED_ABSENCE'::text, 'UNRESOLVABLE_VIDEO'::text]))),
    CONSTRAINT youtube_live_reconciliation_hea_consecutive_absence_slots_check CHECK (((consecutive_absence_slots >= 0) AND (consecutive_absence_slots <= 32767))),
    CONSTRAINT youtube_live_reconciliation_heads_end_candidate_kind_check CHECK ((end_candidate_kind = ANY (ARRAY['EXPLICIT_END'::text, 'EXPLICIT_CANCEL'::text, 'SCOPED_ABSENCE'::text]))),
    CONSTRAINT youtube_live_reconciliation_heads_status_check CHECK ((status = ANY (ARRAY['UPCOMING'::text, 'LIVE'::text, 'ENDED'::text])))
);
ALTER TABLE ONLY public.youtube_live_reconciliation_heads ALTER COLUMN ignored_absence_scheduled_for SET STATISTICS 0;
ALTER TABLE ONLY public.youtube_live_reconciliation_heads ALTER COLUMN ignored_absence_scheduled_for SET COMPRESSION lz4;


--
-- Name: COLUMN youtube_live_reconciliation_heads.unresolvable_since; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.youtube_live_reconciliation_heads.unresolvable_since IS '마지막 LIVE positive 이후 영상 확인이 identity_missing으로만 이어진 첫 관측 시각. positive가 지우며 UNRESOLVABLE_VIDEO 종료의 ended_at 하한이다.';


--
-- Name: youtube_live_review_receipt_facts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_live_review_receipt_facts (
    receipt_id uuid NOT NULL,
    video_id text NOT NULL,
    recorded_at timestamp with time zone NOT NULL,
    facts jsonb NOT NULL,
    semantic_value public.youtube_live_review_value GENERATED ALWAYS AS (public.youtube_live_review_receipt_value(facts)) STORED
);
ALTER TABLE ONLY public.youtube_live_review_receipt_facts ALTER COLUMN facts SET COMPRESSION lz4;


--
-- Name: TABLE youtube_live_review_receipt_facts; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.youtube_live_review_receipt_facts IS '검토 영수증 기록 시 youtube_live_review_receipt_semantic_facts(original_snapshot)로 고정한 영수증 쪽 의미 사실이다. 영수증이 append-only라 이 행도 변경·삭제를 거부한다. youtube_live_review_semantic_facts 정의를 바꾸는 migration은 같은 파일에서 이 표를 TRUNCATE한 뒤 모든 영수증으로 다시 채워야 한다. 다시 채우지 않으면 기존 영수증이 현재 사실과 맞지 않아 검토가 다시 열린다.';


--
-- Name: COLUMN youtube_live_review_receipt_facts.semantic_value; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.youtube_live_review_receipt_facts.semantic_value IS '불변 facts의 정확한 typed 투영. 원본 상태·신선도는 저장하지 않으며 누락/추가 key나 타입이 다른 문서는 NULL이다. decoder·composite 타입·의미 facts 정의를 바꾸는 migration은 같은 파일에서 이 generated 열도 기존 facts로 다시 계산해야 한다.';


--
-- Name: youtube_live_review_receipts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_live_review_receipts (
    receipt_id uuid NOT NULL,
    video_id text NOT NULL,
    snapshot_sha256 text NOT NULL,
    original_snapshot jsonb NOT NULL,
    evidence_refs jsonb NOT NULL,
    disposition text NOT NULL,
    operator_id text NOT NULL,
    reason text NOT NULL,
    recorded_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT youtube_live_review_receipts_disposition_check CHECK ((disposition = 'closed_unresolved'::text)),
    CONSTRAINT youtube_live_review_receipts_operator_id_check CHECK ((((length(operator_id) >= 1) AND (length(operator_id) <= 128)) AND (operator_id = btrim(operator_id)) AND (operator_id !~ '[[:cntrl:]]'::text))),
    CONSTRAINT youtube_live_review_receipts_original_snapshot_check CHECK ((octet_length((original_snapshot)::text) <= 262144)),
    CONSTRAINT youtube_live_review_receipts_reason_check CHECK ((((length(reason) >= 1) AND (length(reason) <= 1024)) AND (reason = btrim(reason)) AND (reason !~ '[[:cntrl:]]'::text))),
    CONSTRAINT youtube_live_review_receipts_snapshot_sha256_check CHECK ((snapshot_sha256 ~ '^[0-9a-f]{64}$'::text))
);
ALTER TABLE ONLY public.youtube_live_review_receipts ALTER COLUMN original_snapshot SET COMPRESSION lz4;


--
-- Name: youtube_live_runtime_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_live_runtime_state (
    video_id text NOT NULL,
    channel_id text,
    session_status text,
    head_status text,
    scheduled_start_time timestamp with time zone,
    lifecycle_origin text,
    last_live_positive_at timestamp with time zone,
    last_live_positive_seen_at timestamp with time zone,
    last_upcoming_positive_at timestamp with time zone,
    last_upcoming_positive_seen_at timestamp with time zone,
    availability_at timestamp with time zone,
    availability_seen_at timestamp with time zone,
    unresolvable_since timestamp with time zone,
    reviewed_at timestamp with time zone,
    CONSTRAINT youtube_live_runtime_state_check CHECK (((channel_id IS NULL) = (session_status IS NULL))),
    CONSTRAINT youtube_live_runtime_state_check1 CHECK ((COALESCE((session_status = ANY (ARRAY['LIVE'::text, 'UPCOMING'::text])), false) OR COALESCE((head_status = ANY (ARRAY['LIVE'::text, 'UPCOMING'::text])), false))),
    CONSTRAINT youtube_live_runtime_state_head_status_check CHECK ((head_status = ANY (ARRAY['UPCOMING'::text, 'LIVE'::text, 'ENDED'::text]))),
    CONSTRAINT youtube_live_runtime_state_lifecycle_origin_check CHECK ((lifecycle_origin = ANY (ARRAY['metadata_only'::text, 'observed'::text, 'legacy_unknown'::text]))),
    CONSTRAINT youtube_live_runtime_state_session_status_check CHECK ((session_status = ANY (ARRAY['UPCOMING'::text, 'LIVE'::text, 'ENDED'::text])))
);


--
-- Name: TABLE youtube_live_runtime_state; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.youtube_live_runtime_state IS 'session 또는 head가 LIVE/UPCOMING인 영상의 동기 읽기 모델. ENDED 상대 행과 session 없는 head도 보존한다. reviewed_at은 현재 검토 가능하고 정확한 310 의미 사실이 일치하는 영수증 중 최대 기록 시각이다. 원본 writer는 같은 READ COMMITTED 트랜잭션 끝에서 변경 key로 refresh_youtube_live_runtime_state()를 호출한다. 관측 writer는 lock_youtube_live_runtime_state_shared(), 배제 경로는 lock_youtube_live_runtime_state()를 원본 잠금보다 먼저 잡는다.';


--
-- Name: youtube_live_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_live_sessions (
    video_id character varying(20) NOT NULL,
    channel_id character varying(64) NOT NULL,
    status text NOT NULL,
    title character varying(500),
    scheduled_start_time timestamp with time zone,
    started_at timestamp with time zone,
    ended_at timestamp with time zone,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    live_first_seen_at timestamp with time zone,
    topic_id text DEFAULT ''::text NOT NULL,
    thumbnail_url text DEFAULT ''::text NOT NULL,
    is_premiere boolean,
    lifecycle_origin text NOT NULL,
    status_observed_at timestamp with time zone,
    schedule_observed_at timestamp with time zone,
    title_observed_at timestamp with time zone,
    history_sort_at timestamp with time zone GENERATED ALWAYS AS (COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at)) STORED,
    times_source text DEFAULT 'observed'::text NOT NULL,
    CONSTRAINT chk_youtube_live_sessions_lifecycle_origin_vocab CHECK ((lifecycle_origin = ANY (ARRAY['metadata_only'::text, 'observed'::text, 'legacy_unknown'::text]))),
    CONSTRAINT chk_youtube_live_sessions_status_vocab CHECK ((status = ANY (ARRAY[('UPCOMING'::character varying)::text, ('LIVE'::character varying)::text, ('ENDED'::character varying)::text]))),
    CONSTRAINT chk_youtube_live_sessions_times_source_vocab CHECK ((times_source = ANY (ARRAY['observed'::text, 'provider'::text])))
)
WITH (fillfactor='90');


--
-- Name: youtube_live_time_finalizations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_live_time_finalizations (
    video_id character varying(20) NOT NULL,
    queue text NOT NULL,
    outcome text NOT NULL,
    reason text,
    attempts smallint DEFAULT 0 NOT NULL,
    deferrals smallint DEFAULT 0 NOT NULL,
    retry_base_at timestamp with time zone,
    next_attempt_at timestamp with time zone,
    last_attempt_at timestamp with time zone,
    observation_id bigint,
    observed_started_at timestamp with time zone,
    observed_ended_at timestamp with time zone,
    enqueued_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_live_time_finalizations_counts CHECK ((((attempts >= 0) AND (attempts <= 4)) AND ((deferrals >= 0) AND (deferrals <= 12)))),
    CONSTRAINT chk_youtube_live_time_finalizations_outcome_vocab CHECK ((outcome = ANY (ARRAY['pending'::text, 'finalized'::text, 'exhausted'::text, 'conflict'::text, 'members_only'::text, 'reverted'::text]))),
    CONSTRAINT chk_youtube_live_time_finalizations_queue_vocab CHECK ((queue = ANY (ARRAY['new'::text, 'backlog'::text]))),
    CONSTRAINT chk_youtube_live_time_finalizations_reason CHECK (((reason IS NULL) OR ((length(reason) >= 1) AND (length(reason) <= 64)))),
    CONSTRAINT chk_youtube_live_time_finalizations_state CHECK (
CASE outcome
    WHEN 'pending'::text THEN ((completed_at IS NULL) AND ((next_attempt_at IS NOT NULL) OR ((queue = 'backlog'::text) AND (attempts = 0) AND (deferrals = 0))))
    WHEN 'finalized'::text THEN ((completed_at IS NOT NULL) AND (next_attempt_at IS NULL) AND (reason IS NULL))
    WHEN 'members_only'::text THEN ((completed_at IS NOT NULL) AND (next_attempt_at IS NULL))
    WHEN 'reverted'::text THEN ((completed_at IS NOT NULL) AND (next_attempt_at IS NULL))
    ELSE ((completed_at IS NOT NULL) AND (next_attempt_at IS NULL) AND (reason IS NOT NULL))
END)
);


--
-- Name: youtube_notification_delivery; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_notification_delivery (
    id bigint NOT NULL,
    outbox_id bigint NOT NULL,
    room_id character varying(100) NOT NULL,
    status text DEFAULT 'PENDING'::character varying NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_at timestamp with time zone,
    sent_at timestamp with time zone,
    error text,
    row_version bigint DEFAULT 0 NOT NULL,
    send_request_id text,
    locked_by text,
    CONSTRAINT chk_youtube_notification_delivery_row_version CHECK (((row_version IS NOT NULL) AND (row_version >= 0))),
    CONSTRAINT chk_youtube_notification_delivery_status_vocab CHECK ((status = ANY (ARRAY[('PENDING'::character varying)::text, ('SENDING'::character varying)::text, ('SENT'::character varying)::text, ('FAILED'::character varying)::text, ('QUARANTINED'::character varying)::text])))
)
WITH (autovacuum_vacuum_scale_factor='0.02', autovacuum_vacuum_threshold='50', autovacuum_analyze_scale_factor='0.02', autovacuum_analyze_threshold='50');


--
-- Name: youtube_notification_delivery_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.youtube_notification_delivery_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: youtube_notification_delivery_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.youtube_notification_delivery_id_seq OWNED BY public.youtube_notification_delivery.id;


--
-- Name: youtube_notification_delivery_ledger; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_notification_delivery_ledger (
    kind text NOT NULL,
    logical_id character varying(50) NOT NULL,
    room_id character varying(100) NOT NULL,
    status text NOT NULL,
    first_recorded_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    sent_at timestamp with time zone,
    quarantined_at timestamp with time zone,
    source_delivery_id bigint,
    CONSTRAINT chk_youtube_notification_delivery_ledger_identity CHECK (((length(btrim((logical_id)::text)) > 0) AND (length(btrim((room_id)::text)) > 0) AND ((logical_id)::text = btrim((logical_id)::text)) AND ((room_id)::text = btrim((room_id)::text)))),
    CONSTRAINT chk_youtube_notification_delivery_ledger_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text]))),
    CONSTRAINT chk_youtube_notification_delivery_ledger_shape CHECK ((((status = 'SENT'::text) AND (sent_at IS NOT NULL)) OR ((status = 'QUARANTINED'::text) AND (sent_at IS NULL) AND (quarantined_at IS NOT NULL)))),
    CONSTRAINT chk_youtube_notification_delivery_ledger_source CHECK (((source_delivery_id IS NULL) OR (source_delivery_id > 0))),
    CONSTRAINT chk_youtube_notification_delivery_ledger_status CHECK ((status = ANY (ARRAY['SENT'::text, 'QUARANTINED'::text]))),
    CONSTRAINT chk_youtube_notification_delivery_ledger_time_order CHECK (((updated_at >= first_recorded_at) AND ((sent_at IS NULL) OR (sent_at >= first_recorded_at)) AND ((quarantined_at IS NULL) OR (quarantined_at >= first_recorded_at))))
);


--
-- Name: youtube_notification_delivery_telemetry; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_notification_delivery_telemetry (
    id bigint NOT NULL,
    delivery_id bigint NOT NULL,
    attempt_ordinal integer CONSTRAINT youtube_notification_delivery_telemetr_attempt_ordinal_not_null NOT NULL,
    outbox_id bigint NOT NULL,
    channel_id character varying(64) NOT NULL,
    content_id character varying(50) NOT NULL,
    room_id character varying(100) NOT NULL,
    alarm_type text NOT NULL,
    dedupe_key text NOT NULL,
    delivery_mode character varying(20) NOT NULL,
    send_result character varying(20) NOT NULL,
    failure_reason character varying(100),
    event_at timestamp with time zone NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() CONSTRAINT youtube_notification_delivery_telemetr_next_attempt_at_not_null NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_at timestamp with time zone,
    logged_at timestamp with time zone,
    error text,
    delivery_path character varying(100) DEFAULT 'youtube_outbox_dispatcher'::character varying NOT NULL,
    post_id character varying(50) NOT NULL,
    attempt_started_at timestamp with time zone,
    attempt_finished_at timestamp with time zone,
    actual_published_at timestamp with time zone,
    detected_at timestamp with time zone,
    alarm_sent_at timestamp with time zone,
    alarm_latency_millis bigint,
    CONSTRAINT chk_youtube_notification_delivery_telemetry_alarm_type_vocab CHECK ((alarm_type = ANY (ARRAY[('LIVE'::character varying)::text, ('COMMUNITY'::character varying)::text, ('SHORTS'::character varying)::text, ('BIRTHDAY'::character varying)::text, ('ANNIVERSARY'::character varying)::text])))
)
WITH (autovacuum_vacuum_scale_factor='0.02', autovacuum_vacuum_threshold='50', autovacuum_analyze_scale_factor='0.02', autovacuum_analyze_threshold='50');


--
-- Name: youtube_notification_delivery_telemetry_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.youtube_notification_delivery_telemetry_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: youtube_notification_delivery_telemetry_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.youtube_notification_delivery_telemetry_id_seq OWNED BY public.youtube_notification_delivery_telemetry.id;


--
-- Name: youtube_notification_outbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_notification_outbox (
    id bigint NOT NULL,
    kind text NOT NULL,
    channel_id character varying(64) NOT NULL,
    content_id character varying(50) NOT NULL,
    payload jsonb NOT NULL,
    status text DEFAULT 'PENDING'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_at timestamp with time zone,
    sent_at timestamp with time zone,
    error text,
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    terminal_at timestamp with time zone,
    CONSTRAINT chk_youtube_notification_outbox_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text]))),
    CONSTRAINT chk_youtube_notification_outbox_status_vocab CHECK ((status = ANY (ARRAY[('PENDING'::character varying)::text, ('SENT'::character varying)::text, ('FAILED'::character varying)::text])))
)
WITH (autovacuum_vacuum_scale_factor='0.02', autovacuum_vacuum_threshold='50', autovacuum_analyze_scale_factor='0.02', autovacuum_analyze_threshold='50');


--
-- Name: youtube_notification_outbox_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.youtube_notification_outbox_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: youtube_notification_outbox_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.youtube_notification_outbox_id_seq OWNED BY public.youtube_notification_outbox.id;


--
-- Name: youtube_notification_send_request; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_notification_send_request (
    base_id text NOT NULL,
    room_id character varying(100) NOT NULL,
    message text NOT NULL,
    message_hash text NOT NULL,
    route text NOT NULL,
    dedupe_keys text[] NOT NULL,
    member_ids bigint[] NOT NULL,
    generation integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    handoff_request_id text,
    handoff_next_check_at timestamp with time zone,
    handoff_checked_at timestamp with time zone,
    CONSTRAINT chk_youtube_send_request_handoff_shape CHECK ((((handoff_request_id IS NULL) OR ((length(handoff_request_id) >= 1) AND (length(handoff_request_id) <= 256))) AND ((handoff_next_check_at IS NULL) OR ((handoff_request_id IS NOT NULL) AND (handoff_checked_at IS NULL))) AND ((handoff_checked_at IS NULL) OR (handoff_request_id IS NOT NULL)) AND ((handoff_request_id IS NULL) OR (handoff_checked_at IS NOT NULL) OR (handoff_next_check_at IS NOT NULL)))),
    CONSTRAINT youtube_notification_send_request_dedupe_keys_check CHECK ((cardinality(dedupe_keys) > 0)),
    CONSTRAINT youtube_notification_send_request_generation_check CHECK (((generation >= 0) AND (generation <= 2))),
    CONSTRAINT youtube_notification_send_request_member_ids_check CHECK ((cardinality(member_ids) > 0)),
    CONSTRAINT youtube_notification_send_request_route_check CHECK ((route = ANY (ARRAY['text'::text, 'markdown'::text, 'richtext'::text, 'sender'::text])))
);


--
-- Name: youtube_schedule_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_schedule_items (
    group_key text NOT NULL,
    provider text NOT NULL,
    external_id text NOT NULL,
    video_id text DEFAULT ''::text NOT NULL,
    channel_id text DEFAULT ''::text NOT NULL,
    title text NOT NULL,
    scheduled_at timestamp with time zone NOT NULL,
    ended_at timestamp with time zone,
    is_live boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    collabo_talent_names text[] DEFAULT '{}'::text[] NOT NULL,
    observed_at timestamp with time zone,
    CONSTRAINT chk_youtube_schedule_item_bounds CHECK ((((length(group_key) >= 1) AND (length(group_key) <= 256)) AND ((length(external_id) >= 1) AND (length(external_id) <= 256)) AND (length(video_id) <= 128) AND (length(channel_id) <= 256) AND ((length(title) >= 1) AND (length(title) <= 4096)))),
    CONSTRAINT chk_youtube_schedule_item_collabo_talent_names CHECK (public.youtube_schedule_collabo_talent_names_valid(collabo_talent_names)),
    CONSTRAINT chk_youtube_schedule_item_provider CHECK ((provider = ANY (ARRAY['youtubejs'::text, 'holodex'::text, 'hololive_official'::text])))
);


--
-- Name: COLUMN youtube_schedule_items.collabo_talent_names; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.youtube_schedule_items.collabo_talent_names IS 'Official schedule collaboTalents[].name evidence. Empty means solo or no guests. Not used for owner identity.';


--
-- Name: youtube_video_availability; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_video_availability (
    video_id character varying(20) NOT NULL,
    channel_id character varying(64) NOT NULL,
    provider text NOT NULL,
    identity_confirmed boolean NOT NULL,
    availability text NOT NULL,
    method text NOT NULL,
    unknown_reason text,
    observation_id bigint,
    evidence_sha256 text NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    effective_at timestamp with time zone NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    received_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_video_availability_availability_vocab CHECK ((availability = ANY (ARRAY['PUBLIC'::text, 'MEMBERS_ONLY'::text, 'PUBLIC_UNAVAILABLE'::text, 'UNKNOWN'::text]))),
    CONSTRAINT chk_youtube_video_availability_effective_clock CHECK ((effective_at = scheduled_for)),
    CONSTRAINT chk_youtube_video_availability_hash CHECK ((evidence_sha256 ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT chk_youtube_video_availability_identity CHECK ((((length((video_id)::text) >= 1) AND (length((video_id)::text) <= 20)) AND ((length((channel_id)::text) >= 1) AND (length((channel_id)::text) <= 64)))),
    CONSTRAINT chk_youtube_video_availability_method_vocab CHECK ((method = ANY (ARRAY['player_public'::text, 'player_members_only'::text, 'player_private'::text, 'unknown'::text]))),
    CONSTRAINT chk_youtube_video_availability_provider CHECK ((provider = 'youtubejs'::text)),
    CONSTRAINT chk_youtube_video_availability_shape CHECK (((((availability = 'PUBLIC'::text) AND (method = 'player_public'::text)) OR ((availability = 'MEMBERS_ONLY'::text) AND (method = 'player_members_only'::text)) OR ((availability = 'PUBLIC_UNAVAILABLE'::text) AND (method = 'player_private'::text)) OR ((availability = 'UNKNOWN'::text) AND (method = 'unknown'::text))) AND ((availability = 'UNKNOWN'::text) = (unknown_reason IS NOT NULL)) AND ((availability = 'UNKNOWN'::text) OR identity_confirmed) AND ((unknown_reason IS DISTINCT FROM 'availability_unclassified'::text) OR identity_confirmed) AND ((unknown_reason IS NULL) OR (unknown_reason <> ALL (ARRAY['identity_missing'::text, 'identity_mismatch'::text])) OR (NOT identity_confirmed)))),
    CONSTRAINT chk_youtube_video_availability_unknown_reason_vocab CHECK (((unknown_reason IS NULL) OR (unknown_reason = ANY (ARRAY['identity_missing'::text, 'identity_mismatch'::text, 'contradictory_fields'::text, 'structure_unrecognized'::text, 'not_waiting_state'::text, 'login_required_unclassified'::text, 'error_unclassified'::text, 'request_failed'::text, 'availability_unclassified'::text]))))
);


--
-- Name: youtube_videos; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_videos (
    video_id character varying(20) NOT NULL,
    channel_id character varying(64) NOT NULL,
    title character varying(500) NOT NULL,
    thumbnail jsonb,
    duration character varying(20),
    published_text character varying(100),
    published_at timestamp with time zone,
    is_short boolean DEFAULT false NOT NULL,
    is_live_replay boolean DEFAULT false NOT NULL,
    view_count bigint DEFAULT 0,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: acl_rooms id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.acl_rooms ALTER COLUMN id SET DEFAULT nextval('public.acl_rooms_id_seq'::regclass);


--
-- Name: acl_settings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.acl_settings ALTER COLUMN id SET DEFAULT nextval('public.acl_settings_id_seq'::regclass);


--
-- Name: alarm_dispatch_admin_actions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_admin_actions ALTER COLUMN id SET DEFAULT nextval('public.alarm_dispatch_admin_actions_id_seq'::regclass);


--
-- Name: alarm_dispatch_deliveries id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_deliveries ALTER COLUMN id SET DEFAULT nextval('public.alarm_dispatch_deliveries_id_seq'::regclass);


--
-- Name: alarm_dispatch_event_collisions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_event_collisions ALTER COLUMN id SET DEFAULT nextval('public.alarm_dispatch_event_collisions_id_seq'::regclass);


--
-- Name: alarm_dispatch_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_events ALTER COLUMN id SET DEFAULT nextval('public.alarm_dispatch_events_id_seq'::regclass);


--
-- Name: alarm_dispatch_send_units id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_send_units ALTER COLUMN id SET DEFAULT nextval('public.alarm_dispatch_send_units_id_seq'::regclass);


--
-- Name: alarms id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarms ALTER COLUMN id SET DEFAULT nextval('public.alarms_id_seq'::regclass);


--
-- Name: bot_command_executions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_command_executions ALTER COLUMN id SET DEFAULT nextval('public.bot_command_executions_id_seq'::regclass);


--
-- Name: bot_reply_outbox id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox ALTER COLUMN id SET DEFAULT nextval('public.bot_reply_outbox_id_seq'::regclass);


--
-- Name: bot_reply_outbox_replay_audit id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox_replay_audit ALTER COLUMN id SET DEFAULT nextval('public.bot_reply_outbox_replay_audit_id_seq'::regclass);


--
-- Name: bot_reply_outbox_resolution_audit id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox_resolution_audit ALTER COLUMN id SET DEFAULT nextval('public.bot_reply_outbox_resolution_audit_id_seq'::regclass);


--
-- Name: bot_webhook_inbox id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_webhook_inbox ALTER COLUMN id SET DEFAULT nextval('public.bot_webhook_inbox_id_seq'::regclass);


--
-- Name: major_event_subscriptions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.major_event_subscriptions ALTER COLUMN id SET DEFAULT nextval('public.major_event_subscriptions_id_seq'::regclass);


--
-- Name: major_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.major_events ALTER COLUMN id SET DEFAULT nextval('public.major_events_id_seq'::regclass);


--
-- Name: member_news_subscriptions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.member_news_subscriptions ALTER COLUMN id SET DEFAULT nextval('public.member_news_subscriptions_id_seq'::regclass);


--
-- Name: members id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.members ALTER COLUMN id SET DEFAULT nextval('public.members_id_seq'::regclass);


--
-- Name: message_strings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.message_strings ALTER COLUMN id SET DEFAULT nextval('public.message_strings_id_seq'::regclass);


--
-- Name: notification_delivery_outbox id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_delivery_outbox ALTER COLUMN id SET DEFAULT nextval('public.notification_delivery_outbox_id_seq'::regclass);


--
-- Name: notification_template_revisions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_template_revisions ALTER COLUMN id SET DEFAULT nextval('public.notification_template_revisions_id_seq'::regclass);


--
-- Name: notification_templates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_templates ALTER COLUMN id SET DEFAULT nextval('public.notification_templates_id_seq'::regclass);


--
-- Name: source_observation_applications id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_applications ALTER COLUMN id SET DEFAULT nextval('public.source_observation_applications_id_seq'::regclass);


--
-- Name: source_observation_collisions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_collisions ALTER COLUMN id SET DEFAULT nextval('public.source_observation_collisions_id_seq'::regclass);


--
-- Name: source_observations id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observations ALTER COLUMN id SET DEFAULT nextval('public.source_observations_id_seq'::regclass);


--
-- Name: source_reconciliation_conflicts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_reconciliation_conflicts ALTER COLUMN id SET DEFAULT nextval('public.source_reconciliation_conflicts_id_seq'::regclass);


--
-- Name: youtube_notification_delivery id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_delivery ALTER COLUMN id SET DEFAULT nextval('public.youtube_notification_delivery_id_seq'::regclass);


--
-- Name: youtube_notification_delivery_telemetry id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_delivery_telemetry ALTER COLUMN id SET DEFAULT nextval('public.youtube_notification_delivery_telemetry_id_seq'::regclass);


--
-- Name: youtube_notification_outbox id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_outbox ALTER COLUMN id SET DEFAULT nextval('public.youtube_notification_outbox_id_seq'::regclass);


--
-- Data for Name: acl_rooms; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: acl_settings; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_dispatch_admin_actions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_dispatch_closeout_receipts; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_dispatch_deliveries; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_dispatch_event_collisions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_dispatch_events; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_dispatch_send_units; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_room_display_names; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_upcoming_candidates; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarms; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: bot_command_executions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: bot_reply_outbox; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: bot_reply_outbox_replay_audit; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: bot_reply_outbox_resolution_audit; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: bot_webhook_heads; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: bot_webhook_inbox; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: kakao_rooms; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: major_event_subscriptions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: major_events; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: member_news_subscriptions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: members; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.members VALUES
	(2, 'sameko-saba', 'UCxsZ6NCzjU_t4YSxQLBcM5A', 'Sameko Saba', '鮫子サバ', '사메코 사바', 'active', false, '{"ja": ["サバ", "鮫子"], "ko": ["사바", "사메코"]}', NULL, NULL, 'Independents', NULL, 'manual', NULL, NULL, '사바', NULL, NULL, '{}', NULL),
	(1, 'yuuki-sakuna', 'UCrV1Hf5r8P148idjoSfrGEQ', 'Yuuki Sakuna', '結城さくな', '유우키 사쿠나', 'active', false, '{"ja": ["さくな", "さくたん"], "ko": ["사쿠나", "사쿠탄"]}', NULL, NULL, 'Independents', NULL, 'manual', NULL, NULL, '사쿠나', NULL, NULL, '{}', NULL),
	(10, 'hanako-nana', 'UCcA21_PzN1EhNe7xS4MJGsQ', 'Hanako Nana', '華子ナナ', '하나코 나나', 'active', false, '{"ja": ["華子", "ナナ"], "ko": ["나나", "나교수님", "77년생", "쌍칠아재", "굴리트"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '4d812b586ff63f8a2946e64fa860bbf5', NULL, '나나', NULL, NULL, '{}', NULL),
	(8, 'akane-lize', 'UC9m5xP6u69zXpD7MscY-uYQ', 'Akane Lize', '朱音リゼ', '아카네 리제', 'active', false, '{"ja": ["朱音", "リゼ"], "ko": ["리제", "리제황녀", "피엔나", "저챗퀸", "천마"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '4325b1d5bbc321fad3042306646e2e50', NULL, '리제', NULL, NULL, '{}', NULL),
	(4, 'ayatsuno-yuni', 'UClbYIn9LDbbFZ9w2shX3K0g', 'Ayatsuno Yuni', '純角ユニ', '아야츠노 유니', 'active', false, '{"ja": ["純角", "ユニ"], "ko": ["유니", "유니링", "유니찌", "정윤희"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '45e71a76e949e16a34764deb962f9d9f', NULL, '유니', NULL, NULL, '{}', NULL),
	(3, 'airi-kanna', 'UC6YnTqZidFg4WUiXpiCtSSQ', 'Airi Kanna', '藍里かんな', '아이리 칸나', 'graduated', true, '{"ja": ["藍里", "かんな"], "ko": ["칸나", "대장용", "락용", "간나"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '82136e09328ffc9143924707293a566d', NULL, '칸나', NULL, NULL, '{}', NULL),
	(5, 'arahashi-tabi', 'UCq-U-D8O6_6e4X6r-z9V0w', 'Arahashi Tabi', '荒橋タビ', '아라하시 타비', 'active', false, '{"ja": ["荒橋", "タビ"], "ko": ["타비", "뿡댕이", "댕댕이", "닌자타비"]}', NULL, NULL, 'Stellive', NULL, 'holodex', 'a6c4ddb09cdb160478996007bff35296', NULL, '타비', NULL, NULL, '{}', NULL),
	(6, 'shirayuki-hina', 'UC99CUC6yR6O_uXyS_3K7yKA', 'Shirayuki Hina', '白雪ひな', '시라유키 히나', 'active', false, '{"ja": ["白雪", "ひな"], "ko": ["히나", "히나피", "공주", "흰눈곰", "존 히나"]}', NULL, NULL, 'Stellive', NULL, 'holodex', 'b044e3a3b9259246bc92e863e7d3f3b8', NULL, '히나', NULL, NULL, '{}', NULL),
	(7, 'neneko-mashiro', 'UC9o9D7U5O8V0A-zO0v7UeLw', 'Neneko Mashiro', '音々子ましろ', '네네코 마시로', 'active', false, '{"ja": ["音々子", "ましろ"], "ko": ["마시로", "시로", "밍대장", "밍"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '4515b179f86b67b4981e16190817c580', NULL, '마시로', NULL, NULL, '{}', NULL),
	(9, 'tenko-shibuki', 'UCYxLMfeX1CbMBll9MsGlzmw', 'Tenko Shibuki', '天鼓紫吹', '텐코 시부키', 'active', false, '{"ja": ["天鼓", "紫吹"], "ko": ["시부키", "부키", "북대장", "땡코 시부키"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '64d76089fba26b180d9c9e48a32600d9', NULL, '시부키', NULL, NULL, '{}', NULL),
	(11, 'tachibana-hinano', 'UCvUc0m317LWTTPZoBQV479A', 'Tachibana Hinano', '橘ひなの', '타치바나 히나노', 'active', false, '{"ja": ["橘ひなの", "ひなの"], "ko": ["타치바나 히나노", "히나노", "히나땅"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'hinanotachiba7', '히나노', NULL, NULL, '{}', NULL),
	(13, 'kaga-nazuna', 'UCiMG6VdScBabPhJ1ZtaVmbw', 'Kaga Nazuna', '花芽なずな', '카가 나즈나', 'active', false, '{"ja": ["花芽なずな", "なずな"], "ko": ["카가 나즈나", "나즈나"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'nazunakaga', '나즈나', NULL, NULL, '{}', NULL),
	(12, 'ichinose-uruha', 'UC5LyYg6cCA4yHEYvtUsir3g', 'Ichinose Uruha', '一ノ瀬うるは', '이치노세 우루하', 'active', false, '{"ja": ["一ノ瀬うるは", "うるは"], "ko": ["이치노세 우루하", "우루하"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'uruhaichinose', '우루하', NULL, NULL, '{}', NULL),
	(15, 'yakumo-beni', 'UCjXBuHmWkieBApgBhDuJMMQ', 'Yakumo Beni', '八雲べに', '야쿠모 베니', 'active', false, '{"ja": ["八雲べに", "べに"], "ko": ["야쿠모 베니", "베니"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'yakumobeni', '베니', NULL, NULL, '{}', NULL),
	(14, 'kaminari-qpi', 'UCMp55EbT_ZlqiMS3lCj01BQ', 'Kaminari Qpi', '神成きゅぴ', '카미나리 큐피', 'active', false, '{"ja": ["神成きゅぴ", "きゅぴ"], "ko": ["카미나리 큐피", "큐피"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'kaminariqpi', '큐피', NULL, NULL, '{}', NULL),
	(16, 'mekpark-achrora', 'UChpRPsAeSZn5DistGacR3iA', 'ACHRORA', 'ACHRORA', '아크로라', 'active', false, '{"ja": ["アクロラ", "ACHRORA"], "ko": ["아크로라", "ACHRORA", "멕파크", "mekPark"]}', NULL, NULL, 'mekPark', NULL, 'manual', NULL, NULL, NULL, NULL, NULL, '{}', NULL),
	(17, 'mekpark-unit-b', 'UC3OH5FKQ3qtl4uRme_vZTgA', 'Unit B', 'UNIT B', '유닛 B', 'active', false, '{"ja": ["UNIT B", "宵凪ネオン", "玲銘ミラ", "清澄ライラ"], "ko": ["유닛비", "유닛 B", "UNIT B", "멕파크", "mekPark"]}', NULL, NULL, 'mekPark', NULL, 'manual', NULL, NULL, NULL, NULL, NULL, '{}', NULL),
	(18, 'shigure-ui', 'UCt30jJgChL8qeT9VPadidSw', 'Shigure Ui', 'しぐれうい', '시구레 우이', 'active', false, '{"ja": ["しぐれうい", "ういママ", "うい"], "ko": ["시구레우이", "시구레 우이", "우이", "우이마마"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '우이', NULL, NULL, '{}', NULL),
	(19, 'holoan-room', 'UCozx5csNhCx1wsVq3SZVkBQ', 'holoAN', NULL, '홀로아나', 'active', false, '{"ja": ["ホロアナ"], "ko": ["홀로아나"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '홀로아나', NULL, NULL, '{}', NULL),
	(20, 'izuki-michiru', 'UCozx5csNhCx1wsVq3SZVkBQ', 'Izuki Michiru', '井月みちる', '이즈키 미치루', 'active', false, '{"ja": ["井月みちる", "みちる"], "ko": ["이즈키 미치루", "미치루"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '미치루', NULL, '2025-10-15', '{holoAN}', 'https://hololive.hololivepro.com/talents/izuki-michiru/'),
	(21, 'hanazono-sayaka', 'UCozx5csNhCx1wsVq3SZVkBQ', 'Hanazono Sayaka', '花園さやか', '하나조노 사야카', 'active', false, '{"ja": ["花園さやか", "さやか"], "ko": ["하나조노 사야카", "사야카"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '사야카', NULL, '2025-11-10', '{holoAN}', 'https://hololive.hololivepro.com/talents/hanazono-sayaka/'),
	(22, 'kazeshiro-yuki', 'UCozx5csNhCx1wsVq3SZVkBQ', 'Kazeshiro Yuki', '風白ゆき', '카제시로 유키', 'active', false, '{"ja": ["風白ゆき", "ゆき"], "ko": ["카제시로 유키", "유키"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '유키', NULL, '2025-12-30', '{holoAN}', 'https://hololive.hololivepro.com/talents/kazeshiro-yuki/'),
	(23, 'hyakuto-kyoko', 'UCSjQDxud2HkAO2DVD3lwxmw', 'Hyakuto Kyoko', '百灯キョーコ', '햐쿠토 쿄코', 'active', false, '{"ja": ["百灯キョーコ", "キョーコ", "ひゃくとうきょーこ"], "ko": ["햐쿠토 쿄코", "햐쿠토쿄코", "쿄코"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '쿄코', '2000-05-08', '2026-09-25', '{ASOBI★MAWARI-TAI!}', 'https://hololive.hololivepro.com/talents/hyakuto-kyoko/'),
	(24, 'achichi-mela', 'UC8eitCE9Z6EwUCs-VUi1blg', 'Achichi Mela', '熱千めら', '아치치 메라', 'active', false, '{"ja": ["熱千めら", "めら", "あちちめら"], "ko": ["아치치 메라", "아치치메라", "메라"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '메라', '2000-04-16', '2026-09-24', '{ASOBI★MAWARI-TAI!}', 'https://hololive.hololivepro.com/talents/achichi-mela/'),
	(25, 'suzuna-tsuzuri', 'UCy9mgxB8pn2C4aNK_MPthDQ', 'Suzuna Tsuzuri', '鈴鳴つづり', '스즈나 츠즈리', 'active', false, '{"ja": ["鈴鳴つづり", "つづり", "すずなつづり"], "ko": ["스즈나 츠즈리", "스즈나츠즈리", "츠즈리"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '츠즈리', '2000-06-28', '2026-09-25', '{ASOBI★MAWARI-TAI!}', 'https://hololive.hololivepro.com/talents/suzuna-tsuzuri/'),
	(26, 'sorashina-sopia', 'UCROQtXcp2loQEmvpe5rhJzQ', 'Sorashina Sopia', '宙科そぴあ', '소라시나 소피아', 'active', false, '{"ja": ["宙科そぴあ", "そぴあ", "そらしなそぴあ"], "ko": ["소라시나 소피아", "소라시나소피아", "소피아"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '소피아', '2000-06-13', '2026-09-24', '{ASOBI★MAWARI-TAI!}', 'https://hololive.hololivepro.com/talents/sorashina-sopia/'),
	(27, 'asobi-mawaritai', 'UCAHwWUotyS3l2qBetFDsjgQ', 'ASOBI★MAWARI-TAI!', 'アソビ★まわり隊！', '아소비★마와리타이!', 'active', false, '{"ja": ["アソビ★まわり隊！", "アソビまわり隊"], "ko": ["아소비", "아소비 마와리타이", "아소비마와리타이", "아소비★마와리타이!"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '아소비', NULL, NULL, '{ASOBI★MAWARI-TAI!}', 'https://hololive.hololivepro.com/special/22482/');


--
-- Data for Name: message_strings; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.message_strings VALUES
	(1, 'org', 'Hololive', 'Holo', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(2, 'org', 'Nijisanji', '니지산지', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(3, 'org', 'Independents', '개인세', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(4, 'org', 'Stellive', '스텔라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(5, 'alarmtype', 'LIVE', '방송', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(6, 'alarmtype', 'COMMUNITY', '커뮤니티', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(7, 'alarmtype', 'SHORTS', '쇼츠', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(8, 'alarmtype', 'BIRTHDAY', '생일', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(9, 'alarmtype', 'ANNIVERSARY', '주년', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(10, 'alarmtype', 'ALL', '전체', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(11, 'newscat', 'birthday_live', '생일 라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(12, 'newscat', 'solo_live', '솔로 라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(13, 'newscat', 'collab', '콜라보', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(14, 'newscat', 'event', '이벤트', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(15, 'newscat', 'goods', '굿즈', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(16, 'newscat', 'other', '기타', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(17, 'social', '歌の再生リスト', '음악 플레이리스트', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(18, 'social', '公式グッズ', '공식 굿즈', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(19, 'social', 'オフィシャルグッズ', '공식 굿즈', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(20, 'misc', 'vtuber_fallback', 'VTuber', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(21, 'misc', 'chzzk_title', '치지직 라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(22, 'error', 'no_member_info_found', '❌ 등록된 멤버 정보를 찾을 수 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(67, 'misc', 'time_unknown', '시간 미정', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(69, 'misc', 'alarm_no_title', '제목 없음', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(70, 'misc', 'alarm_no_stream', '방송 정보 없음', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(74, 'calendar', 'day', '%d월 %d일', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(71, 'calendar', 'header_month', '%d년 %d월 기념일', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(72, 'calendar', 'summary', '총 %d건 · 생일 %d · 데뷔주년 %d', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(73, 'calendar', 'empty', '등록된 기념일이 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(75, 'calendar', 'badge_birthday', '생일', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(76, 'calendar', 'badge_anniversary', '데뷔 %d주년', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(77, 'calendar', 'unknown', '알 수 없음', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(23, 'error', 'cannot_display_member_info', '❌ 멤버 정보를 표시할 수 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(24, 'error', 'member_profile_load_failed', '❌ 프로필을 불러오는 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(25, 'error', 'member_profile_build_failed', '❌ 프로필을 구성하지 못했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(26, 'error', 'graduated_member_blocked', '⚠️ 졸업한 멤버입니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(27, 'error', 'alarm_service_not_initialized', '❌ 알람 서비스가 초기화되지 않았습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(28, 'error', 'alarm_add_failed', '❌ 알람 설정 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(29, 'error', 'alarm_remove_failed', '❌ 알람 제거 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(30, 'error', 'alarm_list_failed', '❌ 알람 목록 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(31, 'error', 'alarm_clear_failed', '❌ 알람 초기화 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(32, 'error', 'alarm_need_member_name_add', '❌ 멤버 이름을 입력해주세요.
예) !알람 추가 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(33, 'error', 'alarm_need_member_name_remove', '❌ 멤버 이름을 입력해주세요.
예) !알람 제거 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(34, 'error', 'invalid_alarm_usage', '❌ 지원하지 않는 알람 명령입니다.
예) !알람 추가 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(35, 'error', 'live_stream_query_failed', '❌ 라이브 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(36, 'error', 'upcoming_stream_query_failed', '❌ 예정 방송 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(37, 'error', 'schedule_query_failed', '❌ 일정 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(38, 'error', 'schedule_need_member_name', '❌ 멤버 이름을 입력해주세요.
예) !일정 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(45, 'error', 'calendar_query_failed', '❌ 기념일 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(46, 'error', 'major_event_service_not_initialized', '❌ 행사 알림 서비스가 초기화되지 않았습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(47, 'error', 'major_event_status_check_failed', '❌ 행사 알림 상태 확인 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(48, 'error', 'major_event_subscribe_failed', '❌ 행사 알림 설정 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(49, 'error', 'major_event_unsubscribe_failed', '❌ 행사 알림 해제 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(50, 'error', 'member_news_service_not_initialized', '❌ 뉴스 서비스가 초기화되지 않았습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(51, 'error', 'member_news_query_failed', '❌ 뉴스 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(52, 'error', 'member_news_subscription_failed', '❌ 뉴스 알림 설정 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(53, 'error', 'unknown_command', '❌ 알 수 없는 명령입니다.
!도움말에서 사용 가능한 명령을 확인할 수 있습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(56, 'error', 'iris_connection_failed', '❌ 서버 연결에 실패했습니다. 잠시 후 다시 시도해주세요.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(57, 'error', 'command_processing_failed', '❌ 명령 처리 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(58, 'error', 'async_command_backpressure', '❌ 요청이 많아 잠시 후 다시 시도해주세요.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(59, 'notify', 'member_news_no_members', '📰 뉴스 대상 멤버가 없습니다.
예) !알람 추가 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(60, 'notify', 'member_news_subscribed', '✅ 뉴스 알림을 켰습니다.
매주 월요일 09:00 KST에 발송됩니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(61, 'notify', 'member_news_already_subscribed', 'ℹ️ 뉴스 알림이 이미 켜져 있습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(62, 'notify', 'member_news_unsubscribed', '✅ 뉴스 알림을 껐습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(63, 'notify', 'member_news_not_subscribed', 'ℹ️ 뉴스 알림이 이미 꺼져 있습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(64, 'notify', 'member_news_status_on', '🔔 뉴스 알림: 켜짐
- 발송: 매주 월요일 09:00 KST
- 해제: !뉴스알림 끄기', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(65, 'notify', 'member_news_status_off', '🔕 뉴스 알림: 꺼짐
- 설정: !뉴스알림 켜기', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(66, 'notify', 'graduated_member_warning', '⚠️ 졸업한 멤버입니다.

', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(123, 'timefmt', 'stream_time_days', '%s (%d일 후)', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(124, 'timefmt', 'stream_time_hours_minutes', '%s (%d시간 %d분 후)', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(125, 'timefmt', 'stream_time_minutes', '%s (%d분 후)', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(126, 'timefmt', 'relative_days', '%d일 후', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(127, 'timefmt', 'relative_hours_minutes', '%d시간 %d분 후', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(128, 'timefmt', 'relative_minutes', '%d분 후', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(129, 'karing', 'alarm_title_prelive', '방송 %d분 전 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(130, 'karing', 'alarm_title_live', '라이브 시작', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(131, 'karing', 'time_left_prelive', '%d분 후 시작', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(132, 'karing', 'time_left_live', '지금 시작', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(133, 'karing', 'outbox_title_community', '커뮤니티 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(134, 'karing', 'outbox_time_community', '새 커뮤니티', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(135, 'karing', 'outbox_title_shorts', '쇼츠 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(136, 'karing', 'outbox_time_shorts', '새 쇼츠', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(137, 'karing', 'outbox_title_video', '새 영상', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(138, 'karing', 'outbox_time_video', '새 영상', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(139, 'karing', 'outbox_title_live', '방송 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(140, 'karing', 'outbox_time_live', '방송 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(141, 'karing', 'title_fallback', '알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(142, 'karing', 'time_fallback', '새 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(143, 'karing', 'count_suffix', '%s · %d건', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(144, 'karing', 'item_title_community_fallback', '커뮤니티 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(145, 'karing', 'status_community', '커뮤니티', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(146, 'karing', 'status_shorts', '쇼츠', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(147, 'karing', 'status_video', '새 영상', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(148, 'karing', 'status_fallback', '알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(149, 'calendar', 'overflow_footer', '외 %d건 생략', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(150, 'livecard', 'header', '현재 라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(151, 'livecard', 'summary', '총 %d건', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(152, 'livecard', 'badge_chzzk', '치지직', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(153, 'livecard', 'overflow_footer', '외 %d건 생략', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(154, 'profilecard', 'badge_graduated', '졸업', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(155, 'rankcard', 'header', '구독자 증가 순위', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(156, 'rankcard', 'summary', '%s · 상위 %d', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(157, 'rankcard', 'total', '구독자 %s', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(158, 'karing', 'alarm_title_prelive_premiere', '선행공개 %d분 전 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(159, 'karing', 'alarm_title_live_premiere', '선행공개 시작', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00'),
	(160, 'karing', 'outbox_title_video_premiere', '%d분 후 공개 예정', '2026-11-09 05:14:20.71582+00', '2026-11-09 05:14:20.71582+00'),
	(161, 'karing', 'outbox_time_video_premiere', '%d분 후 공개', '2026-11-09 05:14:20.71582+00', '2026-11-09 05:14:20.71582+00'),
	(162, 'karing', 'status_video_premiere', '최초공개', '2026-11-09 05:14:20.71582+00', '2026-11-09 05:14:20.71582+00');


--
-- Data for Name: notification_delivery_outbox; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: notification_template_revisions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: notification_templates; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.notification_templates VALUES
	(201, 'CMD_MAJOR_EVENT_NOT_SUB', NULL, 'ℹ️ 행사 알림이 이미 꺼져 있습니다.', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:49.286172+00', 2, 'richtext'),
	(202, 'CMD_MAJOR_EVENT_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 · 매월 1일 10:00 KST
해제: {{mdescape .Prefix}}행사 끄기
{{- else}}
설정: {{mdescape .Prefix}}행사 켜기
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:49.286172+00', 2, 'richtext'),
	(196, 'CMD_LIVE_STREAMS', NULL, '{{- if eq .Count 0 -}}
현재 방송 중인 멤버가 없습니다.
{{- else -}}
🔴 **방송 중** · {{.Count}}개
{{- $names := 0}}
{{- range .Streams}}{{with displayline .ChannelName}}{{if $names}} · {{else}}
{{end}}{{mdsafe .}}{{$names = add $names 1}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .ChannelName}}**{{mdsafe .}}**{{end}}{{- if .Elapsed}} · {{.Elapsed}}{{- end}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:48.715959+00', 3, 'richtext'),
	(200, 'CMD_MAJOR_EVENT_SUBSCRIBED', NULL, '✅ 행사 알림을 켰습니다.
발송: 매주 월요일 09:00 · 매월 1일 10:00 KST', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:49.286172+00', 2, 'richtext'),
	(192, 'OUTBOX_COMMUNITY_GROUP', NULL, '🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 커뮤니티 글 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (displayline $item.ContentText) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{with displayline $item.ContentText}}{{mdsafe (truncate 100 .)}}{{else}}커뮤니티 글{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(193, 'CMD_ALARM_ADDED', NULL, '{{- if .Added -}}
✅ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람을 설정했습니다.
방송 시작 5분 전에 알립니다.
{{- else -}}
ℹ️ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람이 이미 설정되어 있습니다.
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(194, 'CMD_MEMBER_DIRECTORY', NULL, '{{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
👤 **멤버 목록** · {{.Total}}명
{{- range $i, $group := .Groups}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .GroupName}}**{{mdsafe .}}**{{end}}
{{- range .Members}}
{{- if or (displayline .Primary) (displayline .Secondary)}}
· {{if and .ShowBoth (displayline .Primary) (displayline .Secondary)}}{{mdsafe (displayline .Primary)}} ({{mdsafe (displayline .Secondary)}}){{else if displayline .Primary}}{{mdsafe (displayline .Primary)}}{{else if displayline .Secondary}}{{mdsafe (displayline .Secondary)}}{{end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(195, 'OUTBOX_VIDEO_GROUP', NULL, '{{if eq .Kind "LIVE_STREAM"}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 방송 시작{{else if eq .Kind "NEW_VIDEO"}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 영상{{else}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알림{{end}} · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (displayline $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{with displayline $item.Title}}{{mdsafe (truncate 64 .)}}{{else}}영상{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(197, 'CMD_MEMBER_NOT_LIVE', NULL, '{{with displayline .MemberName}}**{{mdsafe .}}**{{end}}: 현재 방송 중이 아닙니다.', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(199, 'OUTBOX_SHORTS_GROUP', NULL, '🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 쇼츠 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (displayline $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{with displayline $item.Title}}{{mdsafe (truncate 64 .)}}{{else}}쇼츠{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(203, 'CMD_MAJOR_EVENT_USAGE', NULL, '🔔 **행사 알림 명령어**
{{mdescape .Prefix}}행사 켜기 / 끄기 / 상태', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(50, 'CMD_AMBIGUOUS_MEMBER', NULL, '같은 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}} · {{mdsafe (displayline .Name)}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (displayline .FirstName))}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 4, 'plain'),
	(33, 'CMD_MEMBER_NEWS_DIGEST', NULL, '{{- if trim .Headline -}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Headline))}}
{{- else -}}
📰 멤버 뉴스
{{- end}}
{{- if eq (len .TopItems) 0}}
표시할 뉴스가 없습니다.
{{- else}}
{{- range $i, $item := .TopItems}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline $item.Member}}{{mdsafe .}}{{else}}뉴스{{end}}{{if $item.DateText}} · {{mdsafe (displayline $item.DateText)}}{{end}}{{if $item.Category}} · {{mdsafe (displayline $item.Category)}}{{end}}
{{- if trim $item.Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline $item.Title))}}
{{- end}}
{{- if $item.Summary}}
{{printf "\u200b"}}{{mdsafe (displayline $item.Summary)}}
{{- end}}
{{- if $item.SourceURL}}
{{$item.SourceURL}}
{{- end}}
{{- end}}
{{- if .MoreSummary}}

{{printf "\u200b"}}{{replace (replace (mdsafe .MoreSummary) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 5, 'plain'),
	(45, 'CMD_MEMBER_NO_UPCOMING', NULL, '📅 {{mdsafe (displayline .MemberName)}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 4, 'plain'),
	(40, 'CMD_PROFILE', NULL, '{{- if eq (len .Names) 0 -}}
👤 멤버 정보
{{- else -}}
👤 {{mdsafe (displayline (index .Names 0))}}
{{- if gt (len .Names) 1}}
별칭: {{mdsafe (displayline (join (slice .Names 1) " / "))}}
{{- end}}
{{- end}}
{{- if .Catchphrase}}
"{{mdsafe (displayline .Catchphrase)}}"
{{- end}}
{{- if .Summary}}

{{printf "\u200b"}}{{replace (replace (mdsafe .Summary) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if .Highlights}}

[하이라이트]
{{- range .Highlights}}
· {{mdsafe (displayline .)}}
{{- end}}
{{- end}}
{{- if .DataRows}}

[프로필]
{{- range .DataRows}}
{{- if .Multiline}}
{{mdsafe (displayline .Label)}}:
{{printf "\u200b"}}{{replace (replace (mdsafe .Value) "\r\n" "\n") "\n" "\n\u200b"}}
{{- else}}
{{mdsafe (displayline .Label)}}: {{mdsafe (displayline .Value)}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

[링크]
{{- range $i, $link := .SocialLinks}}
{{if gt $i 0}}
{{end}}{{mdsafe (displayline .Label)}}
{{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

공식 프로필
{{.OfficialURL}}
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(49, 'CELEBRATION_ANNIVERSARY', NULL, '🎉 {{mdsafe (displayline .MemberName)}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(9, 'CMD_ALARM_LIST', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 설정된 알람 · {{.Count}}개
{{- range $i, $alarm := .Alarms}}
{{if eq $i 0}}
{{end}}{{add $i 1}} · {{mdsafe (displayline $alarm.MemberName)}}{{if $alarm.TypesLabel}} ({{mdsafe (displayline $alarm.TypesLabel)}}){{end}}
{{- end -}}
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 5, 'plain'),
	(46, 'CMD_MEMBER_NOT_FOUND', NULL, '❌ ''{{mdsafe (displayline .MemberName)}}'' 멤버를 찾을 수 없습니다.', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(103, 'CELEBRATION_BIRTHDAY_STREAM', NULL, '🎂 {{mdsafe (displayline .MemberName)}} 생일 방송{{if .ScheduledStartKST}} · {{mdsafe .ScheduledStartKST}}{{end}}
{{- if trim .StreamTitle}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .StreamTitle))}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 4, 'plain'),
	(204, 'CMD_MEMBER_NEWS_NO_MEMBERS', NULL, '📰 뉴스 대상 멤버가 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(26, 'CMD_MAJOR_EVENT_UNSUBSCRIBED', NULL, '✅ 행사 알림을 껐습니다.', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(27, 'CMD_MAJOR_EVENT_ALREADY_SUB', NULL, 'ℹ️ 행사 알림이 이미 켜져 있습니다.', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(36, 'CMD_MEMBER_NEWS_UNSUBSCRIBED', NULL, '✅ 뉴스 알림을 껐습니다.', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(37, 'CMD_MEMBER_NEWS_ALREADY_SUB', NULL, 'ℹ️ 뉴스 알림이 이미 켜져 있습니다.', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(38, 'CMD_MEMBER_NEWS_NOT_SUB', NULL, 'ℹ️ 뉴스 알림이 이미 꺼져 있습니다.', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(43, 'CMD_CALENDAR', NULL, '{{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.
{{- else -}}
📅 {{.Year}}년 {{.Month}}월 기념일 · {{.Count}}개
{{- range $i, $day := .Days}}
{{if gt $i 0}}──────────{{end}}
[{{printf "%02d/%02d" .Month .Day}}]
{{- range .Entries}}
{{if .IsBirthday}}🎂 {{mdsafe (displayline .Name)}} 생일{{else}}🎉 {{mdsafe (displayline .Name)}} 데뷔 {{.Years}}주년{{end}}
{{- end}}
{{- end}}
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 4, 'plain'),
	(162, 'X_SPACE_STARTED', NULL, '🔴 {{mdsafe (displayline .MemberName)}} 스페이스 시작
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}', '2026-11-09 05:14:22.911783+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(17, 'CMD_ALARM_REMOVED', NULL, '{{- if .Removed -}}
✅ {{mdsafe (displayline .MemberName)}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{mdsafe (displayline .MemberName)}} 알람이 설정되어 있지 않습니다.
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(48, 'CELEBRATION_BIRTHDAY', NULL, '🎂 {{mdsafe (displayline .MemberName)}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(18, 'CMD_ALARM_CLEARED', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 {{.Count}}개를 모두 해제했습니다.
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(35, 'CMD_MEMBER_NEWS_SUBSCRIBED', NULL, '✅ 뉴스 알림을 켰습니다.
발송: 매주 월요일 09:00 KST', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(51, 'ALARM_DISPATCH_NOTIFICATION', NULL, '{{if .IsStarting}}🔴 {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{mdsafe (displayline .MemberName)}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{replace (replace (mdsafe .ScheduleMessage) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if .URL}}
{{with trustedlink .URL}}{{.}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace $.URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 4, 'plain'),
	(7, 'OUTBOX_VIDEO', NULL, '{{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (displayline .MemberName)}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{mdsafe (displayline .MemberName)}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{mdsafe (displayline .MemberName)}} 최초공개{{else}}🔔 {{mdsafe (displayline .MemberName)}} 새 영상{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(28, 'CMD_MAJOR_EVENT_NOT_SUB', NULL, 'ℹ️ 행사 알림이 이미 꺼져 있습니다.', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 4, 'plain'),
	(39, 'CMD_MEMBER_NEWS_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 KST
해제: {{mdescape .Prefix}}뉴스알림 끄기
{{- else}}
설정: {{mdescape .Prefix}}뉴스알림 켜기
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(25, 'CMD_MAJOR_EVENT_SUBSCRIBED', NULL, '✅ 행사 알림을 켰습니다.
발송: 매주 월요일 09:00 · 매월 1일 10:00 KST', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 4, 'plain'),
	(163, 'CMD_MAJOR_EVENT_UNSUBSCRIBED', NULL, '✅ 행사 알림을 껐습니다.', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(164, 'CMD_MAJOR_EVENT_ALREADY_SUB', NULL, 'ℹ️ 행사 알림이 이미 켜져 있습니다.', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(165, 'CMD_MEMBER_NEWS_UNSUBSCRIBED', NULL, '✅ 뉴스 알림을 껐습니다.', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(166, 'CMD_MEMBER_NEWS_ALREADY_SUB', NULL, 'ℹ️ 뉴스 알림이 이미 켜져 있습니다.', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(5, 'OUTBOX_SHORTS', NULL, '🔔 {{mdsafe (displayline .MemberName)}} 새 쇼츠
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(6, 'OUTBOX_COMMUNITY', NULL, '🔔 {{mdsafe (displayline .MemberName)}} 커뮤니티 글
{{- if trim .ContentText}}
{{printf "\u200b"}}{{mdsafe (truncate 100 (displayline .ContentText))}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(167, 'CMD_MEMBER_NEWS_NOT_SUB', NULL, 'ℹ️ 뉴스 알림이 이미 꺼져 있습니다.', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(23, 'OUTBOX_COMMUNITY_GROUP', NULL, '🔔 {{mdsafe (displayline .MemberName)}} 커뮤니티 글 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.ContentText) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.ContentText}}{{mdsafe (truncate 100 (displayline $item.ContentText))}}{{else}}커뮤니티 글{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 4, 'plain'),
	(16, 'CMD_ALARM_ADDED', NULL, '{{- if .Added -}}
✅ {{mdsafe (displayline .MemberName)}} 알람을 설정했습니다.
방송 시작 5분 전에 알립니다.
{{- else -}}
ℹ️ {{mdsafe (displayline .MemberName)}} 알람이 이미 설정되어 있습니다.
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 4, 'plain'),
	(168, 'CELEBRATION_BIRTHDAY_STREAM', NULL, '🎂 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 생일 방송{{if .ScheduledStartKST}} · {{mdsafe .ScheduledStartKST}}{{end}}
{{- with displayline .StreamTitle}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .StreamURL}}
{{.StreamURL}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(169, 'CMD_CALENDAR', NULL, '{{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.
{{- else -}}
📅 **{{.Year}}년 {{.Month}}월 기념일** · {{.Count}}개
{{- range $i, $day := .Days}}
{{if gt $i 0}}──────────{{end}}
**{{printf "%02d/%02d" .Month .Day}}**
{{- range .Entries}}
{{if .IsBirthday}}🎂 {{mdsafe (displayline .Name)}} 생일{{else}}🎉 {{mdsafe (displayline .Name)}} 데뷔 {{.Years}}주년{{end}}
{{- end}}
{{- end}}
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(170, 'X_SPACE_STARTED', NULL, '🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 스페이스 시작
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(171, 'CMD_ALARM_REMOVED', NULL, '{{- if .Removed -}}
✅ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람을 해제했습니다.
{{- else -}}
ℹ️ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 알람이 설정되어 있지 않습니다.
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(172, 'CELEBRATION_BIRTHDAY', NULL, '🎂 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}}{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(14, 'CMD_MEMBER_DIRECTORY', NULL, '{{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
👤 멤버 목록 · {{.Total}}명
{{- range $i, $group := .Groups}}
{{if gt $i 0}}──────────{{end}}
[{{mdsafe (displayline .GroupName)}}]
{{- range .Members}}
{{- if or .Primary .Secondary}}
· {{if .ShowBoth}}{{mdsafe (displayline .Primary)}} ({{mdsafe (displayline .Secondary)}}){{else if .Primary}}{{mdsafe (displayline .Primary)}}{{else if .Secondary}}{{mdsafe (displayline .Secondary)}}{{end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 4, 'plain'),
	(21, 'OUTBOX_VIDEO_GROUP', NULL, '{{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe (displayline .MemberName)}} 방송 시작{{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe (displayline .MemberName)}} 새 영상{{else}}🔔 {{mdsafe (displayline .MemberName)}} 알림{{end}} · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.Title}}{{mdsafe (truncate 64 (displayline $item.Title))}}{{else}}영상{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 4, 'plain'),
	(173, 'CMD_MEMBER_NEWS_DIGEST', NULL, '{{- with displayline .Headline -}}
**{{mdsafe (truncate 64 .)}}**
{{- else -}}
📰 **멤버 뉴스**
{{- end}}
{{- if eq (len .TopItems) 0}}
표시할 뉴스가 없습니다.
{{- else}}
{{- range $i, $item := .TopItems}}
{{if gt $i 0}}──────────{{end}}
{{with displayline $item.Member}}**{{mdsafe .}}**{{else}}**뉴스**{{end}}{{if $item.DateText}} · {{mdsafe (displayline $item.DateText)}}{{end}}{{if $item.Category}} · {{mdsafe (displayline $item.Category)}}{{end}}
{{- with displayline $item.Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if $item.Summary}}
{{mdsafe (displayline $item.Summary)}}
{{- end}}
{{- if $item.SourceURL}}
{{$item.SourceURL}}
{{- end}}
{{- end}}
{{- if .MoreSummary}}

{{replace (mdsafe .MoreSummary) "\r\n" "\n"}}
{{- end}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:43.331533+00', 2, 'richtext'),
	(11, 'CMD_LIVE_STREAMS', NULL, '{{- if eq .Count 0 -}}
현재 방송 중인 멤버가 없습니다.
{{- else -}}
🔴 방송 중 · {{.Count}}개
{{- $names := 0}}
{{- range .Streams}}{{with displayline .ChannelName}}{{if $names}} · {{else}}
{{printf "\u200b"}}{{end}}{{mdsafe .}}{{$names = add $names 1}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{mdsafe (displayline .ChannelName)}}{{- if .Elapsed}} · {{.Elapsed}}{{- end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 8, 'plain'),
	(174, 'CMD_PROFILE', NULL, '{{- if eq (len .Names) 0 -}}
👤 **멤버 정보**
{{- else -}}
👤 {{with displayline (index .Names 0)}}**{{mdsafe .}}**{{else}}**멤버 정보**{{end}}
{{- if gt (len .Names) 1}}
별칭: {{mdsafe (displayline (join (slice .Names 1) " / "))}}
{{- end}}
{{- end}}
{{- with displayline .Catchphrase}}
*"{{mdsafe .}}"*
{{- end}}
{{- if .Summary}}

{{replace (mdsafe .Summary) "\r\n" "\n"}}
{{- end}}
{{- if .Highlights}}

**하이라이트**
{{- range .Highlights}}
· {{mdsafe (displayline .)}}
{{- end}}
{{- end}}
{{- if .DataRows}}

**프로필**
{{- range .DataRows}}
{{- if .Multiline}}
{{mdsafe (displayline .Label)}}:
{{replace (mdsafe .Value) "\r\n" "\n"}}
{{- else}}
{{mdsafe (displayline .Label)}}: {{mdsafe (displayline .Value)}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

**링크**
{{- range $i, $link := .SocialLinks}}
{{if gt $i 0}}
{{end}}{{mdsafe (displayline .Label)}}
{{.URL}}
{{- end}}
{{- end}}
{{- if .OfficialURL}}

**공식 프로필**
{{.OfficialURL}}
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(175, 'CELEBRATION_ANNIVERSARY', NULL, '🎉 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
https://youtube.com/channel/{{.ChannelID}}{{end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(177, 'CMD_ALARM_LIST', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라
{{- else -}}
🔔 **설정된 알람** · {{.Count}}개
{{- range $i, $alarm := .Alarms}}
{{if eq $i 0}}
{{end}}· {{mdsafe (displayline $alarm.MemberName)}}{{if $alarm.TypesLabel}} ({{mdsafe (displayline $alarm.TypesLabel)}}){{end}}
{{- end -}}
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(178, 'CMD_MEMBER_NOT_FOUND', NULL, '❌ ''{{mdsafe (displayline .MemberName)}}'' 멤버를 찾을 수 없습니다.', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(44, 'CMD_MEMBER_NOT_LIVE', NULL, '{{printf "\u200b"}}{{mdsafe (displayline .MemberName)}}: 현재 방송 중이 아닙니다.', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(179, 'CMD_AMBIGUOUS_MEMBER', NULL, '같은 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}· {{mdsafe (displayline .Name)}}
{{end}}
예) {{mdescape (printf "%s%s %s" .Prefix .CommandExample (displayline .FirstName))}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(22, 'OUTBOX_SHORTS_GROUP', NULL, '🔔 {{mdsafe (displayline .MemberName)}} 새 쇼츠 · {{.Count}}개
{{- $n := 0}}
{{- range $item := .Items}}
{{- if or (trim $item.Title) $item.URL}}
{{if gt $n 0}}──────────{{end}}
{{$n = add $n 1 -}}
{{$n}} · {{if trim $item.Title}}{{mdsafe (truncate 64 (displayline $item.Title))}}{{else}}쇼츠{{end}}
{{- if $item.URL}}
{{$item.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- if ne $n .Count}}

전체 {{.Count}}개 중 {{$n}}개 표시
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 4, 'plain'),
	(176, 'CMD_MEMBER_NO_UPCOMING', NULL, '📅 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}}: {{.Hours}}시간 이내 예정된 방송이 없습니다.', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:49.286172+00', 2, 'richtext'),
	(12, 'CMD_UPCOMING_STREAMS', NULL, '{{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 예정 방송 · {{.Count}}개 · {{.Hours}}시간 이내
{{- range $i, $stream := .Streams}}{{if eq $i 0}}
다음: {{with displayline .ChannelName}}{{mdsafe .}} · {{end}}{{mdsafe .TimeInfo}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline .ChannelName}}{{mdsafe .}} · {{end}}{{mdsafe .TimeInfo}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 6, 'plain'),
	(180, 'CMD_MAJOR_EVENT_WEEKLY_SUMMARY', NULL, '📅 **이번 주 행사** · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{with displayline $event.Title}}**{{mdsafe (truncate 64 .)}}**{{else}}**행사**{{end}}
{{- if $event.DateStr}}
{{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(182, 'ALARM_DISPATCH_NOTIFICATION', NULL, '{{if .IsStarting}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .ScheduleMessage}}
{{replace (mdsafe .ScheduleMessage) "\r\n" "\n"}}
{{- end}}
{{- if .URL}}
{{with trustedlink .URL}}{{.}}{{else}}{{mdsafe (replace (replace $.URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:49.286172+00', 2, 'richtext'),
	(181, 'CMD_MAJOR_EVENT_MONTHLY_SUMMARY', NULL, '📅 **이번 달 행사** · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{with displayline $event.Title}}**{{mdsafe (truncate 64 .)}}**{{else}}**행사**{{end}}
{{- if $event.DateStr}}
{{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(184, 'CMD_ALARM_CLEARED', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 {{.Count}}개를 모두 해제했습니다.
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(185, 'CMD_MEMBER_NEWS_SUBSCRIBED', NULL, '✅ 뉴스 알림을 켰습니다.
발송: 매주 월요일 09:00 KST', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(187, 'CMD_MEMBER_NEWS_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 KST
해제: {{mdescape .Prefix}}뉴스알림 끄기
{{- else}}
설정: {{mdescape .Prefix}}뉴스알림 켜기
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(188, 'OUTBOX_SHORTS', NULL, '🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 쇼츠
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(189, 'OUTBOX_COMMUNITY', NULL, '🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 커뮤니티 글
{{- with displayline .ContentText}}
{{mdsafe (truncate 100 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(191, 'OUTBOX_VIDEO', NULL, '{{if eq .Kind "LIVE_STREAM"}}🔴 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 방송 시작{{else if .IsUpcomingPremiere}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 최초공개{{else}}🔔 {{with displayline .MemberName}}**{{mdsafe .}}**{{end}} 새 영상{{end}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:42.774308+00', 1, 'richtext'),
	(30, 'CMD_MAJOR_EVENT_USAGE', NULL, '🔔 행사 알림 명령어
{{mdescape .Prefix}}행사 켜기 / 끄기 / 상태', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(34, 'CMD_MEMBER_NEWS_NO_MEMBERS', NULL, '📰 뉴스 대상 멤버가 없습니다.
예) {{mdescape .Prefix}}알람 추가 페코라', '2000-01-01 00:00:00+00', '2026-11-09 05:16:42.774308+00', 3, 'plain'),
	(52, 'ALARM_DISPATCH_NOTIFICATION_GROUP', NULL, '{{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}} · {{len .Entries}}개
{{- $names := 0}}
{{- range .Entries}}{{with displayline .MemberName}}{{if $names}} · {{else}}
{{printf "\u200b"}}{{end}}{{mdsafe .}}{{$names = add $names 1}}{{end}}{{end}}
{{- range $i, $entry := .Entries}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{with displayline .MemberName}}{{mdsafe .}} · {{end}}{{if .IsStarting}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{replace (replace (mdsafe .ScheduleMessage) "\r\n" "\n") "\n" "\n\u200b"}}
{{- end}}
{{- if .URL}}
{{with trustedlink .URL}}{{.}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace $entry.URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 5, 'plain'),
	(183, 'ALARM_DISPATCH_NOTIFICATION_GROUP', NULL, '{{if .IsStarting}}🔴 **{{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작**{{else}}⏰ **{{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전**{{end}} · {{len .Entries}}개
{{- $names := 0}}
{{- range .Entries}}{{with displayline .MemberName}}{{if $names}} · {{else}}
{{end}}{{mdsafe .}}{{$names = add $names 1}}{{end}}{{end}}
{{- range $i, $entry := .Entries}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .MemberName}}**{{mdsafe .}}** · {{end}}{{if .IsStarting}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}{{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .ScheduleMessage}}
{{replace (mdsafe .ScheduleMessage) "\r\n" "\n"}}
{{- end}}
{{- if .URL}}
{{with trustedlink .URL}}{{.}}{{else}}{{mdsafe (replace (replace $entry.URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:49.286172+00', 3, 'richtext'),
	(15, 'CMD_CHANNEL_SCHEDULE', NULL, '{{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{mdsafe (displayline .ChannelName)}}: {{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{mdsafe (displayline .ChannelName)}} 일정 · {{.Count}}개 · {{.Days}}일 이내
{{- range $i, $stream := .Streams}}{{if eq $i 0}}
{{if .IsLive}}지금 방송 중{{else}}다음: {{mdsafe .TimeInfo}}{{end}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{if .IsLive}}🔴 방송 중{{else}}{{mdsafe .TimeInfo}}{{end}}
{{- if trim .Title}}
{{printf "\u200b"}}{{mdsafe (truncate 64 (displayline .Title))}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 6, 'plain'),
	(190, 'CMD_CHANNEL_SCHEDULE', NULL, '{{- if not (displayline .ChannelName) -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}}: {{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
📅 {{with displayline .ChannelName}}**{{mdsafe .}}**{{end}} 일정 · {{.Count}}개 · {{.Days}}일 이내
{{- range $i, $stream := .Streams}}{{if eq $i 0}}
{{if .IsLive}}지금 방송 중{{else}}다음: {{mdsafe .TimeInfo}}{{end}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{if .IsLive}}🔴 **방송 중**{{else}}**{{mdsafe .TimeInfo}}**{{end}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:49.286172+00', 4, 'richtext'),
	(31, 'CMD_MAJOR_EVENT_MONTHLY_SUMMARY', NULL, '📅 이번 달 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{if trim $event.Title}}{{mdsafe (truncate 64 (displayline $event.Title))}}{{else}}행사{{end}}
{{- if $event.DateStr}}
{{printf "\u200b"}}{{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 5, 'plain'),
	(29, 'CMD_MAJOR_EVENT_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: {{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}
{{- if .IsSubscribed}}
발송: 매주 월요일 09:00 · 매월 1일 10:00 KST
해제: {{mdescape .Prefix}}행사 끄기
{{- else}}
설정: {{mdescape .Prefix}}행사 켜기
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 4, 'plain'),
	(24, 'CMD_MAJOR_EVENT_WEEKLY_SUMMARY', NULL, '📅 이번 주 행사 · {{.Count}}개
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{- range $i, $event := .Events}}
{{if gt $i 0}}──────────{{end}}
{{add $i 1}} · {{if trim $event.Title}}{{mdsafe (truncate 64 (displayline $event.Title))}}{{else}}행사{{end}}
{{- if $event.DateStr}}
{{printf "\u200b"}}{{mdsafe (displayline $event.DateStr)}}
{{- end}}
{{- if $event.Members}}
참여: {{mdsafe (displayline $event.Members)}}
{{- end}}
{{- if $event.Link}}
{{$event.Link}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2026-11-09 05:16:49.286172+00', 5, 'plain'),
	(198, 'CMD_UPCOMING_STREAMS', NULL, '{{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
📅 **예정 방송** · {{.Count}}개 · {{.Hours}}시간 이내
{{- range $i, $stream := .Streams}}{{if eq $i 0}}
다음: {{with displayline .ChannelName}}{{mdsafe .}} · {{end}}{{mdsafe .TimeInfo}}{{end}}{{end}}
{{- if gt .Count (len .Streams)}}
전체 {{.Count}}개 중 {{len .Streams}}개 표시
{{- end}}
{{- range $i, $stream := .Streams}}
{{if gt $i 0}}──────────{{end}}
{{with displayline .ChannelName}}**{{mdsafe .}}** · {{end}}{{mdsafe .TimeInfo}}
{{- with displayline .Title}}
{{mdsafe (truncate 64 .)}}
{{- end}}
{{- if .CollabMembers}}
콜라보: {{mdsafe (displayline .CollabMembers)}}
{{- end}}
{{- if .URL}}
{{.URL}}
{{- end}}
{{- end -}}
{{- end -}}', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:49.286172+00', 4, 'richtext'),
	(13, 'CMD_HELP', NULL, '홀로라이브 봇 명령어

[방송]
{{mdescape .Prefix}}라이브 - 방송 중 목록
{{mdescape .Prefix}}라이브 [멤버명] - 멤버 라이브 확인
{{mdescape .Prefix}}예정 - 예정 방송 목록
{{mdescape .Prefix}}예정 [멤버명] - 멤버 예정 방송
{{mdescape .Prefix}}멤버 [이름] - 일주일 이내 방송 일정
{{mdescape .Prefix}}방송시간 [멤버명] [N일] - 방송 시간 합계(기본 7일)

[방송 이력]
{{mdescape .Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
{{mdescape .Prefix}}방송이력 경마 30 - 최근 30일 경마
{{mdescape .Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
{{mdescape .Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
{{mdescape .Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

[멤버]
{{mdescape .Prefix}}멤버 - 전체 멤버 목록
{{mdescape .Prefix}}정보 [멤버명] - 프로필 조회

[알람]
{{mdescape .Prefix}}알람 추가 [멤버명]
{{mdescape .Prefix}}알람 제거 [멤버명]
{{mdescape .Prefix}}알람 목록
{{mdescape .Prefix}}알람 초기화

[뉴스]
{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약
{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태

[행사]
{{mdescape .Prefix}}행사 - 행사 알림 상태
{{mdescape .Prefix}}행사 켜기 / 끄기

[기념일]
{{mdescape .Prefix}}기념일 - 이번 달 생일·주년
{{mdescape .Prefix}}기념일 다음달 / 저번달

[기타]
{{mdescape .Prefix}}도움말 - 도움말', '2000-01-01 00:00:00+00', '2026-11-09 05:16:50.609701+00', 7, 'plain'),
	(186, 'CMD_HELP', NULL, '**홀로라이브 봇 명령어**

**방송**
{{mdescape .Prefix}}라이브 - 방송 중 목록
{{mdescape .Prefix}}라이브 [멤버명] - 멤버 라이브 확인
{{mdescape .Prefix}}예정 - 예정 방송 목록
{{mdescape .Prefix}}예정 [멤버명] - 멤버 예정 방송
{{mdescape .Prefix}}멤버 [이름] - 일주일 이내 방송 일정
{{mdescape .Prefix}}방송시간 [멤버명] [N일] - 방송 시간 합계(기본 7일)

**방송 이력**
{{mdescape .Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
{{mdescape .Prefix}}방송이력 경마 30 - 최근 30일 경마
{{mdescape .Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
{{mdescape .Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
{{mdescape .Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

**멤버**
{{mdescape .Prefix}}멤버 - 전체 멤버 목록
{{mdescape .Prefix}}정보 [멤버명] - 프로필 조회

**알람**
{{mdescape .Prefix}}알람 추가 [멤버명]
{{mdescape .Prefix}}알람 제거 [멤버명]
{{mdescape .Prefix}}알람 목록
{{mdescape .Prefix}}알람 초기화

**뉴스**
{{mdescape .Prefix}}뉴스 - 주간 뉴스 요약
{{mdescape .Prefix}}뉴스알림 켜기 / 끄기 / 상태

**행사**
{{mdescape .Prefix}}행사 - 행사 알림 상태
{{mdescape .Prefix}}행사 켜기 / 끄기

**기념일**
{{mdescape .Prefix}}기념일 - 이번 달 생일·주년
{{mdescape .Prefix}}기념일 다음달 / 저번달

**기타**
{{mdescape .Prefix}}도움말 - 도움말', '2026-11-09 05:16:42.774308+00', '2026-11-09 05:16:50.609701+00', 4, 'richtext');


--
-- Data for Name: observation_contract_generations; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.observation_contract_generations VALUES
	('youtubejs', 'community_page', 1, 1, 'migration-144', '2026-11-09 05:14:14.930526+00'),
	('youtubejs', 'shorts_list', 1, 1, 'migration-144', '2026-11-09 05:14:14.930526+00'),
	('youtubejs', 'channel_profile', 1, 1, 'migration-144', '2026-11-09 05:14:14.930526+00'),
	('youtubejs', 'channel_photo', 1, 1, 'migration-144', '2026-11-09 05:14:14.930526+00'),
	('holodex', 'schedule_snapshot', 1, 1, 'migration-144', '2026-11-09 05:14:14.930526+00'),
	('holodex', 'channel_profile', 1, 1, 'migration-144', '2026-11-09 05:14:14.930526+00'),
	('holodex', 'channel_photo', 1, 1, 'migration-144', '2026-11-09 05:14:14.930526+00'),
	('hololive_official', 'schedule_snapshot', 1, 1, 'migration-144', '2026-11-09 05:14:14.930526+00'),
	('youtubejs', 'channel_live_check', 1, 1, 'migration-218', '2026-11-09 05:14:28.930076+00'),
	('youtubejs', 'video_live_check', 1, 1, 'migration-218', '2026-11-09 05:14:28.930076+00'),
	('holodex', 'live_snapshot', 1, 2, 'migration-225', '2026-11-09 05:14:30.676594+00'),
	('youtubejs', 'live_snapshot', 1, 2, 'migration-225', '2026-11-09 05:14:30.676594+00'),
	('youtubejs', 'video_list', 1, 2, 'migration-260', '2026-11-09 05:15:26.336796+00');


--
-- Data for Name: source_collection_checkpoints; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_applications; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_collisions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_consumer_offsets; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_decisions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_entity_kinds; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_payload_gc_state; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.source_observation_payload_gc_state VALUES
	(true, 0);


--
-- Data for Name: source_observation_payloads; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_provenance_gc_state; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.source_observation_provenance_gc_state VALUES
	(true, 0);


--
-- Data for Name: source_observation_provenances; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_queue; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_replay_epoch; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observation_retention_state; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.source_observation_retention_state VALUES
	(true, '-infinity', 0, NULL, NULL);


--
-- Data for Name: source_observation_subject_heads; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_observations; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: source_reconciliation_conflicts; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: x_space_session; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: x_space_starts; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_channel_live_checks; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_channel_photo_heads; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_channel_photo_variants; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_channel_profile_evidence; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_channel_profile_heads; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_channel_profiles; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_collection_job_leases; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_collection_projection_generations; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_collection_projection_guard; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.youtube_collection_projection_guard VALUES
	(true, '1640');


--
-- Data for Name: youtube_collection_runtime_bundles; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_collection_target_reasons; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_collection_targets; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_community_posts; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_community_shorts_alarm_states; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_community_shorts_source_posts; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_content_absence_slots; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_content_alarm_tracking; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_content_channel_heads; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_content_evidence_clocks; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_content_watermarks; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_absence_slots; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_pending_ends; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_reconciliation_heads; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_review_receipt_facts; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_review_receipts; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_runtime_state; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_sessions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_time_finalizations; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_notification_delivery; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_notification_delivery_ledger; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_notification_delivery_telemetry; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_notification_outbox; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_notification_send_request; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_schedule_items; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_video_availability; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_videos; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Name: acl_rooms_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.acl_rooms_id_seq', 1, false);


--
-- Name: acl_settings_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.acl_settings_id_seq', 1, false);


--
-- Name: alarm_dispatch_admin_actions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.alarm_dispatch_admin_actions_id_seq', 1, false);


--
-- Name: alarm_dispatch_deliveries_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.alarm_dispatch_deliveries_id_seq', 1, false);


--
-- Name: alarm_dispatch_event_collisions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.alarm_dispatch_event_collisions_id_seq', 1, false);


--
-- Name: alarm_dispatch_events_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.alarm_dispatch_events_id_seq', 1, false);


--
-- Name: alarm_dispatch_send_units_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.alarm_dispatch_send_units_id_seq', 1, false);


--
-- Name: alarms_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.alarms_id_seq', 1, false);


--
-- Name: bot_command_executions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.bot_command_executions_id_seq', 1, false);


--
-- Name: bot_reply_outbox_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.bot_reply_outbox_id_seq', 1, false);


--
-- Name: bot_reply_outbox_replay_audit_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.bot_reply_outbox_replay_audit_id_seq', 1, false);


--
-- Name: bot_reply_outbox_resolution_audit_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.bot_reply_outbox_resolution_audit_id_seq', 1, false);


--
-- Name: bot_webhook_inbox_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.bot_webhook_inbox_id_seq', 1, false);


--
-- Name: major_event_subscriptions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.major_event_subscriptions_id_seq', 1, false);


--
-- Name: major_events_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.major_events_id_seq', 1, false);


--
-- Name: member_news_subscriptions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.member_news_subscriptions_id_seq', 1, false);


--
-- Name: members_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.members_id_seq', 27, true);


--
-- Name: message_strings_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.message_strings_id_seq', 162, true);


--
-- Name: notification_delivery_outbox_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.notification_delivery_outbox_id_seq', 1, false);


--
-- Name: notification_template_revisions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.notification_template_revisions_id_seq', 1, false);


--
-- Name: notification_templates_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.notification_templates_id_seq', 204, true);


--
-- Name: source_observation_applications_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.source_observation_applications_id_seq', 1, false);


--
-- Name: source_observation_collisions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.source_observation_collisions_id_seq', 1, false);


--
-- Name: source_observation_decisions_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.source_observation_decisions_id_seq', 1, false);


--
-- Name: source_observation_entity_kinds_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.source_observation_entity_kinds_id_seq', 1, false);


--
-- Name: source_observation_payloads_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.source_observation_payloads_id_seq', 1, false);


--
-- Name: source_observation_provenances_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.source_observation_provenances_id_seq', 1, false);


--
-- Name: source_observations_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.source_observations_id_seq', 1, false);


--
-- Name: source_reconciliation_conflicts_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.source_reconciliation_conflicts_id_seq', 1, false);


--
-- Name: youtube_collection_projection_generations_generation_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.youtube_collection_projection_generations_generation_seq', 1, false);


--
-- Name: youtube_notification_delivery_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.youtube_notification_delivery_id_seq', 1, false);


--
-- Name: youtube_notification_delivery_telemetry_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.youtube_notification_delivery_telemetry_id_seq', 1, false);


--
-- Name: youtube_notification_outbox_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.youtube_notification_outbox_id_seq', 1, false);


--
-- Name: acl_rooms acl_rooms_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.acl_rooms
    ADD CONSTRAINT acl_rooms_pkey PRIMARY KEY (id);


--
-- Name: acl_settings acl_settings_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.acl_settings
    ADD CONSTRAINT acl_settings_key_key UNIQUE (key);


--
-- Name: acl_settings acl_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.acl_settings
    ADD CONSTRAINT acl_settings_pkey PRIMARY KEY (id);


--
-- Name: alarm_dispatch_admin_actions alarm_dispatch_admin_actions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_admin_actions
    ADD CONSTRAINT alarm_dispatch_admin_actions_pkey PRIMARY KEY (id);


--
-- Name: alarm_dispatch_closeout_receipts alarm_dispatch_closeout_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_closeout_receipts
    ADD CONSTRAINT alarm_dispatch_closeout_receipts_pkey PRIMARY KEY (receipt_id);


--
-- Name: alarm_dispatch_closeout_receipts alarm_dispatch_closeout_receipts_send_unit_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_closeout_receipts
    ADD CONSTRAINT alarm_dispatch_closeout_receipts_send_unit_id_key UNIQUE (send_unit_id);


--
-- Name: alarm_dispatch_deliveries alarm_dispatch_deliveries_dedupe_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_deliveries
    ADD CONSTRAINT alarm_dispatch_deliveries_dedupe_key_key UNIQUE (dedupe_key);


--
-- Name: alarm_dispatch_deliveries alarm_dispatch_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_deliveries
    ADD CONSTRAINT alarm_dispatch_deliveries_pkey PRIMARY KEY (id);


--
-- Name: alarm_dispatch_event_collisions alarm_dispatch_event_collisio_event_key_incoming_payload_ha_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_event_collisions
    ADD CONSTRAINT alarm_dispatch_event_collisio_event_key_incoming_payload_ha_key UNIQUE (event_key, incoming_payload_hash);


--
-- Name: alarm_dispatch_event_collisions alarm_dispatch_event_collisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_event_collisions
    ADD CONSTRAINT alarm_dispatch_event_collisions_pkey PRIMARY KEY (id);


--
-- Name: alarm_dispatch_events alarm_dispatch_events_event_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_events
    ADD CONSTRAINT alarm_dispatch_events_event_key_key UNIQUE (event_key);


--
-- Name: alarm_dispatch_events alarm_dispatch_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_events
    ADD CONSTRAINT alarm_dispatch_events_pkey PRIMARY KEY (id);


--
-- Name: alarm_dispatch_send_units alarm_dispatch_send_units_client_request_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_send_units
    ADD CONSTRAINT alarm_dispatch_send_units_client_request_id_key UNIQUE (client_request_id);


--
-- Name: alarm_dispatch_send_units alarm_dispatch_send_units_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_send_units
    ADD CONSTRAINT alarm_dispatch_send_units_pkey PRIMARY KEY (id);


--
-- Name: alarm_dispatch_send_units alarm_dispatch_send_units_unit_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_send_units
    ADD CONSTRAINT alarm_dispatch_send_units_unit_key_key UNIQUE (unit_key);


--
-- Name: alarm_room_display_names alarm_room_display_names_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_room_display_names
    ADD CONSTRAINT alarm_room_display_names_pkey PRIMARY KEY (room_id);


--
-- Name: alarm_upcoming_candidates alarm_upcoming_candidates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_upcoming_candidates
    ADD CONSTRAINT alarm_upcoming_candidates_pkey PRIMARY KEY (dedupe_key);


--
-- Name: alarms alarms_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarms
    ADD CONSTRAINT alarms_pkey PRIMARY KEY (id);


--
-- Name: bot_command_executions bot_command_executions_message_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_command_executions
    ADD CONSTRAINT bot_command_executions_message_id_key UNIQUE (message_id);


--
-- Name: bot_command_executions bot_command_executions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_command_executions
    ADD CONSTRAINT bot_command_executions_pkey PRIMARY KEY (id);


--
-- Name: bot_reply_outbox bot_reply_outbox_client_request_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox
    ADD CONSTRAINT bot_reply_outbox_client_request_id_key UNIQUE (client_request_id);


--
-- Name: bot_reply_outbox bot_reply_outbox_message_id_phase_ordinal_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox
    ADD CONSTRAINT bot_reply_outbox_message_id_phase_ordinal_key UNIQUE (message_id, phase, ordinal);


--
-- Name: bot_reply_outbox bot_reply_outbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox
    ADD CONSTRAINT bot_reply_outbox_pkey PRIMARY KEY (id);


--
-- Name: bot_reply_outbox_replay_audit bot_reply_outbox_replay_audit_outbox_id_grant_number_event__key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox_replay_audit
    ADD CONSTRAINT bot_reply_outbox_replay_audit_outbox_id_grant_number_event__key UNIQUE (outbox_id, grant_number, event_type);


--
-- Name: bot_reply_outbox_replay_audit bot_reply_outbox_replay_audit_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox_replay_audit
    ADD CONSTRAINT bot_reply_outbox_replay_audit_pkey PRIMARY KEY (id);


--
-- Name: bot_reply_outbox_resolution_audit bot_reply_outbox_resolution_audit_outbox_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox_resolution_audit
    ADD CONSTRAINT bot_reply_outbox_resolution_audit_outbox_id_key UNIQUE (outbox_id);


--
-- Name: bot_reply_outbox_resolution_audit bot_reply_outbox_resolution_audit_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox_resolution_audit
    ADD CONSTRAINT bot_reply_outbox_resolution_audit_pkey PRIMARY KEY (id);


--
-- Name: bot_webhook_heads bot_webhook_heads_message_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_webhook_heads
    ADD CONSTRAINT bot_webhook_heads_message_id_key UNIQUE (message_id);


--
-- Name: bot_webhook_heads bot_webhook_heads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_webhook_heads
    ADD CONSTRAINT bot_webhook_heads_pkey PRIMARY KEY (ordering_key);


--
-- Name: bot_webhook_inbox bot_webhook_inbox_message_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_webhook_inbox
    ADD CONSTRAINT bot_webhook_inbox_message_id_key UNIQUE (message_id);


--
-- Name: bot_webhook_inbox bot_webhook_inbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_webhook_inbox
    ADD CONSTRAINT bot_webhook_inbox_pkey PRIMARY KEY (id);


--
-- Name: kakao_rooms kakao_rooms_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.kakao_rooms
    ADD CONSTRAINT kakao_rooms_pkey PRIMARY KEY (room_id);


--
-- Name: major_event_subscriptions major_event_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.major_event_subscriptions
    ADD CONSTRAINT major_event_subscriptions_pkey PRIMARY KEY (id);


--
-- Name: major_event_subscriptions major_event_subscriptions_room_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.major_event_subscriptions
    ADD CONSTRAINT major_event_subscriptions_room_id_key UNIQUE (room_id);


--
-- Name: major_events major_events_external_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.major_events
    ADD CONSTRAINT major_events_external_id_key UNIQUE (external_id);


--
-- Name: major_events major_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.major_events
    ADD CONSTRAINT major_events_pkey PRIMARY KEY (id);


--
-- Name: member_news_subscriptions member_news_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.member_news_subscriptions
    ADD CONSTRAINT member_news_subscriptions_pkey PRIMARY KEY (id);


--
-- Name: member_news_subscriptions member_news_subscriptions_room_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.member_news_subscriptions
    ADD CONSTRAINT member_news_subscriptions_room_id_key UNIQUE (room_id);


--
-- Name: members members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.members
    ADD CONSTRAINT members_pkey PRIMARY KEY (id);


--
-- Name: message_strings message_strings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.message_strings
    ADD CONSTRAINT message_strings_pkey PRIMARY KEY (id);


--
-- Name: notification_delivery_outbox notification_delivery_outbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_delivery_outbox
    ADD CONSTRAINT notification_delivery_outbox_pkey PRIMARY KEY (id);


--
-- Name: notification_template_revisions notification_template_revisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_template_revisions
    ADD CONSTRAINT notification_template_revisions_pkey PRIMARY KEY (id);


--
-- Name: notification_templates notification_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_templates
    ADD CONSTRAINT notification_templates_pkey PRIMARY KEY (id);


--
-- Name: observation_contract_generations observation_contract_generations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.observation_contract_generations
    ADD CONSTRAINT observation_contract_generations_pkey PRIMARY KEY (provider, observation_kind);


--
-- Name: source_collection_checkpoints source_collection_checkpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_collection_checkpoints
    ADD CONSTRAINT source_collection_checkpoints_pkey PRIMARY KEY (provider, observation_kind, subject_key, scope_sha256);


--
-- Name: source_observation_applications source_observation_applications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_applications
    ADD CONSTRAINT source_observation_applications_pkey PRIMARY KEY (id);


--
-- Name: source_observation_collisions source_observation_collisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_collisions
    ADD CONSTRAINT source_observation_collisions_pkey PRIMARY KEY (id);


--
-- Name: source_observation_consumer_offsets source_observation_consumer_offsets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_consumer_offsets
    ADD CONSTRAINT source_observation_consumer_offsets_pkey PRIMARY KEY (consumer_name, observation_kind);


--
-- Name: source_observation_decisions source_observation_decisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_decisions
    ADD CONSTRAINT source_observation_decisions_pkey PRIMARY KEY (id);


--
-- Name: source_observation_entity_kinds source_observation_entity_kinds_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_entity_kinds
    ADD CONSTRAINT source_observation_entity_kinds_pkey PRIMARY KEY (id);


--
-- Name: source_observation_payload_gc_state source_observation_payload_gc_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_payload_gc_state
    ADD CONSTRAINT source_observation_payload_gc_state_pkey PRIMARY KEY (singleton);


--
-- Name: source_observation_payloads source_observation_payloads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_payloads
    ADD CONSTRAINT source_observation_payloads_pkey PRIMARY KEY (id);


--
-- Name: source_observation_provenance_gc_state source_observation_provenance_gc_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_provenance_gc_state
    ADD CONSTRAINT source_observation_provenance_gc_state_pkey PRIMARY KEY (singleton);


--
-- Name: source_observation_provenances source_observation_provenances_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_provenances
    ADD CONSTRAINT source_observation_provenances_pkey PRIMARY KEY (id);


--
-- Name: source_observation_queue source_observation_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_queue
    ADD CONSTRAINT source_observation_queue_pkey PRIMARY KEY (observation_id);


--
-- Name: source_observation_replay_epoch source_observation_replay_epoch_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_replay_epoch
    ADD CONSTRAINT source_observation_replay_epoch_pkey PRIMARY KEY (singleton);


--
-- Name: source_observation_retention_state source_observation_retention_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_retention_state
    ADD CONSTRAINT source_observation_retention_state_pkey PRIMARY KEY (singleton);


--
-- Name: source_observation_subject_heads source_observation_subject_heads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_subject_heads
    ADD CONSTRAINT source_observation_subject_heads_pkey PRIMARY KEY (provider, observation_kind, subject_key);


--
-- Name: source_observations source_observations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observations
    ADD CONSTRAINT source_observations_pkey PRIMARY KEY (id);


--
-- Name: source_reconciliation_conflicts source_reconciliation_conflicts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_reconciliation_conflicts
    ADD CONSTRAINT source_reconciliation_conflicts_pkey PRIMARY KEY (id);


--
-- Name: source_observation_decisions uq_source_observation_decision; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_decisions
    ADD CONSTRAINT uq_source_observation_decision UNIQUE (name);


--
-- Name: source_observation_entity_kinds uq_source_observation_entity_kind; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_entity_kinds
    ADD CONSTRAINT uq_source_observation_entity_kind UNIQUE (name);


--
-- Name: source_observations uq_source_observation_identity; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observations
    ADD CONSTRAINT uq_source_observation_identity UNIQUE (provider, observation_kind, subject_key, observation_key, schema_version, contract_generation);


--
-- Name: source_observation_payloads uq_source_observation_payload_digest; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_payloads
    ADD CONSTRAINT uq_source_observation_payload_digest UNIQUE (observation_kind, schema_version, canonical_profile, payload_sha256);


--
-- Name: source_observation_provenances uq_source_observation_provenance; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_provenances
    ADD CONSTRAINT uq_source_observation_provenance UNIQUE (job_key, collector_instance, collection_job_kind);


--
-- Name: source_reconciliation_conflicts uq_source_reconciliation_conflict; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_reconciliation_conflicts
    ADD CONSTRAINT uq_source_reconciliation_conflict UNIQUE (observation_id, entity_kind, entity_key, field_name);


--
-- Name: youtube_channel_live_checks uq_youtube_channel_live_checks_observation; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_live_checks
    ADD CONSTRAINT uq_youtube_channel_live_checks_observation UNIQUE (observation_id);


--
-- Name: youtube_live_pending_ends uq_youtube_live_pending_end_observation; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_pending_ends
    ADD CONSTRAINT uq_youtube_live_pending_end_observation UNIQUE (video_id, observation_id);


--
-- Name: youtube_live_review_receipt_facts uq_youtube_live_review_receipt_facts_video; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_review_receipt_facts
    ADD CONSTRAINT uq_youtube_live_review_receipt_facts_video UNIQUE (video_id, receipt_id);


--
-- Name: youtube_live_time_finalizations uq_youtube_live_time_finalizations_observation; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_time_finalizations
    ADD CONSTRAINT uq_youtube_live_time_finalizations_observation UNIQUE (observation_id);


--
-- Name: youtube_video_availability uq_youtube_video_availability_observation; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_video_availability
    ADD CONSTRAINT uq_youtube_video_availability_observation UNIQUE (observation_id);


--
-- Name: message_strings ux_message_strings; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.message_strings
    ADD CONSTRAINT ux_message_strings UNIQUE (namespace, key);


--
-- Name: x_space_session x_space_session_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.x_space_session
    ADD CONSTRAINT x_space_session_pkey PRIMARY KEY (id);


--
-- Name: x_space_starts x_space_starts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.x_space_starts
    ADD CONSTRAINT x_space_starts_pkey PRIMARY KEY (space_id);


--
-- Name: youtube_channel_live_checks youtube_channel_live_checks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_live_checks
    ADD CONSTRAINT youtube_channel_live_checks_pkey PRIMARY KEY (channel_id);


--
-- Name: youtube_channel_photo_heads youtube_channel_photo_heads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_photo_heads
    ADD CONSTRAINT youtube_channel_photo_heads_pkey PRIMARY KEY (channel_id, kind);


--
-- Name: youtube_channel_photo_variants youtube_channel_photo_variants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_photo_variants
    ADD CONSTRAINT youtube_channel_photo_variants_pkey PRIMARY KEY (channel_id, kind, provider, scheduled_for);


--
-- Name: youtube_channel_profile_evidence youtube_channel_profile_evidence_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_profile_evidence
    ADD CONSTRAINT youtube_channel_profile_evidence_pkey PRIMARY KEY (channel_id, scheduled_for, provider);


--
-- Name: youtube_channel_profile_heads youtube_channel_profile_heads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_profile_heads
    ADD CONSTRAINT youtube_channel_profile_heads_pkey PRIMARY KEY (channel_id);


--
-- Name: youtube_channel_profiles youtube_channel_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_profiles
    ADD CONSTRAINT youtube_channel_profiles_pkey PRIMARY KEY (channel_id);


--
-- Name: youtube_collection_job_leases youtube_collection_job_leases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_collection_job_leases
    ADD CONSTRAINT youtube_collection_job_leases_pkey PRIMARY KEY (job_key);


--
-- Name: youtube_collection_projection_generations youtube_collection_projection_generations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_collection_projection_generations
    ADD CONSTRAINT youtube_collection_projection_generations_pkey PRIMARY KEY (generation);


--
-- Name: youtube_collection_projection_guard youtube_collection_projection_guard_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_collection_projection_guard
    ADD CONSTRAINT youtube_collection_projection_guard_pkey PRIMARY KEY (guard_key);


--
-- Name: youtube_collection_runtime_bundles youtube_collection_runtime_bundles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_collection_runtime_bundles
    ADD CONSTRAINT youtube_collection_runtime_bundles_pkey PRIMARY KEY (projection_generation, kind, subject_key);


--
-- Name: youtube_collection_target_reasons youtube_collection_target_reasons_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_collection_target_reasons
    ADD CONSTRAINT youtube_collection_target_reasons_pkey PRIMARY KEY (projection_generation, subject_key, observation_kind, reason_kind, reason_key);


--
-- Name: youtube_collection_targets youtube_collection_targets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_collection_targets
    ADD CONSTRAINT youtube_collection_targets_pkey PRIMARY KEY (projection_generation, subject_key, observation_kind);


--
-- Name: youtube_community_posts youtube_community_posts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_community_posts
    ADD CONSTRAINT youtube_community_posts_pkey PRIMARY KEY (post_id);


--
-- Name: youtube_community_shorts_alarm_states youtube_community_shorts_alarm_states_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_community_shorts_alarm_states
    ADD CONSTRAINT youtube_community_shorts_alarm_states_pkey PRIMARY KEY (kind, post_id);


--
-- Name: youtube_community_shorts_source_posts youtube_community_shorts_source_posts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_community_shorts_source_posts
    ADD CONSTRAINT youtube_community_shorts_source_posts_pkey PRIMARY KEY (kind, post_id);


--
-- Name: youtube_content_absence_slots youtube_content_absence_slots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_absence_slots
    ADD CONSTRAINT youtube_content_absence_slots_pkey PRIMARY KEY (channel_id, observation_kind, scheduled_for);


--
-- Name: youtube_content_alarm_tracking youtube_content_alarm_tracking_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_alarm_tracking
    ADD CONSTRAINT youtube_content_alarm_tracking_pkey PRIMARY KEY (kind, canonical_content_id);


--
-- Name: youtube_content_channel_heads youtube_content_channel_heads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_channel_heads
    ADD CONSTRAINT youtube_content_channel_heads_pkey PRIMARY KEY (channel_id, observation_kind);


--
-- Name: youtube_content_evidence_clocks youtube_content_evidence_clocks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_evidence_clocks
    ADD CONSTRAINT youtube_content_evidence_clocks_pkey PRIMARY KEY (video_id);


--
-- Name: youtube_content_watermarks youtube_content_watermarks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_watermarks
    ADD CONSTRAINT youtube_content_watermarks_pkey PRIMARY KEY (channel_id, watermark_type);


--
-- Name: youtube_live_absence_slots youtube_live_absence_slots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_absence_slots
    ADD CONSTRAINT youtube_live_absence_slots_pkey PRIMARY KEY (observation_id);


--
-- Name: youtube_live_pending_ends youtube_live_pending_ends_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_pending_ends
    ADD CONSTRAINT youtube_live_pending_ends_pkey PRIMARY KEY (video_id);


--
-- Name: youtube_live_reconciliation_heads youtube_live_reconciliation_heads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_reconciliation_heads
    ADD CONSTRAINT youtube_live_reconciliation_heads_pkey PRIMARY KEY (video_id);


--
-- Name: youtube_live_review_receipt_facts youtube_live_review_receipt_facts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_review_receipt_facts
    ADD CONSTRAINT youtube_live_review_receipt_facts_pkey PRIMARY KEY (receipt_id);


--
-- Name: youtube_live_review_receipts youtube_live_review_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_review_receipts
    ADD CONSTRAINT youtube_live_review_receipts_pkey PRIMARY KEY (receipt_id);


--
-- Name: youtube_live_review_receipts youtube_live_review_receipts_video_id_snapshot_sha256_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_review_receipts
    ADD CONSTRAINT youtube_live_review_receipts_video_id_snapshot_sha256_key UNIQUE (video_id, snapshot_sha256);


--
-- Name: youtube_live_runtime_state youtube_live_runtime_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_runtime_state
    ADD CONSTRAINT youtube_live_runtime_state_pkey PRIMARY KEY (video_id);


--
-- Name: youtube_live_sessions youtube_live_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_sessions
    ADD CONSTRAINT youtube_live_sessions_pkey PRIMARY KEY (video_id);


--
-- Name: youtube_live_time_finalizations youtube_live_time_finalizations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_time_finalizations
    ADD CONSTRAINT youtube_live_time_finalizations_pkey PRIMARY KEY (video_id);


--
-- Name: youtube_notification_delivery_ledger youtube_notification_delivery_ledger_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_delivery_ledger
    ADD CONSTRAINT youtube_notification_delivery_ledger_pkey PRIMARY KEY (kind, logical_id, room_id);


--
-- Name: youtube_notification_delivery youtube_notification_delivery_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_delivery
    ADD CONSTRAINT youtube_notification_delivery_pkey PRIMARY KEY (id);


--
-- Name: youtube_notification_delivery_telemetry youtube_notification_delivery_telemetry_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_delivery_telemetry
    ADD CONSTRAINT youtube_notification_delivery_telemetry_pkey PRIMARY KEY (id);


--
-- Name: youtube_notification_outbox youtube_notification_outbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_outbox
    ADD CONSTRAINT youtube_notification_outbox_pkey PRIMARY KEY (id);


--
-- Name: youtube_notification_send_request youtube_notification_send_request_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_send_request
    ADD CONSTRAINT youtube_notification_send_request_pkey PRIMARY KEY (base_id);


--
-- Name: youtube_schedule_items youtube_schedule_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_schedule_items
    ADD CONSTRAINT youtube_schedule_items_pkey PRIMARY KEY (group_key, provider, external_id);


--
-- Name: youtube_video_availability youtube_video_availability_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_video_availability
    ADD CONSTRAINT youtube_video_availability_pkey PRIMARY KEY (video_id);


--
-- Name: youtube_videos youtube_videos_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_videos
    ADD CONSTRAINT youtube_videos_pkey PRIMARY KEY (video_id);


--
-- Name: idx_alarm_dispatch_admin_actions_delivery; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_admin_actions_delivery ON public.alarm_dispatch_admin_actions USING btree (delivery_id);


--
-- Name: idx_alarm_dispatch_deliveries_cancelled_retention; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_cancelled_retention ON public.alarm_dispatch_deliveries USING btree (cancelled_at, id) WHERE (status = 'cancelled'::text);


--
-- Name: idx_alarm_dispatch_deliveries_dlq_retention; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_dlq_retention ON public.alarm_dispatch_deliveries USING btree (dlq_at, id) WHERE (status = 'dlq'::text);


--
-- Name: idx_alarm_dispatch_deliveries_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_due ON public.alarm_dispatch_deliveries USING btree (next_attempt_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'retry'::text]));


--
-- Name: idx_alarm_dispatch_deliveries_event_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_event_id ON public.alarm_dispatch_deliveries USING btree (event_id);


--
-- Name: idx_alarm_dispatch_deliveries_leased_expired; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_leased_expired ON public.alarm_dispatch_deliveries USING btree (lock_expires_at, id) WHERE (status = 'leased'::text);


--
-- Name: idx_alarm_dispatch_deliveries_quarantined_retention; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_quarantined_retention ON public.alarm_dispatch_deliveries USING btree (quarantined_at, id) WHERE (status = 'quarantined'::text);


--
-- Name: idx_alarm_dispatch_deliveries_send_unit; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_send_unit ON public.alarm_dispatch_deliveries USING btree (send_unit_id) WHERE (send_unit_id IS NOT NULL);


--
-- Name: idx_alarm_dispatch_deliveries_send_unit_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_send_unit_due ON public.alarm_dispatch_deliveries USING btree (send_unit_id, next_attempt_at, id) WHERE ((send_unit_id IS NOT NULL) AND (status = ANY (ARRAY['pending'::text, 'retry'::text])));


--
-- Name: idx_alarm_dispatch_deliveries_sending_stale; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_sending_stale ON public.alarm_dispatch_deliveries USING btree (sending_started_at, id) WHERE (status = 'sending'::text);


--
-- Name: idx_alarm_dispatch_deliveries_sent_event_room; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_sent_event_room ON public.alarm_dispatch_deliveries USING btree (event_id, room_id, sent_at DESC) WHERE ((status = 'sent'::text) AND (sent_at IS NOT NULL));


--
-- Name: idx_alarm_dispatch_deliveries_sent_retention; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_sent_retention ON public.alarm_dispatch_deliveries USING btree (sent_at, id) WHERE (status = 'sent'::text);


--
-- Name: idx_alarm_dispatch_deliveries_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_status_created ON public.alarm_dispatch_deliveries USING btree (status, created_at DESC);


--
-- Name: idx_alarm_dispatch_event_collisions_existing_event; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_event_collisions_existing_event ON public.alarm_dispatch_event_collisions USING btree (existing_event_id) WHERE (existing_event_id IS NOT NULL);


--
-- Name: idx_alarm_dispatch_events_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_events_created ON public.alarm_dispatch_events USING btree (created_at, id);


--
-- Name: idx_alarm_dispatch_events_live_stream_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_events_live_stream_created ON public.alarm_dispatch_events USING btree (stream_id, created_at DESC) WHERE (alarm_type = 'LIVE'::public.alarm_type);


--
-- Name: idx_alarm_dispatch_send_units_handoff_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_send_units_handoff_due ON public.alarm_dispatch_send_units USING btree (handoff_next_check_at, id) WHERE (handoff_next_check_at IS NOT NULL);


--
-- Name: idx_alarm_upcoming_candidates_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_upcoming_candidates_pending ON public.alarm_upcoming_candidates USING btree (checked_at, dedupe_key) WHERE (outcome = 'pending'::text);


--
-- Name: idx_alarm_upcoming_candidates_terminal; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_upcoming_candidates_terminal ON public.alarm_upcoming_candidates USING btree (terminal_at) WHERE (terminal_at IS NOT NULL);


--
-- Name: idx_alarms_alarm_types_gin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarms_alarm_types_gin ON public.alarms USING gin (alarm_types);


--
-- Name: idx_alarms_channel_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarms_channel_created ON public.alarms USING btree (channel_id, created_at);


--
-- Name: idx_alarms_room_channel_host; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_alarms_room_channel_host ON public.alarms USING btree (room_id, channel_id, host_id);


--
-- Name: idx_alarms_room_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarms_room_created ON public.alarms USING btree (room_id, created_at);


--
-- Name: idx_bot_command_executions_status_claimed; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_command_executions_status_claimed ON public.bot_command_executions USING btree (claimed_at, id) WHERE (status = 'claimed'::text);


--
-- Name: idx_bot_command_executions_terminal_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_command_executions_terminal_updated ON public.bot_command_executions USING btree (updated_at, id) WHERE (status = ANY (ARRAY['succeeded'::text, 'failed'::text, 'outcome_unknown'::text]));


--
-- Name: idx_bot_reply_outbox_discarded_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_discarded_updated ON public.bot_reply_outbox USING btree (updated_at, id) WHERE (status = 'discarded'::text);


--
-- Name: idx_bot_reply_outbox_due_available; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_due_available ON public.bot_reply_outbox USING btree (available_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text]));


--
-- Name: idx_bot_reply_outbox_handoff_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_handoff_due ON public.bot_reply_outbox USING btree (handoff_next_check_at, id) WHERE ((status = 'accepted'::text) AND (claim_token IS NULL));


--
-- Name: idx_bot_reply_outbox_manual_review_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_manual_review_updated ON public.bot_reply_outbox USING btree (updated_at, id) WHERE (status = 'manual_review'::text);


--
-- Name: idx_bot_reply_outbox_message; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_message ON public.bot_reply_outbox USING btree (message_id, ordinal);


--
-- Name: idx_bot_reply_outbox_replay_audit_outbox_recorded; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_replay_audit_outbox_recorded ON public.bot_reply_outbox_replay_audit USING btree (outbox_id, recorded_at, id);


--
-- Name: idx_bot_reply_outbox_room_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_room_active ON public.bot_reply_outbox USING btree (room_id, id) WHERE (status = ANY (ARRAY['pending'::text, 'submitting'::text, 'accepted'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text]));


--
-- Name: idx_bot_reply_outbox_submitting_lease_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_submitting_lease_expiry ON public.bot_reply_outbox USING btree (lease_until, id) WHERE (status = 'submitting'::text);


--
-- Name: idx_bot_reply_outbox_terminal_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_terminal_updated ON public.bot_reply_outbox USING btree (updated_at, id) WHERE (status = ANY (ARRAY['handoff_completed'::text, 'dead'::text, 'permanent_conflict'::text]));


--
-- Name: idx_bot_webhook_inbox_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_webhook_inbox_due ON public.bot_webhook_inbox USING btree (available_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'retry'::text]));


--
-- Name: idx_bot_webhook_inbox_lease_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_webhook_inbox_lease_expiry ON public.bot_webhook_inbox USING btree (lease_until, id) WHERE (status = 'processing'::text);


--
-- Name: idx_bot_webhook_inbox_ordering_partition; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_webhook_inbox_ordering_partition ON public.bot_webhook_inbox USING btree (ordering_key, id) WHERE (status = ANY (ARRAY['pending'::text, 'processing'::text, 'retry'::text]));


--
-- Name: idx_bot_webhook_inbox_terminal_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_webhook_inbox_terminal_updated ON public.bot_webhook_inbox USING btree (updated_at, id) WHERE (status = ANY (ARRAY['dead'::text, 'succeeded'::text]));


--
-- Name: idx_major_events_start_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_major_events_start_date ON public.major_events USING btree (event_start_date);


--
-- Name: idx_major_events_status_type_start; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_major_events_status_type_start ON public.major_events USING btree (status, type, event_start_date);


--
-- Name: idx_member_news_subscriptions_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_member_news_subscriptions_created_at ON public.member_news_subscriptions USING btree (created_at);


--
-- Name: idx_members_active_channel; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_members_active_channel ON public.members USING btree (channel_id) WHERE ((is_graduated = false) AND (channel_id IS NOT NULL));


--
-- Name: idx_members_aliases_ja_gin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_members_aliases_ja_gin ON public.members USING gin (((aliases -> 'ja'::text)));


--
-- Name: idx_members_aliases_ko_gin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_members_aliases_ko_gin ON public.members USING gin (((aliases -> 'ko'::text)));


--
-- Name: idx_members_birthday_month_day; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_members_birthday_month_day ON public.members USING btree (EXTRACT(month FROM birthday), EXTRACT(day FROM birthday)) WHERE (birthday IS NOT NULL);


--
-- Name: idx_members_channel_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_members_channel_id ON public.members USING btree (channel_id) WHERE (channel_id IS NOT NULL);


--
-- Name: idx_members_debut_date_month_day; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_members_debut_date_month_day ON public.members USING btree (EXTRACT(month FROM debut_date), EXTRACT(day FROM debut_date)) WHERE (debut_date IS NOT NULL);


--
-- Name: idx_members_english_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_members_english_name ON public.members USING btree (english_name);


--
-- Name: idx_members_org_english_name; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_members_org_english_name ON public.members USING btree (org, english_name);


--
-- Name: idx_members_photo_updated_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_members_photo_updated_at ON public.members USING btree (photo_updated_at);


--
-- Name: idx_members_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_members_slug ON public.members USING btree (slug);


--
-- Name: idx_ndo_handoff_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ndo_handoff_due ON public.notification_delivery_outbox USING btree (handoff_next_check_at, id) WHERE (handoff_next_check_at IS NOT NULL);


--
-- Name: idx_ndo_kind_content; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ndo_kind_content ON public.notification_delivery_outbox USING btree (kind, content_id);


--
-- Name: idx_ndo_pending_due_created_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ndo_pending_due_created_id ON public.notification_delivery_outbox USING btree (next_attempt_at, created_at, id) WHERE (status = 'PENDING'::text);


--
-- Name: idx_ndo_sending_stale; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ndo_sending_stale ON public.notification_delivery_outbox USING btree (sending_started_at, id) WHERE (status = 'SENDING'::text);


--
-- Name: idx_ndo_terminal_cleanup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ndo_terminal_cleanup ON public.notification_delivery_outbox USING btree (COALESCE(sent_at, created_at)) WHERE (status = ANY (ARRAY['SENT'::text, 'FAILED'::text, 'QUARANTINED'::text]));


--
-- Name: idx_room_list; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_room_list ON public.acl_rooms USING btree (room_id, list_type);


--
-- Name: idx_source_application_entity_latest; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_application_entity_latest ON public.source_observation_applications USING btree (entity_kind_id, entity_key, observation_id);


--
-- Name: idx_source_observation_collisions_existing_observation; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_observation_collisions_existing_observation ON public.source_observation_collisions USING btree (existing_observation_id);


--
-- Name: idx_source_observation_collisions_occurred; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_observation_collisions_occurred ON public.source_observation_collisions USING btree (occurred_at, id);


--
-- Name: idx_source_observation_queue_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_observation_queue_claim ON public.source_observation_queue USING btree (available_at, observation_id) WHERE (status = 'PENDING'::text);


--
-- Name: idx_source_observation_queue_dead_letter_retention; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_observation_queue_dead_letter_retention ON public.source_observation_queue USING btree (dead_lettered_at, observation_id) WHERE (status = 'DEAD_LETTER'::text);


--
-- Name: idx_source_observation_queue_lease_recovery; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_observation_queue_lease_recovery ON public.source_observation_queue USING btree (lease_expires_at, observation_id) WHERE (status = 'PROCESSING'::text);


--
-- Name: idx_source_observation_queue_processed_retention; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_observation_queue_processed_retention ON public.source_observation_queue USING btree (processed_at, observation_id) WHERE (status = 'PROCESSED'::text);


--
-- Name: idx_source_observations_kind_received_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_observations_kind_received_id ON public.source_observations USING btree (observation_kind, received_at, id);


--
-- Name: idx_source_observations_payload_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_observations_payload_id ON public.source_observations USING btree (payload_id);


--
-- Name: idx_source_observations_provenance_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_source_observations_provenance_id ON public.source_observations USING btree (provenance_id);


--
-- Name: idx_template_revisions_template_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_template_revisions_template_created ON public.notification_template_revisions USING btree (template_id, created_at DESC);


--
-- Name: idx_x_space_starts_first_seen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_x_space_starts_first_seen ON public.x_space_starts USING btree (first_seen_at);


--
-- Name: idx_ycat_kind_content; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycat_kind_content ON public.youtube_content_alarm_tracking USING btree (kind, content_id);


--
-- Name: idx_ycp_channel_first_seen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycp_channel_first_seen ON public.youtube_community_posts USING btree (channel_id, first_seen_at DESC);


--
-- Name: idx_ycsas_kind_content; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ycsas_kind_content ON public.youtube_community_shorts_alarm_states USING btree (kind, content_id);


--
-- Name: idx_ydt_delivery_attempt; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ydt_delivery_attempt ON public.youtube_notification_delivery_telemetry USING btree (delivery_id, attempt_ordinal);


--
-- Name: idx_ydt_logged_event_retention; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ydt_logged_event_retention ON public.youtube_notification_delivery_telemetry USING btree (event_at, id) WHERE (logged_at IS NOT NULL);


--
-- Name: idx_ydt_outbox; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ydt_outbox ON public.youtube_notification_delivery_telemetry USING btree (outbox_id);


--
-- Name: idx_ydt_pending_next; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ydt_pending_next ON public.youtube_notification_delivery_telemetry USING btree (next_attempt_at, event_at) WHERE (logged_at IS NULL);


--
-- Name: idx_yls_channel; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_channel ON public.youtube_live_sessions USING btree (channel_id);


--
-- Name: idx_yls_ended_channel_history_sort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_ended_channel_history_sort ON public.youtube_live_sessions USING btree (channel_id, history_sort_at DESC, video_id DESC) WHERE (status = 'ENDED'::text);


--
-- Name: idx_yls_ended_history_sort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_ended_history_sort ON public.youtube_live_sessions USING btree (history_sort_at DESC, video_id DESC) WHERE (status = 'ENDED'::text);


--
-- Name: idx_yls_live_first_seen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_live_first_seen ON public.youtube_live_sessions USING btree (live_first_seen_at, channel_id) WHERE (status = 'LIVE'::text);


--
-- Name: idx_yls_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_status ON public.youtube_live_sessions USING btree (status);


--
-- Name: idx_ynd_outbox_room; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ynd_outbox_room ON public.youtube_notification_delivery USING btree (outbox_id, room_id);


--
-- Name: idx_ynd_pending_due_created_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ynd_pending_due_created_id ON public.youtube_notification_delivery USING btree (next_attempt_at, created_at, id) WHERE (status = 'PENDING'::text);


--
-- Name: idx_ynd_sending_stale; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ynd_sending_stale ON public.youtube_notification_delivery USING btree (locked_at, id) WHERE (status = 'SENDING'::text);


--
-- Name: idx_yno_kind_content; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_yno_kind_content ON public.youtube_notification_outbox USING btree (kind, content_id);


--
-- Name: idx_yno_pending_due_created_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yno_pending_due_created_id ON public.youtube_notification_outbox USING btree (next_attempt_at, created_at, id) WHERE (status = 'PENDING'::text);


--
-- Name: idx_yno_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yno_status_created ON public.youtube_notification_outbox USING btree (status, created_at);


--
-- Name: idx_youtube_channel_photo_variants_observation_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_channel_photo_variants_observation_id ON public.youtube_channel_photo_variants USING btree (observation_id);


--
-- Name: idx_youtube_channel_photo_variants_received_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_channel_photo_variants_received_at ON public.youtube_channel_photo_variants USING btree (received_at);


--
-- Name: idx_youtube_channel_profile_evidence_observation_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_channel_profile_evidence_observation_id ON public.youtube_channel_profile_evidence USING btree (observation_id);


--
-- Name: idx_youtube_channel_profile_evidence_received_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_channel_profile_evidence_received_at ON public.youtube_channel_profile_evidence USING btree (received_at);


--
-- Name: idx_youtube_collection_job_projection_generation; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_collection_job_projection_generation ON public.youtube_collection_job_leases USING btree (projection_generation, job_key);


--
-- Name: idx_youtube_collection_projection_retired_retention; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_collection_projection_retired_retention ON public.youtube_collection_projection_generations USING btree (valid_until, generation) WHERE (status = 'RETIRED'::text);


--
-- Name: idx_youtube_collection_targets_generation_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_collection_targets_generation_kind ON public.youtube_collection_targets USING btree (projection_generation, observation_kind, subject_key);


--
-- Name: idx_youtube_content_absence_slots_effective; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_content_absence_slots_effective ON public.youtube_content_absence_slots USING btree (channel_id, observation_kind, effective_at);


--
-- Name: idx_youtube_content_absence_slots_observation_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_content_absence_slots_observation_id ON public.youtube_content_absence_slots USING btree (observation_id);


--
-- Name: idx_youtube_content_absence_slots_scheduled_for; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_content_absence_slots_scheduled_for ON public.youtube_content_absence_slots USING btree (scheduled_for);


--
-- Name: idx_youtube_content_evidence_clocks_last_absence_observation_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_content_evidence_clocks_last_absence_observation_id ON public.youtube_content_evidence_clocks USING btree (last_absence_observation_id);


--
-- Name: idx_youtube_delivery_send_request; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_delivery_send_request ON public.youtube_notification_delivery USING btree (send_request_id) WHERE (send_request_id IS NOT NULL);


--
-- Name: idx_youtube_live_absence_slots_channels; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_live_absence_slots_channels ON public.youtube_live_absence_slots USING gin (((coverage -> 'requested_channel_ids'::text)));


--
-- Name: idx_youtube_live_absence_slots_scheduled_for; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_live_absence_slots_scheduled_for ON public.youtube_live_absence_slots USING btree (scheduled_for);


--
-- Name: idx_youtube_live_pending_end_observation; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_live_pending_end_observation ON public.youtube_live_pending_ends USING btree (observation_id);


--
-- Name: idx_youtube_live_reconciliation_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_live_reconciliation_due ON public.youtube_live_reconciliation_heads USING btree (next_end_check_at, video_id) WHERE (next_end_check_at IS NOT NULL);


--
-- Name: idx_youtube_live_reconciliation_end_candidate; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_live_reconciliation_end_candidate ON public.youtube_live_reconciliation_heads USING btree (end_candidate_observation_id) WHERE (end_candidate_observation_id IS NOT NULL);


--
-- Name: idx_youtube_live_reconciliation_heads_active_video; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_live_reconciliation_heads_active_video ON public.youtube_live_reconciliation_heads USING btree (video_id) WHERE (status = ANY (ARRAY['LIVE'::text, 'UPCOMING'::text]));


--
-- Name: idx_youtube_send_request_handoff_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_send_request_handoff_due ON public.youtube_notification_send_request USING btree (handoff_next_check_at, base_id) WHERE (handoff_next_check_at IS NOT NULL);


--
-- Name: idx_yv_channel_is_short; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yv_channel_is_short ON public.youtube_videos USING btree (channel_id, is_short);


--
-- Name: uq_source_observation_application_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_source_observation_application_entity ON public.source_observation_applications USING btree (observation_id, entity_kind_id, entity_key);


--
-- Name: uq_youtube_collection_projection_one_current; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_youtube_collection_projection_one_current ON public.youtube_collection_projection_generations USING btree (status) WHERE (status = 'CURRENT'::text);


--
-- Name: ux_notification_templates_channel_format; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ux_notification_templates_channel_format ON public.notification_templates USING btree (template_key, channel_id, format_variant) WHERE (channel_id IS NOT NULL);


--
-- Name: ux_notification_templates_default_format; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ux_notification_templates_default_format ON public.notification_templates USING btree (template_key, format_variant) WHERE (channel_id IS NULL);


--
-- Name: alarm_dispatch_closeout_receipts alarm_dispatch_closeout_receipts_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER alarm_dispatch_closeout_receipts_immutable BEFORE DELETE OR UPDATE ON public.alarm_dispatch_closeout_receipts FOR EACH ROW EXECUTE FUNCTION public.reject_alarm_dispatch_closeout_receipt_change();


--
-- Name: alarm_dispatch_closeout_receipts alarm_dispatch_closeout_receipts_no_truncate; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER alarm_dispatch_closeout_receipts_no_truncate BEFORE TRUNCATE ON public.alarm_dispatch_closeout_receipts FOR EACH STATEMENT EXECUTE FUNCTION public.reject_alarm_dispatch_closeout_receipt_change();


--
-- Name: bot_command_executions bot_command_execution_terminal_summary_scrub; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_command_execution_terminal_summary_scrub BEFORE INSERT OR UPDATE OF status, result_summary ON public.bot_command_executions FOR EACH ROW WHEN ((new.status = ANY (ARRAY['succeeded'::text, 'failed'::text, 'outcome_unknown'::text]))) EXECUTE FUNCTION public.scrub_bot_command_execution_terminal_summary();


--
-- Name: bot_reply_outbox bot_reply_outbox_discard_audit_required; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_reply_outbox_discard_audit_required BEFORE INSERT OR UPDATE ON public.bot_reply_outbox FOR EACH ROW EXECUTE FUNCTION public.enforce_bot_reply_outbox_discard_audit();


--
-- Name: bot_reply_outbox_replay_audit bot_reply_outbox_replay_audit_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_reply_outbox_replay_audit_immutable BEFORE DELETE OR UPDATE ON public.bot_reply_outbox_replay_audit FOR EACH ROW EXECUTE FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation();


--
-- Name: bot_reply_outbox bot_reply_outbox_replay_claim_audit; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_reply_outbox_replay_claim_audit BEFORE UPDATE ON public.bot_reply_outbox FOR EACH ROW EXECUTE FUNCTION public.append_bot_reply_outbox_replay_claim_audit();


--
-- Name: bot_reply_outbox_resolution_audit bot_reply_outbox_resolution_audit_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_reply_outbox_resolution_audit_immutable BEFORE DELETE OR UPDATE ON public.bot_reply_outbox_resolution_audit FOR EACH ROW EXECUTE FUNCTION public.reject_bot_reply_outbox_resolution_audit_mutation();


--
-- Name: notification_templates notification_template_row_version_trigger; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER notification_template_row_version_trigger BEFORE UPDATE ON public.notification_templates FOR EACH ROW EXECUTE FUNCTION public.notification_template_row_version();


--
-- Name: youtube_collection_projection_generations youtube_collection_runtime_delete; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_delete AFTER DELETE ON public.youtube_collection_projection_generations REFERENCING OLD TABLE AS runtime_old FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement();


--
-- Name: youtube_collection_targets youtube_collection_runtime_delete; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_delete AFTER DELETE ON public.youtube_collection_targets REFERENCING OLD TABLE AS runtime_old FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement();


--
-- Name: youtube_collection_projection_generations youtube_collection_runtime_guard; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_guard BEFORE INSERT OR DELETE OR UPDATE OR TRUNCATE ON public.youtube_collection_projection_generations FOR EACH STATEMENT EXECUTE FUNCTION public.guard_youtube_collection_runtime_bundles();


--
-- Name: youtube_collection_targets youtube_collection_runtime_guard; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_guard BEFORE INSERT OR DELETE OR UPDATE OR TRUNCATE ON public.youtube_collection_targets FOR EACH STATEMENT EXECUTE FUNCTION public.guard_youtube_collection_runtime_bundles();


--
-- Name: youtube_collection_projection_generations youtube_collection_runtime_insert; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_insert AFTER INSERT ON public.youtube_collection_projection_generations REFERENCING NEW TABLE AS runtime_new FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement();


--
-- Name: youtube_collection_targets youtube_collection_runtime_insert; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_insert AFTER INSERT ON public.youtube_collection_targets REFERENCING NEW TABLE AS runtime_new FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement();


--
-- Name: youtube_collection_projection_generations youtube_collection_runtime_truncate; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_truncate AFTER TRUNCATE ON public.youtube_collection_projection_generations FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement();


--
-- Name: youtube_collection_targets youtube_collection_runtime_truncate; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_truncate AFTER TRUNCATE ON public.youtube_collection_targets FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement();


--
-- Name: youtube_collection_projection_generations youtube_collection_runtime_update; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_update AFTER UPDATE ON public.youtube_collection_projection_generations REFERENCING OLD TABLE AS runtime_old NEW TABLE AS runtime_new FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement();


--
-- Name: youtube_collection_targets youtube_collection_runtime_update; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_collection_runtime_update AFTER UPDATE ON public.youtube_collection_targets REFERENCING OLD TABLE AS runtime_old NEW TABLE AS runtime_new FOR EACH STATEMENT EXECUTE FUNCTION public.refresh_youtube_collection_runtime_bundles_statement();


--
-- Name: youtube_live_review_receipt_facts youtube_live_review_receipt_facts_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_live_review_receipt_facts_immutable BEFORE DELETE OR UPDATE ON public.youtube_live_review_receipt_facts FOR EACH ROW EXECUTE FUNCTION public.reject_youtube_live_review_receipt_change();


--
-- Name: youtube_live_review_receipts youtube_live_review_receipts_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_live_review_receipts_immutable BEFORE DELETE OR UPDATE ON public.youtube_live_review_receipts FOR EACH ROW EXECUTE FUNCTION public.reject_youtube_live_review_receipt_change();


--
-- Name: youtube_live_review_receipts youtube_live_review_receipts_store_facts; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER youtube_live_review_receipts_store_facts AFTER INSERT ON public.youtube_live_review_receipts FOR EACH ROW EXECUTE FUNCTION public.store_youtube_live_review_receipt_facts();


--
-- Name: alarm_dispatch_admin_actions alarm_dispatch_admin_actions_delivery_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_admin_actions
    ADD CONSTRAINT alarm_dispatch_admin_actions_delivery_id_fkey FOREIGN KEY (delivery_id) REFERENCES public.alarm_dispatch_deliveries(id) ON DELETE SET NULL;


--
-- Name: alarm_dispatch_deliveries alarm_dispatch_deliveries_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_deliveries
    ADD CONSTRAINT alarm_dispatch_deliveries_event_id_fkey FOREIGN KEY (event_id) REFERENCES public.alarm_dispatch_events(id) ON DELETE RESTRICT;


--
-- Name: alarm_dispatch_deliveries alarm_dispatch_deliveries_send_unit_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_deliveries
    ADD CONSTRAINT alarm_dispatch_deliveries_send_unit_fk FOREIGN KEY (send_unit_id) REFERENCES public.alarm_dispatch_send_units(id) ON DELETE RESTRICT;


--
-- Name: alarm_dispatch_event_collisions alarm_dispatch_event_collisions_existing_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_event_collisions
    ADD CONSTRAINT alarm_dispatch_event_collisions_existing_event_id_fkey FOREIGN KEY (existing_event_id) REFERENCES public.alarm_dispatch_events(id) ON DELETE SET NULL;


--
-- Name: bot_reply_outbox_replay_audit bot_reply_outbox_replay_audit_outbox_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox_replay_audit
    ADD CONSTRAINT bot_reply_outbox_replay_audit_outbox_id_fkey FOREIGN KEY (outbox_id) REFERENCES public.bot_reply_outbox(id) ON DELETE CASCADE;


--
-- Name: bot_reply_outbox_resolution_audit bot_reply_outbox_resolution_audit_outbox_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox_resolution_audit
    ADD CONSTRAINT bot_reply_outbox_resolution_audit_outbox_id_fkey FOREIGN KEY (outbox_id) REFERENCES public.bot_reply_outbox(id) ON DELETE CASCADE;


--
-- Name: bot_webhook_heads bot_webhook_heads_message_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_webhook_heads
    ADD CONSTRAINT bot_webhook_heads_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.bot_webhook_inbox(message_id) ON DELETE CASCADE;


--
-- Name: source_observation_applications fk_source_application_decision; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_applications
    ADD CONSTRAINT fk_source_application_decision FOREIGN KEY (decision_id) REFERENCES public.source_observation_decisions(id) ON DELETE RESTRICT;


--
-- Name: source_observation_applications fk_source_application_entity_kind; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_applications
    ADD CONSTRAINT fk_source_application_entity_kind FOREIGN KEY (entity_kind_id) REFERENCES public.source_observation_entity_kinds(id) ON DELETE RESTRICT;


--
-- Name: source_collection_checkpoints fk_source_checkpoint_contract; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_collection_checkpoints
    ADD CONSTRAINT fk_source_checkpoint_contract FOREIGN KEY (provider, observation_kind) REFERENCES public.observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT;


--
-- Name: source_observation_collisions fk_source_observation_collision_contract; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_collisions
    ADD CONSTRAINT fk_source_observation_collision_contract FOREIGN KEY (provider, observation_kind) REFERENCES public.observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT;


--
-- Name: source_observations fk_source_observation_contract; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observations
    ADD CONSTRAINT fk_source_observation_contract FOREIGN KEY (provider, observation_kind) REFERENCES public.observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT;


--
-- Name: source_observations fk_source_observation_payload; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observations
    ADD CONSTRAINT fk_source_observation_payload FOREIGN KEY (payload_id) REFERENCES public.source_observation_payloads(id) ON DELETE RESTRICT;


--
-- Name: source_observations fk_source_observation_provenance; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observations
    ADD CONSTRAINT fk_source_observation_provenance FOREIGN KEY (provenance_id) REFERENCES public.source_observation_provenances(id) ON DELETE RESTRICT;


--
-- Name: source_observation_subject_heads fk_source_observation_subject_head_contract; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_subject_heads
    ADD CONSTRAINT fk_source_observation_subject_head_contract FOREIGN KEY (provider, observation_kind) REFERENCES public.observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT;


--
-- Name: source_reconciliation_conflicts fk_source_reconciliation_conflict_contract; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_reconciliation_conflicts
    ADD CONSTRAINT fk_source_reconciliation_conflict_contract FOREIGN KEY (provider, observation_kind) REFERENCES public.observation_contract_generations(provider, observation_kind) ON DELETE RESTRICT;


--
-- Name: youtube_channel_live_checks fk_youtube_channel_live_checks_observation; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_live_checks
    ADD CONSTRAINT fk_youtube_channel_live_checks_observation FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE SET NULL;


--
-- Name: youtube_live_reconciliation_heads fk_youtube_live_head_pending_end; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_reconciliation_heads
    ADD CONSTRAINT fk_youtube_live_head_pending_end FOREIGN KEY (video_id, end_candidate_observation_id) REFERENCES public.youtube_live_pending_ends(video_id, observation_id) DEFERRABLE INITIALLY DEFERRED;


--
-- Name: youtube_video_availability fk_youtube_video_availability_observation; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_video_availability
    ADD CONSTRAINT fk_youtube_video_availability_observation FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE SET NULL;


--
-- Name: notification_template_revisions notification_template_revisions_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_template_revisions
    ADD CONSTRAINT notification_template_revisions_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.notification_templates(id) ON DELETE CASCADE;


--
-- Name: source_observation_applications source_observation_applications_observation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_applications
    ADD CONSTRAINT source_observation_applications_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE CASCADE;


--
-- Name: source_observation_collisions source_observation_collisions_existing_observation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_collisions
    ADD CONSTRAINT source_observation_collisions_existing_observation_id_fkey FOREIGN KEY (existing_observation_id) REFERENCES public.source_observations(id) ON DELETE SET NULL;


--
-- Name: source_observation_queue source_observation_queue_observation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_observation_queue
    ADD CONSTRAINT source_observation_queue_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE CASCADE;


--
-- Name: source_reconciliation_conflicts source_reconciliation_conflicts_observation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_reconciliation_conflicts
    ADD CONSTRAINT source_reconciliation_conflicts_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE SET NULL;


--
-- Name: youtube_channel_photo_variants youtube_channel_photo_variants_observation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_photo_variants
    ADD CONSTRAINT youtube_channel_photo_variants_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE SET NULL;


--
-- Name: youtube_channel_profile_evidence youtube_channel_profile_evidence_observation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_profile_evidence
    ADD CONSTRAINT youtube_channel_profile_evidence_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE SET NULL;


--
-- Name: youtube_collection_job_leases youtube_collection_job_leases_projection_generation_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_collection_job_leases
    ADD CONSTRAINT youtube_collection_job_leases_projection_generation_fkey FOREIGN KEY (projection_generation) REFERENCES public.youtube_collection_projection_generations(generation) ON DELETE RESTRICT;


--
-- Name: youtube_collection_target_reasons youtube_collection_target_rea_projection_generation_subjec_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_collection_target_reasons
    ADD CONSTRAINT youtube_collection_target_rea_projection_generation_subjec_fkey FOREIGN KEY (projection_generation, subject_key, observation_kind) REFERENCES public.youtube_collection_targets(projection_generation, subject_key, observation_kind) ON DELETE CASCADE;


--
-- Name: youtube_collection_targets youtube_collection_targets_projection_generation_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_collection_targets
    ADD CONSTRAINT youtube_collection_targets_projection_generation_fkey FOREIGN KEY (projection_generation) REFERENCES public.youtube_collection_projection_generations(generation) ON DELETE CASCADE;


--
-- Name: youtube_content_absence_slots youtube_content_absence_slots_observation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_absence_slots
    ADD CONSTRAINT youtube_content_absence_slots_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE SET NULL;


--
-- Name: youtube_content_evidence_clocks youtube_content_evidence_clock_last_absence_observation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_evidence_clocks
    ADD CONSTRAINT youtube_content_evidence_clock_last_absence_observation_id_fkey FOREIGN KEY (last_absence_observation_id) REFERENCES public.source_observations(id) ON DELETE SET NULL;


--
-- Name: youtube_content_evidence_clocks youtube_content_evidence_clocks_video_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_evidence_clocks
    ADD CONSTRAINT youtube_content_evidence_clocks_video_id_fkey FOREIGN KEY (video_id) REFERENCES public.youtube_videos(video_id) ON DELETE CASCADE;


--
-- Name: youtube_live_review_receipt_facts youtube_live_review_receipt_facts_receipt_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_review_receipt_facts
    ADD CONSTRAINT youtube_live_review_receipt_facts_receipt_id_fkey FOREIGN KEY (receipt_id) REFERENCES public.youtube_live_review_receipts(receipt_id);


--
-- Name: youtube_live_time_finalizations youtube_live_time_finalizations_observation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_time_finalizations
    ADD CONSTRAINT youtube_live_time_finalizations_observation_id_fkey FOREIGN KEY (observation_id) REFERENCES public.source_observations(id) ON DELETE SET NULL;


--
-- Name: youtube_live_time_finalizations youtube_live_time_finalizations_video_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_time_finalizations
    ADD CONSTRAINT youtube_live_time_finalizations_video_id_fkey FOREIGN KEY (video_id) REFERENCES public.youtube_live_sessions(video_id) ON DELETE CASCADE;


--
-- Name: youtube_notification_delivery youtube_notification_delivery_outbox_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_delivery
    ADD CONSTRAINT youtube_notification_delivery_outbox_id_fkey FOREIGN KEY (outbox_id) REFERENCES public.youtube_notification_outbox(id) ON DELETE CASCADE;


--
-- Name: youtube_notification_delivery youtube_notification_delivery_send_request_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_delivery
    ADD CONSTRAINT youtube_notification_delivery_send_request_id_fkey FOREIGN KEY (send_request_id) REFERENCES public.youtube_notification_send_request(base_id);


--
-- Name: TYPE youtube_live_review_head_value; Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON TYPE public.youtube_live_review_head_value FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON TYPE public.youtube_live_review_head_value TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TYPE youtube_live_review_pending_value; Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON TYPE public.youtube_live_review_pending_value FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON TYPE public.youtube_live_review_pending_value TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TYPE youtube_live_review_session_value; Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON TYPE public.youtube_live_review_session_value FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON TYPE public.youtube_live_review_session_value TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TYPE youtube_live_review_value; Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON TYPE public.youtube_live_review_value FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON TYPE public.youtube_live_review_value TO hololive_runtime;
    END IF;
END $grant$;


DO $acl$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.append_bot_reply_outbox_replay_claim_audit() FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.append_bot_reply_outbox_replay_claim_audit() FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.discard_bot_reply_outbox_manual_review(bigint,text,text,text) FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.discard_bot_reply_outbox_manual_review(bigint,text,text,text) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.enforce_bot_reply_outbox_discard_audit() FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.enforce_bot_reply_outbox_discard_audit() FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.grant_bot_reply_outbox_manual_replay(bigint,text,text) FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.grant_bot_reply_outbox_manual_replay(bigint,text,text) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.guard_youtube_collection_runtime_bundles() FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.guard_youtube_collection_runtime_bundles() FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.lock_youtube_collection_runtime_bundles() FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.lock_youtube_live_runtime_state() FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.lock_youtube_live_runtime_state_shared() FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.record_youtube_live_review(uuid,text,text,text,text) FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.record_youtube_live_review(uuid,text,text,text,text) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.refresh_youtube_collection_runtime_bundle_keys(bigint[],text[],text[]) FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.refresh_youtube_collection_runtime_bundle_keys(bigint[],text[],text[]) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.refresh_youtube_collection_runtime_bundles(bigint[]) FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.refresh_youtube_collection_runtime_bundles(bigint[]) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.refresh_youtube_collection_runtime_bundles_statement() FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.refresh_youtube_collection_runtime_bundles_statement() FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.refresh_youtube_live_runtime_state(text[]) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation() FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation() FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE EXECUTE ON FUNCTION public.reject_bot_reply_outbox_resolution_audit_mutation() FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.reject_bot_reply_outbox_resolution_audit_mutation() FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.youtube_live_review_current_facts(text[]) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.youtube_live_review_current_receipt(text) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.youtube_live_review_current_receipts(text[]) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.youtube_live_review_receipt_value(jsonb) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.youtube_live_review_semantic_facts(jsonb) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.youtube_live_review_snapshot(text) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE EXECUTE ON FUNCTION public.youtube_live_review_source_value(text,text,text,text,text,text,boolean,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,text,text,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,timestamp with time zone,smallint,bigint,text,bigint,timestamp with time zone,text,text,text,text,bigint,timestamp with time zone,timestamp with time zone,timestamp with time zone,boolean,boolean,text) FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.bot_reply_outbox_replay_audit_id_seq FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.bot_reply_outbox_replay_audit_id_seq FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.bot_reply_outbox_resolution_audit_id_seq FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.bot_reply_outbox_resolution_audit_id_seq FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.major_events_id_seq FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE UPDATE ON SEQUENCE public.source_observation_applications_id_seq FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.source_observation_applications_id_seq FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.source_observation_collisions_id_seq FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE UPDATE ON SEQUENCE public.source_observation_collisions_id_seq FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.source_observation_payloads_id_seq FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE UPDATE ON SEQUENCE public.source_observation_payloads_id_seq FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.source_observations_id_seq FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE UPDATE ON SEQUENCE public.source_observations_id_seq FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE UPDATE ON SEQUENCE public.source_reconciliation_conflicts_id_seq FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.source_reconciliation_conflicts_id_seq FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE UPDATE ON SEQUENCE public.youtube_collection_projection_generations_generation_seq FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE SELECT, UPDATE, USAGE ON SEQUENCE public.youtube_collection_projection_generations_generation_seq FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.alarms FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.bot_reply_outbox_replay_audit FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.bot_reply_outbox_replay_audit FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.bot_reply_outbox_resolution_audit FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.bot_reply_outbox_resolution_audit FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.major_event_subscriptions FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.major_events FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.observation_contract_generations FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.observation_contract_generations FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_collection_checkpoints FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_collection_checkpoints FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_applications FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_applications FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_collisions FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_collisions FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_consumer_offsets FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_consumer_offsets FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_payload_gc_state FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_payload_gc_state FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_payloads FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_payloads FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_queue FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_queue FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE UPDATE, DELETE, TRUNCATE ON TABLE public.source_observation_replay_epoch FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_replay_epoch FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_retention_state FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_retention_state FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_subject_heads FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observation_subject_heads FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observations FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_observations FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE UPDATE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_reconciliation_conflicts FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.source_reconciliation_conflicts FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_live_checks FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_live_checks FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_photo_heads FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_photo_heads FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_photo_variants FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_photo_variants FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_profile_evidence FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_profile_evidence FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_profile_heads FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_channel_profile_heads FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_job_leases FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_job_leases FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_projection_generations FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_projection_generations FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        IF has_table_privilege('hololive_runtime', 'public.youtube_collection_projection_guard', 'INSERT') THEN
            REVOKE INSERT ON TABLE public.youtube_collection_projection_guard FROM hololive_runtime;
            GRANT INSERT (guard_key) ON TABLE public.youtube_collection_projection_guard TO hololive_runtime;
        END IF;
        IF has_table_privilege('hololive_runtime', 'public.youtube_collection_projection_guard', 'UPDATE') THEN
            REVOKE UPDATE ON TABLE public.youtube_collection_projection_guard FROM hololive_runtime;
            GRANT UPDATE (guard_key) ON TABLE public.youtube_collection_projection_guard TO hololive_runtime;
        END IF;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        IF has_table_privilege('hololive_scraper', 'public.youtube_collection_projection_guard', 'INSERT') THEN
            REVOKE INSERT ON TABLE public.youtube_collection_projection_guard FROM hololive_scraper;
            GRANT INSERT (guard_key) ON TABLE public.youtube_collection_projection_guard TO hololive_scraper;
        END IF;
        IF has_table_privilege('hololive_scraper', 'public.youtube_collection_projection_guard', 'UPDATE') THEN
            REVOKE UPDATE ON TABLE public.youtube_collection_projection_guard FROM hololive_scraper;
            GRANT UPDATE (guard_key) ON TABLE public.youtube_collection_projection_guard TO hololive_scraper;
        END IF;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_runtime_bundles FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_runtime_bundles FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_runtime_kinds FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_runtime_kinds FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_target_reasons FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_target_reasons FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_targets FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_collection_targets FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_content_absence_slots FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_content_absence_slots FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_content_channel_heads FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_content_channel_heads FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_content_evidence_clocks FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_content_evidence_clocks FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_absence_slots FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_absence_slots FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_pending_ends FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_pending_ends FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_reconciliation_heads FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_reconciliation_heads FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_review_receipt_facts FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_review_receipt_facts FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_review_receipts FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_review_receipts FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_runtime_state FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_runtime_state FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_sessions FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_time_finalizations FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_live_time_finalizations FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_notification_outbox FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_schedule_items FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_schedule_items FROM hololive_scraper;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        REVOKE DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_video_availability FROM hololive_runtime;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE INSERT, SELECT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLE public.youtube_video_availability FROM hololive_scraper;
    END IF;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_proc object
        CROSS JOIN LATERAL aclexplode(object.proacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.append_bot_reply_outbox_replay_claim_audit()'::regprocedure AND privilege.grantee <> object.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON FUNCTION public.append_bot_reply_outbox_replay_claim_audit() FROM %I', role_name);
    END LOOP;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_proc object
        CROSS JOIN LATERAL aclexplode(object.proacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.discard_bot_reply_outbox_manual_review(bigint,text,text,text)'::regprocedure AND privilege.grantee <> object.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON FUNCTION public.discard_bot_reply_outbox_manual_review(bigint,text,text,text) FROM %I', role_name);
    END LOOP;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_proc object
        CROSS JOIN LATERAL aclexplode(object.proacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.enforce_bot_reply_outbox_discard_audit()'::regprocedure AND privilege.grantee <> object.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON FUNCTION public.enforce_bot_reply_outbox_discard_audit() FROM %I', role_name);
    END LOOP;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_proc object
        CROSS JOIN LATERAL aclexplode(object.proacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.grant_bot_reply_outbox_manual_replay(bigint,text,text)'::regprocedure AND privilege.grantee <> object.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON FUNCTION public.grant_bot_reply_outbox_manual_replay(bigint,text,text) FROM %I', role_name);
    END LOOP;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_proc object
        CROSS JOIN LATERAL aclexplode(object.proacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.reject_bot_reply_outbox_replay_audit_mutation()'::regprocedure AND privilege.grantee <> object.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation() FROM %I', role_name);
    END LOOP;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_proc object
        CROSS JOIN LATERAL aclexplode(object.proacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.reject_bot_reply_outbox_resolution_audit_mutation()'::regprocedure AND privilege.grantee <> object.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON FUNCTION public.reject_bot_reply_outbox_resolution_audit_mutation() FROM %I', role_name);
    END LOOP;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_class object
        CROSS JOIN LATERAL aclexplode(object.relacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.bot_reply_outbox_replay_audit_id_seq'::regclass AND privilege.grantee <> object.relowner
    LOOP
        EXECUTE format('REVOKE ALL ON SEQUENCE public.bot_reply_outbox_replay_audit_id_seq FROM %I', role_name);
    END LOOP;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_class object
        CROSS JOIN LATERAL aclexplode(object.relacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.bot_reply_outbox_resolution_audit_id_seq'::regclass AND privilege.grantee <> object.relowner
    LOOP
        EXECUTE format('REVOKE ALL ON SEQUENCE public.bot_reply_outbox_resolution_audit_id_seq FROM %I', role_name);
    END LOOP;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_class object
        CROSS JOIN LATERAL aclexplode(object.relacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.bot_reply_outbox_replay_audit'::regclass AND privilege.grantee <> object.relowner
    LOOP
        EXECUTE format('REVOKE ALL ON TABLE public.bot_reply_outbox_replay_audit FROM %I', role_name);
    END LOOP;
END
$acl$;
DO $acl$
DECLARE role_name text;
BEGIN
    FOR role_name IN
        SELECT DISTINCT role.rolname
        FROM pg_class object
        CROSS JOIN LATERAL aclexplode(object.relacl) privilege
        JOIN pg_roles role ON role.oid = privilege.grantee
        WHERE object.oid = 'public.bot_reply_outbox_resolution_audit'::regclass AND privilege.grantee <> object.relowner
    LOOP
        EXECUTE format('REVOKE ALL ON TABLE public.bot_reply_outbox_resolution_audit FROM %I', role_name);
    END LOOP;
END
$acl$;
--
-- Name: FUNCTION alarm_dispatch_closeout_snapshot(p_delivery_id bigint); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.alarm_dispatch_closeout_snapshot(p_delivery_id bigint) FROM PUBLIC;


--
-- Name: FUNCTION append_bot_reply_outbox_replay_claim_audit(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.append_bot_reply_outbox_replay_claim_audit() FROM PUBLIC;


--
-- Name: FUNCTION assert_source_observation_payload_count(actual bigint, expected bigint); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.assert_source_observation_payload_count(actual bigint, expected bigint) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.assert_source_observation_payload_count(actual bigint, expected bigint) TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION assert_source_observation_payload_match(requested_id bigint, stored_payload jsonb, expected_payload jsonb); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.assert_source_observation_payload_match(requested_id bigint, stored_payload jsonb, expected_payload jsonb) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.assert_source_observation_payload_match(requested_id bigint, stored_payload jsonb, expected_payload jsonb) TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_retired_youtube_collection_job_leases(requested_cutoff timestamp with time zone, requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_retired_youtube_collection_job_leases(requested_cutoff timestamp with time zone, requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_retired_youtube_collection_job_leases(requested_cutoff timestamp with time zone, requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_retired_youtube_projection_batch(requested_cutoff timestamp with time zone, requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_retired_youtube_projection_batch(requested_cutoff timestamp with time zone, requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_retired_youtube_projection_batch(requested_cutoff timestamp with time zone, requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_source_collection_checkpoint_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_source_collection_checkpoint_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_source_collection_checkpoint_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_source_observation_payload_batch(requested_cutoff timestamp with time zone, requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_source_observation_payload_batch(requested_cutoff timestamp with time zone, requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_source_observation_payload_batch(requested_cutoff timestamp with time zone, requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_source_observation_provenance_batch(requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_source_observation_provenance_batch(requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_source_observation_provenance_batch(requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_source_observation_retention_batch(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_source_observation_retention_batch(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_source_observation_retention_batch(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_source_observation_retention_window(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer, OUT deleted_ids bigint[], OUT passed integer, OUT traversal_completed boolean); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_source_observation_retention_window(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer, OUT deleted_ids bigint[], OUT passed integer, OUT traversal_completed boolean) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_source_observation_retention_window(requested_kinds text[], requested_cutoffs timestamp with time zone[], requested_limit integer, OUT deleted_ids bigint[], OUT passed integer, OUT traversal_completed boolean) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_youtube_channel_photo_variant_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_youtube_channel_photo_variant_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_youtube_channel_photo_variant_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_youtube_channel_profile_evidence_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_youtube_channel_profile_evidence_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_youtube_channel_profile_evidence_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_youtube_content_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_youtube_content_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_youtube_content_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION delete_youtube_live_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.delete_youtube_live_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.delete_youtube_live_absence_slot_retention_batch(requested_cutoff timestamp with time zone, requested_limit integer) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION discard_bot_reply_outbox_manual_review(requested_outbox_id bigint, operator_actor text, operator_reason text, observed_iris_state text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.discard_bot_reply_outbox_manual_review(requested_outbox_id bigint, operator_actor text, operator_reason text, observed_iris_state text) FROM PUBLIC;


--
-- Name: FUNCTION enforce_bot_reply_outbox_discard_audit(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.enforce_bot_reply_outbox_discard_audit() FROM PUBLIC;


--
-- Name: FUNCTION grant_bot_reply_outbox_manual_replay(requested_outbox_id bigint, operator_actor text, operator_reason text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.grant_bot_reply_outbox_manual_replay(requested_outbox_id bigint, operator_actor text, operator_reason text) FROM PUBLIC;


--
-- Name: FUNCTION guard_youtube_collection_runtime_bundles(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.guard_youtube_collection_runtime_bundles() FROM PUBLIC;


--
-- Name: FUNCTION lock_current_youtube_collection_projection(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.lock_current_youtube_collection_projection() FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.lock_current_youtube_collection_projection() TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION lock_observation_contract(requested_provider text, requested_observation_kind text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.lock_observation_contract(requested_provider text, requested_observation_kind text) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.lock_observation_contract(requested_provider text, requested_observation_kind text) TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION lock_source_observation(requested_observation_id bigint); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.lock_source_observation(requested_observation_id bigint) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.lock_source_observation(requested_observation_id bigint) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION lock_source_observation_identity(requested_provider text, requested_observation_kind text, requested_subject_key text, requested_observation_key text, requested_schema_version smallint, requested_contract_generation bigint); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.lock_source_observation_identity(requested_provider text, requested_observation_kind text, requested_subject_key text, requested_observation_key text, requested_schema_version smallint, requested_contract_generation bigint) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.lock_source_observation_identity(requested_provider text, requested_observation_kind text, requested_subject_key text, requested_observation_key text, requested_schema_version smallint, requested_contract_generation bigint) TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION lock_source_observation_payload(requested_kind text, requested_version smallint, requested_digest bytea, expected_payload jsonb); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.lock_source_observation_payload(requested_kind text, requested_version smallint, requested_digest bytea, expected_payload jsonb) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.lock_source_observation_payload(requested_kind text, requested_version smallint, requested_digest bytea, expected_payload jsonb) TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION lock_youtube_collection_runtime_bundles(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.lock_youtube_collection_runtime_bundles() FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.lock_youtube_collection_runtime_bundles() TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION lock_youtube_live_runtime_state(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.lock_youtube_live_runtime_state() FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.lock_youtube_live_runtime_state() TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION lock_youtube_live_runtime_state_shared(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.lock_youtube_live_runtime_state_shared() FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.lock_youtube_live_runtime_state_shared() TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION record_alarm_dispatch_closeout(p_receipt_id uuid, p_addressed_delivery_id bigint, p_expected_target_ids bigint[], p_expected_sha256 text, p_operator_id text, p_reason text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.record_alarm_dispatch_closeout(p_receipt_id uuid, p_addressed_delivery_id bigint, p_expected_target_ids bigint[], p_expected_sha256 text, p_operator_id text, p_reason text) FROM PUBLIC;


--
-- Name: FUNCTION record_youtube_live_review(p_receipt_id uuid, p_video_id text, p_expected_sha256 text, p_operator_id text, p_reason text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.record_youtube_live_review(p_receipt_id uuid, p_video_id text, p_expected_sha256 text, p_operator_id text, p_reason text) FROM PUBLIC;


--
-- Name: FUNCTION refresh_youtube_collection_runtime_bundle_keys(p_generations bigint[], p_kinds text[], p_subjects text[]); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.refresh_youtube_collection_runtime_bundle_keys(p_generations bigint[], p_kinds text[], p_subjects text[]) FROM PUBLIC;


--
-- Name: FUNCTION refresh_youtube_collection_runtime_bundles(p_generations bigint[]); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.refresh_youtube_collection_runtime_bundles(p_generations bigint[]) FROM PUBLIC;


--
-- Name: FUNCTION refresh_youtube_collection_runtime_bundles_statement(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.refresh_youtube_collection_runtime_bundles_statement() FROM PUBLIC;


--
-- Name: FUNCTION refresh_youtube_live_runtime_state(p_video_ids text[]); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.refresh_youtube_live_runtime_state(p_video_ids text[]) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.refresh_youtube_live_runtime_state(p_video_ids text[]) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION reject_bot_reply_outbox_replay_audit_mutation(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation() FROM PUBLIC;


--
-- Name: FUNCTION reject_bot_reply_outbox_resolution_audit_mutation(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.reject_bot_reply_outbox_resolution_audit_mutation() FROM PUBLIC;


--
-- Name: FUNCTION reject_youtube_live_review_receipt_change(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.reject_youtube_live_review_receipt_change() FROM PUBLIC;


--
-- Name: FUNCTION require_source_observation_payload(requested_id bigint, requested_kind text, requested_version smallint, actual_kind text, actual_version smallint, actual_profile text, actual_digest bytea, actual_payload jsonb); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.require_source_observation_payload(requested_id bigint, requested_kind text, requested_version smallint, actual_kind text, actual_version smallint, actual_profile text, actual_digest bytea, actual_payload jsonb) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.require_source_observation_payload(requested_id bigint, requested_kind text, requested_version smallint, actual_kind text, actual_version smallint, actual_profile text, actual_digest bytea, actual_payload jsonb) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION source_observation_decision_id(requested_name text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.source_observation_decision_id(requested_name text) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.source_observation_decision_id(requested_name text) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION source_observation_entity_kind_id(requested_name text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.source_observation_entity_kind_id(requested_name text) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.source_observation_entity_kind_id(requested_name text) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION source_observation_provenance_id(requested_job_key text, requested_collector_instance text, requested_collection_job_kind text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.source_observation_provenance_id(requested_job_key text, requested_collector_instance text, requested_collection_job_kind text) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.source_observation_provenance_id(requested_job_key text, requested_collector_instance text, requested_collection_job_kind text) TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION source_observation_retention_protected(requested_id bigint); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.source_observation_retention_protected(requested_id bigint) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.source_observation_retention_protected(requested_id bigint) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION store_youtube_live_review_receipt_facts(); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.store_youtube_live_review_receipt_facts() FROM PUBLIC;


--
-- Name: FUNCTION youtube_collection_job_candidates(p_generation bigint, p_kinds text[], p_provider text, p_job_kind text, p_excluded text[], p_limit integer); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_collection_job_candidates(p_generation bigint, p_kinds text[], p_provider text, p_job_kind text, p_excluded text[], p_limit integer) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.youtube_collection_job_candidates(p_generation bigint, p_kinds text[], p_provider text, p_job_kind text, p_excluded text[], p_limit integer) TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_collection_job_global_candidates(p_generation bigint, p_kinds text[], p_exact_subject boolean, p_subject_key text, p_job_key text, p_excluded text[]); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_collection_job_global_candidates(p_generation bigint, p_kinds text[], p_exact_subject boolean, p_subject_key text, p_job_key text, p_excluded text[]) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.youtube_collection_job_global_candidates(p_generation bigint, p_kinds text[], p_exact_subject boolean, p_subject_key text, p_job_key text, p_excluded text[]) TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_collection_membership_valid(requested_kinds text[], requested_exact_subject boolean, requested_subject_key text, requested_target_count integer, requested_generation bigint); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_collection_membership_valid(requested_kinds text[], requested_exact_subject boolean, requested_subject_key text, requested_target_count integer, requested_generation bigint) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT ALL ON FUNCTION public.youtube_collection_membership_valid(requested_kinds text[], requested_exact_subject boolean, requested_subject_key text, requested_target_count integer, requested_generation bigint) TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_live_review_current_facts(p_video_ids text[]); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_live_review_current_facts(p_video_ids text[]) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.youtube_live_review_current_facts(p_video_ids text[]) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_live_review_current_receipt(p_video_id text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipt(p_video_id text) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.youtube_live_review_current_receipt(p_video_id text) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_live_review_current_receipts(p_video_ids text[]); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_live_review_current_receipts(p_video_ids text[]) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.youtube_live_review_current_receipts(p_video_ids text[]) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_live_review_receipt_semantic_facts(p_snapshot jsonb); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_live_review_receipt_semantic_facts(p_snapshot jsonb) FROM PUBLIC;


--
-- Name: FUNCTION youtube_live_review_receipt_value(p_facts jsonb); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_live_review_receipt_value(p_facts jsonb) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.youtube_live_review_receipt_value(p_facts jsonb) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_live_review_semantic_facts(p_snapshot jsonb); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_live_review_semantic_facts(p_snapshot jsonb) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.youtube_live_review_semantic_facts(p_snapshot jsonb) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_live_review_snapshot(p_video_id text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_live_review_snapshot(p_video_id text) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.youtube_live_review_snapshot(p_video_id text) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_live_review_source_value(p_session_video_id text, p_session_channel_id text, p_session_status text, p_session_lifecycle_origin text, p_session_title text, p_session_topic_id text, p_session_is_premiere boolean, p_session_scheduled_start_time timestamp with time zone, p_session_started_at timestamp with time zone, p_session_ended_at timestamp with time zone, p_session_live_first_seen_at timestamp with time zone, p_head_video_id text, p_head_status text, p_head_last_upcoming_positive_at timestamp with time zone, p_head_last_upcoming_positive_seen_at timestamp with time zone, p_head_last_live_positive_at timestamp with time zone, p_head_last_live_positive_seen_at timestamp with time zone, p_head_last_end_evidence_at timestamp with time zone, p_head_last_complete_absence_at timestamp with time zone, p_head_last_absence_scheduled_for timestamp with time zone, p_head_first_absence_scheduled_for timestamp with time zone, p_head_second_absence_scheduled_for timestamp with time zone, p_head_consecutive_absence_slots smallint, p_head_last_absence_observation_id bigint, p_head_end_candidate_kind text, p_head_end_candidate_observation_id bigint, p_head_ended_at timestamp with time zone, p_head_end_reason text, p_pending_video_id text, p_pending_channel_id text, p_pending_kind text, p_pending_observation_id bigint, p_pending_effective_at timestamp with time zone, p_pending_scheduled_for timestamp with time zone, p_pending_ended_at timestamp with time zone, p_pending_negative_eligible boolean, p_pending_scope_covers boolean, p_availability_availability text); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_live_review_source_value(p_session_video_id text, p_session_channel_id text, p_session_status text, p_session_lifecycle_origin text, p_session_title text, p_session_topic_id text, p_session_is_premiere boolean, p_session_scheduled_start_time timestamp with time zone, p_session_started_at timestamp with time zone, p_session_ended_at timestamp with time zone, p_session_live_first_seen_at timestamp with time zone, p_head_video_id text, p_head_status text, p_head_last_upcoming_positive_at timestamp with time zone, p_head_last_upcoming_positive_seen_at timestamp with time zone, p_head_last_live_positive_at timestamp with time zone, p_head_last_live_positive_seen_at timestamp with time zone, p_head_last_end_evidence_at timestamp with time zone, p_head_last_complete_absence_at timestamp with time zone, p_head_last_absence_scheduled_for timestamp with time zone, p_head_first_absence_scheduled_for timestamp with time zone, p_head_second_absence_scheduled_for timestamp with time zone, p_head_consecutive_absence_slots smallint, p_head_last_absence_observation_id bigint, p_head_end_candidate_kind text, p_head_end_candidate_observation_id bigint, p_head_ended_at timestamp with time zone, p_head_end_reason text, p_pending_video_id text, p_pending_channel_id text, p_pending_kind text, p_pending_observation_id bigint, p_pending_effective_at timestamp with time zone, p_pending_scheduled_for timestamp with time zone, p_pending_ended_at timestamp with time zone, p_pending_negative_eligible boolean, p_pending_scope_covers boolean, p_availability_availability text) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.youtube_live_review_source_value(p_session_video_id text, p_session_channel_id text, p_session_status text, p_session_lifecycle_origin text, p_session_title text, p_session_topic_id text, p_session_is_premiere boolean, p_session_scheduled_start_time timestamp with time zone, p_session_started_at timestamp with time zone, p_session_ended_at timestamp with time zone, p_session_live_first_seen_at timestamp with time zone, p_head_video_id text, p_head_status text, p_head_last_upcoming_positive_at timestamp with time zone, p_head_last_upcoming_positive_seen_at timestamp with time zone, p_head_last_live_positive_at timestamp with time zone, p_head_last_live_positive_seen_at timestamp with time zone, p_head_last_end_evidence_at timestamp with time zone, p_head_last_complete_absence_at timestamp with time zone, p_head_last_absence_scheduled_for timestamp with time zone, p_head_first_absence_scheduled_for timestamp with time zone, p_head_second_absence_scheduled_for timestamp with time zone, p_head_consecutive_absence_slots smallint, p_head_last_absence_observation_id bigint, p_head_end_candidate_kind text, p_head_end_candidate_observation_id bigint, p_head_ended_at timestamp with time zone, p_head_end_reason text, p_pending_video_id text, p_pending_channel_id text, p_pending_kind text, p_pending_observation_id bigint, p_pending_effective_at timestamp with time zone, p_pending_scheduled_for timestamp with time zone, p_pending_ended_at timestamp with time zone, p_pending_negative_eligible boolean, p_pending_scope_covers boolean, p_availability_availability text) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: FUNCTION youtube_schedule_collabo_talent_names_valid(names text[]); Type: ACL; Schema: public; Owner: -
--

REVOKE ALL ON FUNCTION public.youtube_schedule_collabo_talent_names_valid(names text[]) FROM PUBLIC;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT ALL ON FUNCTION public.youtube_schedule_collabo_talent_names_valid(names text[]) TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE observation_contract_generations; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT ON TABLE public.observation_contract_generations TO hololive_scraper;
    END IF;
END $grant$;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.observation_contract_generations TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE source_collection_checkpoints; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.source_collection_checkpoints TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_applications; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT ON TABLE public.source_observation_applications TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: SEQUENCE source_observation_applications_id_seq; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,USAGE ON SEQUENCE public.source_observation_applications_id_seq TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_collisions; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT INSERT ON TABLE public.source_observation_collisions TO hololive_scraper;
    END IF;
END $grant$;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,DELETE ON TABLE public.source_observation_collisions TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: SEQUENCE source_observation_collisions_id_seq; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT,USAGE ON SEQUENCE public.source_observation_collisions_id_seq TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_consumer_offsets; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.source_observation_consumer_offsets TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_decisions; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.source_observation_decisions TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_entity_kinds; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.source_observation_entity_kinds TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_payloads; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.source_observation_payloads TO hololive_runtime;
    END IF;
END $grant$;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT,INSERT ON TABLE public.source_observation_payloads TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: SEQUENCE source_observation_payloads_id_seq; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT,USAGE ON SEQUENCE public.source_observation_payloads_id_seq TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_provenances; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.source_observation_provenances TO hololive_runtime;
    END IF;
END $grant$;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT ON TABLE public.source_observation_provenances TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_queue; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT,INSERT ON TABLE public.source_observation_queue TO hololive_scraper;
    END IF;
END $grant$;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.source_observation_queue TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_replay_epoch; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT ON TABLE public.source_observation_replay_epoch TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE source_observation_subject_heads; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.source_observation_subject_heads TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE source_observations; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT,INSERT ON TABLE public.source_observations TO hololive_scraper;
    END IF;
END $grant$;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,DELETE ON TABLE public.source_observations TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: SEQUENCE source_observations_id_seq; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT,USAGE ON SEQUENCE public.source_observations_id_seq TO hololive_scraper;
    END IF;
END $grant$;


--
-- Name: TABLE source_reconciliation_conflicts; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,DELETE ON TABLE public.source_reconciliation_conflicts TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: SEQUENCE source_reconciliation_conflicts_id_seq; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,USAGE ON SEQUENCE public.source_reconciliation_conflicts_id_seq TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_channel_live_checks; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_channel_live_checks TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_channel_photo_heads; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_channel_photo_heads TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_channel_photo_variants; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_channel_photo_variants TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_channel_profile_evidence; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_channel_profile_evidence TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_channel_profile_heads; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_channel_profile_heads TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_channel_profiles; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_channel_profiles TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_collection_job_leases; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_collection_job_leases TO hololive_scraper;
    END IF;
END $grant$;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.youtube_collection_job_leases TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_collection_projection_generations; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT ON TABLE public.youtube_collection_projection_generations TO hololive_scraper;
    END IF;
END $grant$;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.youtube_collection_projection_generations TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: SEQUENCE youtube_collection_projection_generations_generation_seq; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,USAGE ON SEQUENCE public.youtube_collection_projection_generations_generation_seq TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_collection_projection_guard; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.youtube_collection_projection_guard TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: COLUMN youtube_collection_projection_guard.guard_key; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT UPDATE(guard_key) ON TABLE public.youtube_collection_projection_guard TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_collection_runtime_bundles; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.youtube_collection_runtime_bundles TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_collection_runtime_kinds; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.youtube_collection_runtime_kinds TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_collection_target_reasons; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.youtube_collection_target_reasons TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_collection_targets; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT ON TABLE public.youtube_collection_targets TO hololive_scraper;
    END IF;
END $grant$;
DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.youtube_collection_targets TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_content_absence_slots; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_content_absence_slots TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_content_channel_heads; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_content_channel_heads TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_content_evidence_clocks; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_content_evidence_clocks TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_live_absence_slots; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT ON TABLE public.youtube_live_absence_slots TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_live_pending_ends; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,DELETE,UPDATE ON TABLE public.youtube_live_pending_ends TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_live_reconciliation_heads; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_live_reconciliation_heads TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_live_review_receipt_facts; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.youtube_live_review_receipt_facts TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_live_review_receipts; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.youtube_live_review_receipts TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_live_runtime_state; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT ON TABLE public.youtube_live_runtime_state TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_live_sessions; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_live_sessions TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_live_time_finalizations; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_live_time_finalizations TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_schedule_items; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_schedule_items TO hololive_runtime;
    END IF;
END $grant$;


--
-- Name: TABLE youtube_video_availability; Type: ACL; Schema: public; Owner: -
--

DO $grant$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_runtime') THEN
        GRANT SELECT,INSERT,UPDATE ON TABLE public.youtube_video_availability TO hololive_runtime;
    END IF;
END $grant$;


--
-- PostgreSQL database dump complete
--


DO $settings$
BEGIN
    EXECUTE format('ALTER DATABASE %I SET idle_in_transaction_session_timeout = %L', current_database(), '5min');
    EXECUTE format('ALTER DATABASE %I SET default_toast_compression = %L', current_database(), 'lz4');
END
$settings$;
-- 생성 열의 decoder는 영수증 INSERT와 같은 invoker 권한으로 실행된다.
-- 이미 허가된 기록 역할에만 이 의존 권한을 보충하고 기록·표·역할 구성·PUBLIC 권한은 바꾸지 않는다.
DO $roles$
DECLARE
    recording_role text;
BEGIN
    FOR recording_role IN
        SELECT DISTINCT role.rolname
        FROM pg_catalog.pg_proc procedure
        CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(procedure.proacl,pg_catalog.acldefault('f',procedure.proowner))) privilege
        JOIN pg_catalog.pg_roles role ON role.oid=privilege.grantee
        WHERE procedure.oid='public.record_youtube_live_review(uuid,text,text,text,text)'::regprocedure
          AND privilege.privilege_type='EXECUTE'
    LOOP
        EXECUTE format('GRANT EXECUTE ON FUNCTION public.youtube_live_review_receipt_value(jsonb), public.lock_youtube_live_runtime_state(), public.refresh_youtube_live_runtime_state(text[]) TO %I',recording_role);
    END LOOP;
END
$roles$;

DO $roles$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        REVOKE ALL ON TYPE public.youtube_live_review_session_value, public.youtube_live_review_head_value,
            public.youtube_live_review_pending_value, public.youtube_live_review_value FROM hololive_scraper;
    END IF;
END
$roles$;
COMMIT;
