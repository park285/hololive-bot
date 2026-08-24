-- GENERATED: holobot migration epoch-2 baseline
-- Source commit: 6a06d1a5414e3a082f71f4f7360c2a8926d92bd5
-- Legacy cutoff: 139_trust_alarm_short_links.sql
-- Compatibility checkpoint: 140_epoch2_checkpoint.sql
-- IMPORTANT:
--   - execute only on a fresh database
--   - existing databases must skip this via the R1 checkpoint marker
--   - immutable after production exposure

BEGIN;

--
-- PostgreSQL database dump
--


-- Dumped from database version 18.4
-- Dumped by pg_dump version 18.4


--
-- Name: public; Type: SCHEMA; Schema: -; Owner: -
--



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
-- Name: grant_bot_reply_outbox_manual_replay(bigint, text, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.grant_bot_reply_outbox_manual_replay(requested_outbox_id bigint, operator_actor text, operator_reason text) RETURNS text
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path TO 'pg_catalog'
    AS $_$
DECLARE
    granted_at TIMESTAMPTZ := clock_timestamp();
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
-- Name: scrub_bot_webhook_inbox_terminal_payload(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.scrub_bot_webhook_inbox_terminal_payload() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.payload := '{}'::jsonb;
    RETURN NEW;
END
$$;




--
-- Name: acl_rooms; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.acl_rooms (
    id integer NOT NULL,
    room_id character varying(100) NOT NULL,
    list_type character varying(16) DEFAULT 'whitelist'::character varying NOT NULL,
    CONSTRAINT chk_acl_rooms_list_type_vocab CHECK (list_type IN ('whitelist', 'blacklist'))
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
    id integer NOT NULL,
    key character varying(64) NOT NULL,
    value text
);


--
-- Name: acl_settings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.acl_settings_id_seq
    AS integer
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
    CONSTRAINT alarm_dispatch_deliveries_attempt_check CHECK ((attempt_count >= 0)),
    CONSTRAINT alarm_dispatch_deliveries_dedupe_key_check CHECK (((length(dedupe_key) > 0) AND (length(dedupe_key) <= 768))),
    CONSTRAINT alarm_dispatch_deliveries_room_id_check CHECK (((length((room_id)::text) > 0) AND (length((room_id)::text) <= 100))),
    CONSTRAINT alarm_dispatch_deliveries_status_check CHECK ((status = ANY (ARRAY['shadowed'::text, 'pending'::text, 'retry'::text, 'leased'::text, 'sending'::text, 'sent'::text, 'dlq'::text, 'quarantined'::text, 'cancelled'::text]))),
    CONSTRAINT chk_alarm_dispatch_deliveries_last_error_size CHECK ((octet_length(last_error) <= 8192)),
    CONSTRAINT chk_alarm_dispatch_deliveries_state_shape CHECK ((((status <> 'leased'::text) OR ((locked_by IS NOT NULL) AND (locked_at IS NOT NULL) AND (lock_expires_at IS NOT NULL))) AND ((status <> 'sending'::text) OR ((locked_by IS NOT NULL) AND (locked_at IS NOT NULL) AND (lock_expires_at IS NOT NULL) AND (sending_started_at IS NOT NULL))) AND ((status <> 'sent'::text) OR (sent_at IS NOT NULL)) AND ((status <> 'dlq'::text) OR (dlq_at IS NOT NULL)) AND ((status <> 'quarantined'::text) OR (quarantined_at IS NOT NULL)) AND ((status <> 'cancelled'::text) OR (cancelled_at IS NOT NULL))))
)
WITH (autovacuum_vacuum_scale_factor='0.02', autovacuum_vacuum_threshold='50', autovacuum_analyze_scale_factor='0.02', autovacuum_analyze_threshold='50');


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
-- Name: alarms; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alarms (
    id integer NOT NULL,
    room_id character varying(100) NOT NULL,
    user_id character varying(64) NOT NULL,
    channel_id character varying(64) NOT NULL,
    member_name text,
    room_name character varying(255),
    user_name character varying(200),
    created_at timestamp with time zone DEFAULT now(),
    alarm_types public.alarm_type[] DEFAULT ARRAY['LIVE'::public.alarm_type] NOT NULL
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
-- Name: auth_password_reset_tokens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auth_password_reset_tokens (
    token_hash text NOT NULL,
    user_id text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: auth_users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auth_users (
    id text NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    display_name text NOT NULL,
    avatar_url text,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


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
    CONSTRAINT chk_bot_reply_outbox_state_shape CHECK ((((status <> ALL (ARRAY['submitting'::text, 'accepted'::text])) OR ((claim_token IS NOT NULL) AND (lease_until IS NOT NULL) AND (first_attempt_at IS NOT NULL))) AND ((status <> 'accepted'::text) OR (length(iris_request_id) > 0)) AND ((status = ANY (ARRAY['handoff_completed'::text, 'dead'::text, 'permanent_conflict'::text])) OR (payload IS NOT NULL)))),
    CONSTRAINT chk_bot_reply_outbox_status_vocab CHECK ((status = ANY (ARRAY['pending'::text, 'submitting'::text, 'accepted'::text, 'handoff_completed'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text, 'dead'::text, 'permanent_conflict'::text, 'manual_review'::text])))
);


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
    CONSTRAINT chk_bot_reply_outbox_replay_audit_reason CHECK (octet_length(reason) BETWEEN 1 AND 256 AND reason !~ '[[:cntrl:]]')
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
    aliases jsonb,
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
    CONSTRAINT chk_members_graduated_sync CHECK ((is_graduated = (status = 'graduated'::text))),
    CONSTRAINT chk_members_status_vocab CHECK ((status = ANY (ARRAY[('active'::character varying)::text, ('graduated'::character varying)::text])))
);


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
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_at timestamp with time zone,
    sent_at timestamp with time zone,
    error text,
    locked_by text,
    lock_expires_at timestamp with time zone,
    sending_started_at timestamp with time zone,
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
    updated_at timestamp with time zone DEFAULT now() NOT NULL
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
-- Name: youtube_channel_latest_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_channel_latest_stats (
    channel_id character varying(64) NOT NULL,
    member_name text,
    subscribers bigint,
    videos bigint,
    views bigint,
    "time" timestamp with time zone NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP
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
-- Name: youtube_channel_stats_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_channel_stats_snapshots (
    channel_id character varying(64) NOT NULL,
    captured_at timestamp with time zone NOT NULL,
    subscriber_count bigint DEFAULT 0 NOT NULL,
    view_count bigint DEFAULT 0 NOT NULL,
    video_count bigint DEFAULT 0 NOT NULL,
    joined_date bigint,
    description text,
    country character varying(50),
    handle character varying(100)
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
    authorized_at timestamp with time zone,
    alarm_sent_at timestamp with time zone,
    delivery_status text DEFAULT 'DETECTED'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_community_shorts_alarm_states_delivery_status_vocab CHECK ((delivery_status = ANY (ARRAY[('DETECTED'::character varying)::text, ('ENQUEUED'::character varying)::text, ('SENT'::character varying)::text]))),
    CONSTRAINT chk_youtube_community_shorts_alarm_states_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text, 'MILESTONE'::text])))
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
    CONSTRAINT chk_youtube_community_shorts_source_posts_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text, 'MILESTONE'::text])))
);


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
    CONSTRAINT chk_youtube_content_alarm_tracking_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text, 'MILESTONE'::text])))
)
WITH (autovacuum_vacuum_scale_factor='0.05', autovacuum_vacuum_threshold='100', autovacuum_analyze_scale_factor='0.05', autovacuum_analyze_threshold='100');


--
-- Name: youtube_content_watermarks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_content_watermarks (
    channel_id character varying(64) NOT NULL,
    watermark_type character varying(20) NOT NULL,
    initialized boolean DEFAULT false NOT NULL,
    last_content_id character varying(50),
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chk_youtube_content_watermarks_watermark_type_vocab CHECK (watermark_type IN ('VIDEO', 'SHORT', 'COMMUNITY_POST'))
);


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
    CONSTRAINT chk_youtube_live_sessions_status_vocab CHECK ((status = ANY (ARRAY[('UPCOMING'::character varying)::text, ('LIVE'::character varying)::text, ('ENDED'::character varying)::text])))
);


--
-- Name: youtube_live_viewer_samples; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_live_viewer_samples (
    video_id character varying(20) NOT NULL,
    captured_at timestamp with time zone NOT NULL,
    channel_id character varying(64) NOT NULL,
    concurrent_viewers integer DEFAULT 0 NOT NULL
);


--
-- Name: youtube_milestone_approaching; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_milestone_approaching (
    id integer NOT NULL,
    channel_id character varying(64) NOT NULL,
    milestone_value bigint NOT NULL,
    notified_at timestamp with time zone DEFAULT now() NOT NULL,
    current_subs bigint NOT NULL,
    chat_notified boolean DEFAULT false NOT NULL
);


--
-- Name: youtube_milestone_approaching_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.youtube_milestone_approaching_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: youtube_milestone_approaching_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.youtube_milestone_approaching_id_seq OWNED BY public.youtube_milestone_approaching.id;


--
-- Name: youtube_milestones; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_milestones (
    id integer NOT NULL,
    channel_id character varying(64) NOT NULL,
    member_name text NOT NULL,
    type character varying(20) NOT NULL,
    value bigint NOT NULL,
    achieved_at timestamp with time zone DEFAULT now() NOT NULL,
    notified boolean DEFAULT false NOT NULL
);


--
-- Name: youtube_milestones_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.youtube_milestones_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: youtube_milestones_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.youtube_milestones_id_seq OWNED BY public.youtube_milestones.id;


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
    CONSTRAINT chk_youtube_notification_outbox_kind_vocab CHECK ((kind = ANY (ARRAY['NEW_VIDEO'::text, 'NEW_SHORT'::text, 'LIVE_STREAM'::text, 'COMMUNITY_POST'::text, 'MILESTONE'::text]))),
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
-- Name: youtube_stats_changes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_stats_changes (
    id integer NOT NULL,
    channel_id character varying(64) NOT NULL,
    member_name text,
    subscriber_change bigint DEFAULT 0 NOT NULL,
    video_change bigint DEFAULT 0 NOT NULL,
    view_change bigint DEFAULT 0 NOT NULL,
    previous_subs bigint,
    current_subs bigint,
    previous_videos bigint,
    current_videos bigint,
    detected_at timestamp with time zone DEFAULT now() NOT NULL,
    notified boolean DEFAULT false NOT NULL
);


--
-- Name: youtube_stats_changes_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.youtube_stats_changes_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: youtube_stats_changes_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.youtube_stats_changes_id_seq OWNED BY public.youtube_stats_changes.id;


--
-- Name: youtube_stats_history; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_stats_history (
    "time" timestamp with time zone NOT NULL,
    channel_id character varying(64) NOT NULL,
    member_name text,
    subscribers bigint,
    videos bigint,
    views bigint
);


--
-- Name: youtube_stream_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.youtube_stream_stats (
    video_id character varying(20) NOT NULL,
    channel_id character varying(64) NOT NULL,
    started_at timestamp with time zone,
    ended_at timestamp with time zone,
    max_concurrent_viewers integer DEFAULT 0,
    avg_concurrent_viewers integer DEFAULT 0,
    sample_count integer DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
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
-- Name: youtube_milestone_approaching id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_milestone_approaching ALTER COLUMN id SET DEFAULT nextval('public.youtube_milestone_approaching_id_seq'::regclass);


--
-- Name: youtube_milestones id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_milestones ALTER COLUMN id SET DEFAULT nextval('public.youtube_milestones_id_seq'::regclass);


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
-- Name: youtube_stats_changes id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_stats_changes ALTER COLUMN id SET DEFAULT nextval('public.youtube_stats_changes_id_seq'::regclass);


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
-- Data for Name: alarm_dispatch_deliveries; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_dispatch_event_collisions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarm_dispatch_events; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: alarms; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: auth_password_reset_tokens; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: auth_users; Type: TABLE DATA; Schema: public; Owner: -
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
-- Data for Name: bot_webhook_heads; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: bot_webhook_inbox; Type: TABLE DATA; Schema: public; Owner: -
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

INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (2, 'sameko-saba', 'UCxsZ6NCzjU_t4YSxQLBcM5A', 'Sameko Saba', '鮫子サバ', '사메코 사바', 'active', false, '{"ja": ["サバ", "鮫子"], "ko": ["사바", "사메코"]}', NULL, NULL, 'Independents', NULL, 'manual', NULL, NULL, '사바', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (1, 'yuuki-sakuna', 'UCrV1Hf5r8P148idjoSfrGEQ', 'Yuuki Sakuna', '結城さくな', '유우키 사쿠나', 'active', false, '{"ja": ["さくな", "さくたん"], "ko": ["사쿠나", "사쿠탄"]}', NULL, NULL, 'Independents', NULL, 'manual', NULL, NULL, '사쿠나', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (10, 'hanako-nana', 'UCcA21_PzN1EhNe7xS4MJGsQ', 'Hanako Nana', '華子ナナ', '하나코 나나', 'active', false, '{"ja": ["華子", "ナナ"], "ko": ["나나", "나교수님", "77년생", "쌍칠아재", "굴리트"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '4d812b586ff63f8a2946e64fa860bbf5', NULL, '나나', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (8, 'akane-lize', 'UC9m5xP6u69zXpD7MscY-uYQ', 'Akane Lize', '朱音リゼ', '아카네 리제', 'active', false, '{"ja": ["朱音", "リゼ"], "ko": ["리제", "리제황녀", "피엔나", "저챗퀸", "천마"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '4325b1d5bbc321fad3042306646e2e50', NULL, '리제', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (4, 'ayatsuno-yuni', 'UClbYIn9LDbbFZ9w2shX3K0g', 'Ayatsuno Yuni', '純角ユニ', '아야츠노 유니', 'active', false, '{"ja": ["純角", "ユニ"], "ko": ["유니", "유니링", "유니찌", "정윤희"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '45e71a76e949e16a34764deb962f9d9f', NULL, '유니', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (3, 'airi-kanna', 'UC6YnTqZidFg4WUiXpiCtSSQ', 'Airi Kanna', '藍里かんな', '아이리 칸나', 'graduated', true, '{"ja": ["藍里", "かんな"], "ko": ["칸나", "대장용", "락용", "간나"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '82136e09328ffc9143924707293a566d', NULL, '칸나', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (5, 'arahashi-tabi', 'UCq-U-D8O6_6e4X6r-z9V0w', 'Arahashi Tabi', '荒橋タビ', '아라하시 타비', 'active', false, '{"ja": ["荒橋", "タビ"], "ko": ["타비", "뿡댕이", "댕댕이", "닌자타비"]}', NULL, NULL, 'Stellive', NULL, 'holodex', 'a6c4ddb09cdb160478996007bff35296', NULL, '타비', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (6, 'shirayuki-hina', 'UC99CUC6yR6O_uXyS_3K7yKA', 'Shirayuki Hina', '白雪ひな', '시라유키 히나', 'active', false, '{"ja": ["白雪", "ひな"], "ko": ["히나", "히나피", "공주", "흰눈곰", "존 히나"]}', NULL, NULL, 'Stellive', NULL, 'holodex', 'b044e3a3b9259246bc92e863e7d3f3b8', NULL, '히나', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (7, 'neneko-mashiro', 'UC9o9D7U5O8V0A-zO0v7UeLw', 'Neneko Mashiro', '音々子ましろ', '네네코 마시로', 'active', false, '{"ja": ["音々子", "ましろ"], "ko": ["마시로", "시로", "밍대장", "밍"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '4515b179f86b67b4981e16190817c580', NULL, '마시로', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (9, 'tenko-shibuki', 'UCYxLMfeX1CbMBll9MsGlzmw', 'Tenko Shibuki', '天鼓紫吹', '텐코 시부키', 'active', false, '{"ja": ["天鼓", "紫吹"], "ko": ["시부키", "부키", "북대장", "땡코 시부키"]}', NULL, NULL, 'Stellive', NULL, 'holodex', '64d76089fba26b180d9c9e48a32600d9', NULL, '시부키', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (11, 'tachibana-hinano', 'UCvUc0m317LWTTPZoBQV479A', 'Tachibana Hinano', '橘ひなの', '타치바나 히나노', 'active', false, '{"ja": ["橘ひなの", "ひなの"], "ko": ["타치바나 히나노", "히나노", "히나땅"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'hinanotachiba7', '히나노', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (13, 'kaga-nazuna', 'UCiMG6VdScBabPhJ1ZtaVmbw', 'Kaga Nazuna', '花芽なずな', '카가 나즈나', 'active', false, '{"ja": ["花芽なずな", "なずな"], "ko": ["카가 나즈나", "나즈나"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'nazunakaga', '나즈나', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (12, 'ichinose-uruha', 'UC5LyYg6cCA4yHEYvtUsir3g', 'Ichinose Uruha', '一ノ瀬うるは', '이치노세 우루하', 'active', false, '{"ja": ["一ノ瀬うるは", "うるは"], "ko": ["이치노세 우루하", "우루하"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'uruhaichinose', '우루하', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (15, 'yakumo-beni', 'UCjXBuHmWkieBApgBhDuJMMQ', 'Yakumo Beni', '八雲べに', '야쿠모 베니', 'active', false, '{"ja": ["八雲べに", "べに"], "ko": ["야쿠모 베니", "베니"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'yakumobeni', '베니', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (14, 'kaminari-qpi', 'UCMp55EbT_ZlqiMS3lCj01BQ', 'Kaminari Qpi', '神成きゅぴ', '카미나리 큐피', 'active', false, '{"ja": ["神成きゅぴ", "きゅぴ"], "ko": ["카미나리 큐피", "큐피"]}', NULL, NULL, 'VSPO', NULL, 'manual', NULL, 'kaminariqpi', '큐피', NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (16, 'mekpark-achrora', 'UChpRPsAeSZn5DistGacR3iA', 'ACHRORA', 'ACHRORA', '아크로라', 'active', false, '{"ja": ["アクロラ", "ACHRORA"], "ko": ["아크로라", "ACHRORA", "멕파크", "mekPark"]}', NULL, NULL, 'mekPark', NULL, 'manual', NULL, NULL, NULL, NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (17, 'mekpark-unit-b', 'UC3OH5FKQ3qtl4uRme_vZTgA', 'Unit B', 'UNIT B', '유닛 B', 'active', false, '{"ja": ["UNIT B", "宵凪ネオン", "玲銘ミラ", "清澄ライラ"], "ko": ["유닛비", "유닛 B", "UNIT B", "멕파크", "mekPark"]}', NULL, NULL, 'mekPark', NULL, 'manual', NULL, NULL, NULL, NULL, NULL);
INSERT INTO public.members (id, slug, channel_id, english_name, japanese_name, korean_name, status, is_graduated, aliases, photo, photo_updated_at, org, suborg, sync_source, chzzk_channel_id, twitch_user_id, short_korean_name, birthday, debut_date) VALUES (18, 'shigure-ui', 'UCt30jJgChL8qeT9VPadidSw', 'Shigure Ui', 'しぐれうい', '시구레 우이', 'active', false, '{"ja": ["しぐれうい", "ういママ", "うい"], "ko": ["시구레우이", "시구레 우이", "우이", "우이마마"]}', NULL, NULL, 'Hololive', NULL, 'manual', NULL, NULL, '우이', NULL, NULL);


--
-- Data for Name: message_strings; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (1, 'org', 'Hololive', 'Holo', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (2, 'org', 'Nijisanji', '니지산지', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (3, 'org', 'Independents', '개인세', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (4, 'org', 'Stellive', '스텔라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (5, 'alarmtype', 'LIVE', '방송', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (6, 'alarmtype', 'COMMUNITY', '커뮤니티', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (7, 'alarmtype', 'SHORTS', '쇼츠', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (8, 'alarmtype', 'BIRTHDAY', '생일', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (9, 'alarmtype', 'ANNIVERSARY', '주년', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (10, 'alarmtype', 'ALL', '전체', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (11, 'newscat', 'birthday_live', '생일 라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (12, 'newscat', 'solo_live', '솔로 라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (13, 'newscat', 'collab', '콜라보', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (14, 'newscat', 'event', '이벤트', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (15, 'newscat', 'goods', '굿즈', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (16, 'newscat', 'other', '기타', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (17, 'social', '歌の再生リスト', '음악 플레이리스트', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (18, 'social', '公式グッズ', '공식 굿즈', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (19, 'social', 'オフィシャルグッズ', '공식 굿즈', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (20, 'misc', 'vtuber_fallback', 'VTuber', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (21, 'misc', 'chzzk_title', '치지직 라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (22, 'error', 'no_member_info_found', '❌ 등록된 멤버 정보를 찾을 수 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (67, 'misc', 'time_unknown', '시간 미정', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (68, 'misc', 'alarm_unknown_member', '알 수 없는 멤버', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (69, 'misc', 'alarm_no_title', '제목 없음', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (70, 'misc', 'alarm_no_stream', '방송 정보 없음', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (74, 'calendar', 'day', '%d월 %d일', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (71, 'calendar', 'header_month', '%d년 %d월 기념일', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (72, 'calendar', 'summary', '총 %d건 · 생일 %d · 데뷔주년 %d', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (73, 'calendar', 'empty', '등록된 기념일이 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (75, 'calendar', 'badge_birthday', '생일', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (76, 'calendar', 'badge_anniversary', '데뷔 %d주년', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (77, 'calendar', 'unknown', '알 수 없음', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (23, 'error', 'cannot_display_member_info', '❌ 멤버 정보를 표시할 수 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (24, 'error', 'member_profile_load_failed', '❌ 프로필을 불러오는 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (25, 'error', 'member_profile_build_failed', '❌ 프로필을 구성하지 못했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (26, 'error', 'graduated_member_blocked', '⚠️ 졸업한 멤버입니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (27, 'error', 'alarm_service_not_initialized', '❌ 알람 서비스가 초기화되지 않았습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (28, 'error', 'alarm_add_failed', '❌ 알람 설정 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (29, 'error', 'alarm_remove_failed', '❌ 알람 제거 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (30, 'error', 'alarm_list_failed', '❌ 알람 목록 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (31, 'error', 'alarm_clear_failed', '❌ 알람 초기화 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (32, 'error', 'alarm_need_member_name_add', '❌ 멤버 이름을 입력해주세요.
예) !알람 추가 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (33, 'error', 'alarm_need_member_name_remove', '❌ 멤버 이름을 입력해주세요.
예) !알람 제거 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (34, 'error', 'invalid_alarm_usage', '❌ 지원하지 않는 알람 명령입니다.
예) !알람 추가 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (35, 'error', 'live_stream_query_failed', '❌ 라이브 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (36, 'error', 'upcoming_stream_query_failed', '❌ 예정 방송 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (37, 'error', 'schedule_query_failed', '❌ 일정 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (38, 'error', 'schedule_need_member_name', '❌ 멤버 이름을 입력해주세요.
예) !일정 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (39, 'error', 'unknown_stats_period', '❌ 알 수 없는 통계 유형입니다. !도움말을 참고해주세요.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (40, 'error', 'stats_query_failed', '❌ 구독자 순위 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (41, 'error', 'no_stats_data', '❌ 해당 기간의 통계 데이터가 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (42, 'error', 'subscriber_need_member_name', '❌ 멤버 이름을 입력해주세요.
예) !구독자 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (43, 'error', 'subscriber_query_failed', '❌ 구독자 정보 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (44, 'error', 'no_subscriber_data', '❌ 해당 멤버의 구독자 정보가 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (45, 'error', 'calendar_query_failed', '❌ 기념일 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (46, 'error', 'major_event_service_not_initialized', '❌ 행사 알림 서비스가 초기화되지 않았습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (47, 'error', 'major_event_status_check_failed', '❌ 행사 알림 상태 확인 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (48, 'error', 'major_event_subscribe_failed', '❌ 행사 알림 설정 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (49, 'error', 'major_event_unsubscribe_failed', '❌ 행사 알림 해제 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (50, 'error', 'member_news_service_not_initialized', '❌ 뉴스 서비스가 초기화되지 않았습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (51, 'error', 'member_news_query_failed', '❌ 뉴스 조회 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (52, 'error', 'member_news_subscription_failed', '❌ 뉴스 알림 설정 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (53, 'error', 'unknown_command', '❌ 알 수 없는 명령입니다.
!도움말에서 사용 가능한 명령을 확인할 수 있습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (54, 'error', 'external_api_call_failed', '❌ 외부 데이터 조회 중 오류가 발생했습니다. 잠시 후 다시 시도해주세요.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (55, 'error', 'cache_connection_failed', '❌ 일시적인 문제로 요청을 처리하지 못했습니다. 잠시 후 다시 시도해주세요.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (56, 'error', 'iris_connection_failed', '❌ 서버 연결에 실패했습니다. 잠시 후 다시 시도해주세요.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (57, 'error', 'command_processing_failed', '❌ 명령 처리 중 오류가 발생했습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (58, 'error', 'async_command_backpressure', '❌ 요청이 많아 잠시 후 다시 시도해주세요.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (59, 'notify', 'member_news_no_members', '📰 뉴스 대상 멤버가 없습니다.
예) !알람 추가 페코라', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (60, 'notify', 'member_news_subscribed', '✅ 뉴스 알림을 켰습니다.
매주 월요일 09:00 KST에 발송됩니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (61, 'notify', 'member_news_already_subscribed', 'ℹ️ 뉴스 알림이 이미 켜져 있습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (62, 'notify', 'member_news_unsubscribed', '✅ 뉴스 알림을 껐습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (63, 'notify', 'member_news_not_subscribed', 'ℹ️ 뉴스 알림이 이미 꺼져 있습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (64, 'notify', 'member_news_status_on', '🔔 뉴스 알림: 켜짐
- 발송: 매주 월요일 09:00 KST
- 해제: !뉴스알림 끄기', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (65, 'notify', 'member_news_status_off', '🔕 뉴스 알림: 꺼짐
- 설정: !뉴스알림 켜기', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (66, 'notify', 'graduated_member_warning', '⚠️ 졸업한 멤버입니다.

', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (123, 'timefmt', 'stream_time_days', '%s (%d일 후)', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (124, 'timefmt', 'stream_time_hours_minutes', '%s (%d시간 %d분 후)', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (125, 'timefmt', 'stream_time_minutes', '%s (%d분 후)', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (126, 'timefmt', 'relative_days', '%d일 후', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (127, 'timefmt', 'relative_hours_minutes', '%d시간 %d분 후', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (128, 'timefmt', 'relative_minutes', '%d분 후', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (129, 'karing', 'alarm_title_prelive', '방송 %d분 전 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (130, 'karing', 'alarm_title_live', '라이브 시작', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (131, 'karing', 'time_left_prelive', '%d분 후 시작', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (132, 'karing', 'time_left_live', '지금 시작', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (133, 'karing', 'outbox_title_community', '커뮤니티 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (134, 'karing', 'outbox_time_community', '새 커뮤니티', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (135, 'karing', 'outbox_title_shorts', '쇼츠 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (136, 'karing', 'outbox_time_shorts', '새 쇼츠', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (137, 'karing', 'outbox_title_video', '새 영상', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (138, 'karing', 'outbox_time_video', '새 영상', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (139, 'karing', 'outbox_title_live', '방송 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (140, 'karing', 'outbox_time_live', '방송 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (141, 'karing', 'title_fallback', '알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (142, 'karing', 'time_fallback', '새 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (143, 'karing', 'count_suffix', '%s · %d건', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (144, 'karing', 'item_title_community_fallback', '커뮤니티 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (145, 'karing', 'status_community', '커뮤니티', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (146, 'karing', 'status_shorts', '쇼츠', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (147, 'karing', 'status_video', '새 영상', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (148, 'karing', 'status_fallback', '알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (149, 'calendar', 'overflow_footer', '외 %d건 생략', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (150, 'livecard', 'header', '현재 라이브', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (151, 'livecard', 'summary', '총 %d건', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (152, 'livecard', 'badge_chzzk', '치지직', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (153, 'livecard', 'overflow_footer', '외 %d건 생략', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (154, 'profilecard', 'badge_graduated', '졸업', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (155, 'rankcard', 'header', '구독자 증가 순위', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (156, 'rankcard', 'summary', '%s · 상위 %d', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (157, 'rankcard', 'total', '구독자 %s', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (158, 'karing', 'alarm_title_prelive_premiere', '선행공개 %d분 전 알림', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (159, 'karing', 'alarm_title_live_premiere', '선행공개 시작', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (160, 'karing', 'outbox_title_video_premiere', '%d분 후 공개 예정', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (161, 'karing', 'outbox_time_video_premiere', '%d분 후 공개', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.message_strings (id, namespace, key, value, created_at, updated_at) VALUES (162, 'karing', 'status_video_premiere', '최초공개', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');


--
-- Data for Name: notification_delivery_outbox; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: notification_template_revisions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: notification_templates; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (20, 'CMD_MILESTONE_APPROACHING', NULL, '📊 **{{mdsafe .MemberName}}** 구독자 {{.Milestone}}명까지 {{.Remaining}}명 남았습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (103, 'CELEBRATION_BIRTHDAY_STREAM', NULL, '🎂 **{{mdsafe .MemberName}}** 생일 방송 일정이 잡혔습니다!
{{- if and .StreamTitle .StreamURL}}
- [{{mdsafe .StreamTitle}}]({{.StreamURL}})
{{- else if .StreamTitle}}
- {{mdsafe .StreamTitle}}
{{- else if .StreamURL}}
- {{.StreamURL}}
{{- end}}
{{- if .ScheduledStartKST}}
- ⏰ {{.ScheduledStartKST}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (6, 'OUTBOX_COMMUNITY', NULL, '🔔 **{{mdsafe .MemberName}}** 커뮤니티 글
{{- if .ContentText}}
{{mdsafe (truncate 100 .ContentText)}}
{{- end}}
{{- if .URL}}
[커뮤니티 글 보기]({{.URL}})
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (7, 'OUTBOX_VIDEO', NULL, '{{if eq .Kind "LIVE_STREAM"}}🔴 **{{mdsafe .MemberName}}** 방송 시작{{else if .IsUpcomingPremiere}}🔔 **{{mdsafe .MemberName}}** {{.MinutesUntilPremiere}}분 후 공개 예정{{else if .IsPremiere}}🔔 **{{mdsafe .MemberName}}** 최초공개{{else}}🔔 **{{mdsafe .MemberName}}** 새 영상{{end}}
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (5, 'OUTBOX_SHORTS', NULL, '🔔 **{{mdsafe .MemberName}}** 새 쇼츠
{{- if and .Title .URL}}
[{{mdsafe (truncate 50 .Title)}}]({{.URL}})
{{- else if .Title}}
{{mdsafe (truncate 50 .Title)}}
{{- else if .URL}}
{{.URL}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (8, 'OUTBOX_MILESTONE', NULL, '🎉 **{{mdsafe .MemberName}}** {{mdsafe .Milestone}} 달성', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (17, 'CMD_ALARM_REMOVED', NULL, '{{- if .Removed -}}
✅ **{{mdsafe .MemberName}}** 알람을 해제했습니다.
{{- else -}}
ℹ️ **{{mdsafe .MemberName}}** 알람이 설정되어 있지 않습니다.
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (41, 'CMD_STATS_COUNT', NULL, '📊 **{{mdsafe .MemberName}}** 구독자 {{.Subscribers}}명', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (40, 'CMD_PROFILE', NULL, '{{- if eq (len .Names) 0 -}}
## 👤 멤버 정보
{{- else -}}
## 👤 {{mdsafe (index .Names 0)}}{{if gt (len .Names) 1}} ({{mdsafe (join (slice .Names 1) " / ")}}){{end}}
{{- end}}
{{- if .Catchphrase}}
"{{mdsafe .Catchphrase}}"
{{- end}}
{{- if .Summary}}
{{mdsafe .Summary}}
{{- end}}
{{- if .Highlights}}

**하이라이트**
{{- range .Highlights}}
- {{mdsafe .}}
{{- end}}
{{- end}}
{{- if .DataRows}}

**프로필**
{{- range .DataRows}}
{{- if .Multiline}}
- {{mdsafe .Label}}:
{{mdsafe .Value}}
{{- else}}
- {{mdsafe .Label}}: {{mdsafe .Value}}
{{- end}}
{{- end}}
{{- end}}
{{- if .SocialLinks}}

**링크**
{{- range .SocialLinks}}
- [{{mdsafe .Label}}]({{.URL}})
{{- end}}
{{- end}}
{{- if .OfficialURL}}

[공식 프로필]({{.OfficialURL}})
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (48, 'CELEBRATION_BIRTHDAY', NULL, '🎂 **{{mdsafe .MemberName}}**{{if gt .Ordinal 0}} {{.Ordinal}}번째{{end}} 생일 축하합니다!{{if .ChannelID}}
[YouTube 채널 보기](https://youtube.com/channel/{{.ChannelID}}){{end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (49, 'CELEBRATION_ANNIVERSARY', NULL, '🎉 **{{mdsafe .MemberName}}** 데뷔 {{.Years}}주년 축하합니다!{{if .ChannelID}}
[YouTube 채널 보기](https://youtube.com/channel/{{.ChannelID}}){{end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (15, 'CMD_CHANNEL_SCHEDULE', NULL, '{{- if not .ChannelName -}}
❌ 채널 정보를 찾을 수 없습니다.
{{- else if eq .Count 0 -}}
📅 **{{mdsafe .ChannelName}}**
{{.Days}}일 이내 예정된 방송이 없습니다.
{{- else -}}
## 📅 {{mdsafe .ChannelName}} 일정 ({{.Days}}일 이내, {{.Count}})
{{range .Streams}}
{{- if .IsLive}}
- 🔴 방송 중
{{- else}}
- ⏰ {{.TimeInfo}}
{{- end}}
{{- if and .Title .URL}}
  [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
  {{mdsafe .Title}}
{{- else if .URL}}
  {{.URL}}
{{- end}}
{{- end -}}
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (14, 'CMD_MEMBER_DIRECTORY', NULL, '{{- if eq (len .Groups) 0 -}}
👤 등록된 멤버가 없습니다.
{{- else -}}
## 👤 멤버 목록 ({{.Total}})
{{- range .Groups}}

**{{mdsafe .GroupName}}**
{{- range .Members}}
{{- if .ShowBoth}}
- {{mdsafe .Primary}} ({{mdsafe .Secondary}})
{{- else if .Primary}}
- {{mdsafe .Primary}}
{{- else if .Secondary}}
- {{mdsafe .Secondary}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (19, 'CMD_MILESTONE_ACHIEVED', NULL, '🎉 **{{mdsafe .MemberName}}** 구독자 {{.Milestone}}명 달성!', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (26, 'CMD_MAJOR_EVENT_UNSUBSCRIBED', NULL, '✅ 행사 알림을 껐습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (27, 'CMD_MAJOR_EVENT_ALREADY_SUB', NULL, 'ℹ️ 행사 알림이 이미 켜져 있습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (36, 'CMD_MEMBER_NEWS_UNSUBSCRIBED', NULL, '✅ 뉴스 알림을 껐습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (37, 'CMD_MEMBER_NEWS_ALREADY_SUB', NULL, 'ℹ️ 뉴스 알림이 이미 켜져 있습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (38, 'CMD_MEMBER_NEWS_NOT_SUB', NULL, 'ℹ️ 뉴스 알림이 이미 꺼져 있습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (13, 'CMD_HELP', NULL, '홀로라이브 봇 명령어

[방송]
  {{.Prefix}}라이브 - 방송 중 목록
  {{.Prefix}}라이브 [멤버명] - 멤버 라이브 확인
  {{.Prefix}}예정 - 예정 방송 목록
  {{.Prefix}}예정 [멤버명] - 멤버 예정 방송
  {{.Prefix}}멤버 [이름] - 일주일 이내 방송 일정
  {{.Prefix}}방송이력/방송기록 [멤버명] [타입] - 종료된 방송 이력
  {{.Prefix}}방송이력 경마 30 - 최근 30일 경마
  {{.Prefix}}방송기록 페코라 게임 - 멤버·타입 필터
  {{.Prefix}}방송이력 카테고리:게임 14일 개수:10 - 타입·기간·개수
  타입: 게임/잡담/노래/ASMR/멤버십/이벤트/경마/동시시청/뉴스/기타/미분류
  {{.Prefix}}방송이력 썸네일 [video_id] - 종료 방송 썸네일

[멤버]
  {{.Prefix}}멤버 - 전체 멤버 목록
  {{.Prefix}}정보 [멤버명] - 프로필 조회

[알람]
  {{.Prefix}}알람 추가 [멤버명]
  {{.Prefix}}알람 제거 [멤버명]
  {{.Prefix}}알람 목록
  {{.Prefix}}알람 초기화

[뉴스]
  {{.Prefix}}뉴스 - 주간 뉴스 요약
  {{.Prefix}}뉴스알림 켜기 / 끄기 / 상태

[행사]
  {{.Prefix}}행사 - 행사 알림 상태
  {{.Prefix}}행사 켜기 / 끄기

[기념일]
  {{.Prefix}}기념일 - 이번 달 생일·주년
  {{.Prefix}}기념일 다음달 / 저번달

[기타]
  {{.Prefix}}구독자 [멤버명] - 구독자 수
  {{.Prefix}}도움말 - 도움말', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (16, 'CMD_ALARM_ADDED', NULL, '{{- if .Added -}}
✅ **{{mdsafe .MemberName}}** 알람을 설정했습니다. 방송 시작 5분 전에 알립니다.
{{- if .NextStream}}
{{- if eq .NextStream.Status "live"}}
- 🔴 방송 중
{{- if and .NextStream.Title .NextStream.URL}}
- [{{mdsafe .NextStream.Title}}]({{.NextStream.URL}})
{{- else if .NextStream.Title}}
- {{mdsafe .NextStream.Title}}
{{- else if .NextStream.URL}}
- {{.NextStream.URL}}
{{- end}}
{{- else if eq .NextStream.Status "upcoming"}}
- ⏰ {{if .NextStream.StartingSoon}}곧 시작{{else}}{{.NextStream.ScheduledKST}}{{if .NextStream.TimeDetail}} ({{.NextStream.TimeDetail}}){{end}}{{end}}
{{- if and .NextStream.Title .NextStream.URL}}
- [{{mdsafe .NextStream.Title}}]({{.NextStream.URL}})
{{- else if .NextStream.Title}}
- {{mdsafe .NextStream.Title}}
{{- else if .NextStream.URL}}
- {{.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- else -}}
ℹ️ **{{mdsafe .MemberName}}** 알람이 이미 설정되어 있습니다.
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (31, 'CMD_MAJOR_EVENT_MONTHLY_SUMMARY', NULL, '## 📅 이번 달 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if and $event.Title $event.Link}}
{{add $index 1}}. [{{mdsafe $event.Title}}]({{$event.Link}})
{{- else if $event.Title}}
{{add $index 1}}. {{mdsafe $event.Title}}
{{- else}}
{{add $index 1}}. {{$event.Link}}
{{- end}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{mdsafe $event.Members}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (28, 'CMD_MAJOR_EVENT_NOT_SUB', NULL, 'ℹ️ 행사 알림이 꺼져 있습니다.
- 설정: `{{.Prefix}}행사 켜기`', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (29, 'CMD_MAJOR_EVENT_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 행사 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
- 해제: `{{.Prefix}}행사 끄기`
{{- else}}
- 설정: `{{.Prefix}}행사 켜기`
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (30, 'CMD_MAJOR_EVENT_USAGE', NULL, '🔔 행사 알림 명령어
- `{{.Prefix}}행사 켜기 / 끄기 / 상태`', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (33, 'CMD_MEMBER_NEWS_DIGEST', NULL, '{{- if .Headline -}}
## {{mdsafe .Headline}}
{{- else -}}
## 📰 멤버 뉴스
{{- end -}}
{{- if eq (len .TopItems) 0 }}
표시할 뉴스가 없습니다.
{{- else }}
{{range $index, $item := .TopItems}}
{{add $index 1}}. {{$item.DateText}} · **{{mdsafe $item.Member}}** · {{mdsafe $item.Category}}
{{- if and $item.Title $item.SourceURL}}
   [{{mdsafe $item.Title}}]({{$item.SourceURL}})
{{- else if $item.Title}}
   {{mdsafe $item.Title}}
{{- else if $item.SourceURL}}
   {{$item.SourceURL}}
{{- end}}
{{- if $item.Summary}}
   {{mdsafe $item.Summary}}
{{- end}}
{{- end}}
{{- if .MoreSummary }}

{{mdsafe .MoreSummary}}
{{- end }}
{{- end }}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (34, 'CMD_MEMBER_NEWS_NO_MEMBERS', NULL, '📰 뉴스 대상 멤버가 없습니다.
예) `{{.Prefix}}알람 추가 페코라`', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (39, 'CMD_MEMBER_NEWS_STATUS', NULL, '{{if .IsSubscribed}}🔔{{else}}🔕{{end}} 뉴스 알림: **{{if .IsSubscribed}}켜짐{{else}}꺼짐{{end}}**
{{- if .IsSubscribed}}
- 발송: 매주 월요일 09:00 KST
- 해제: `{{.Prefix}}뉴스알림 끄기`
{{- else}}
- 설정: `{{.Prefix}}뉴스알림 켜기`
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (52, 'ALARM_DISPATCH_NOTIFICATION_GROUP', NULL, '## {{if .IsStarting}}🔴 {{if .AllPremiere}}선행공개{{else}}방송{{end}} 시작{{else}}⏰ {{if .AllPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- range .Entries}}

{{if .IsStarting}}🔴 **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- $linkable := and .Title $safeURL}}
{{- if $linkable}}
[{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
{{printf "\u200b"}}{{mdsafe .Title}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{mdsafe .ScheduleMessage}}
{{- end}}
{{- if and .URL (not $linkable)}}
{{if or $safeURL $composite}}{{.URL}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (21, 'OUTBOX_VIDEO_GROUP', NULL, '## {{if eq .Kind "LIVE_STREAM"}}🔴 {{mdsafe .MemberName}} 방송 시작 ({{.Count}}){{else if eq .Kind "NEW_VIDEO"}}🔔 {{mdsafe .MemberName}} 새 영상 ({{.Count}}){{else}}🔔 {{mdsafe .MemberName}} 알림 ({{.Count}}){{end}}
{{- $n := 0}}
{{- range $item := .Items}}
{{- if and $item.Title $item.URL}}
{{- $n = add $n 1}}
{{$n}}. [{{mdsafe (truncate 40 $item.Title)}}]({{$item.URL}})
{{- else if $item.Title}}
{{- $n = add $n 1}}
{{$n}}. {{mdsafe (truncate 40 $item.Title)}}
{{- else if $item.URL}}
{{- $n = add $n 1}}
{{$n}}. {{$item.URL}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (22, 'OUTBOX_SHORTS_GROUP', NULL, '## 🔔 {{mdsafe .MemberName}} 새 쇼츠 ({{.Count}})
{{- $n := 0}}
{{- range $item := .Items}}
{{- if and $item.Title $item.URL}}
{{- $n = add $n 1}}
{{$n}}. [{{mdsafe (truncate 40 $item.Title)}}]({{$item.URL}})
{{- else if $item.Title}}
{{- $n = add $n 1}}
{{$n}}. {{mdsafe (truncate 40 $item.Title)}}
{{- else if $item.URL}}
{{- $n = add $n 1}}
{{$n}}. {{$item.URL}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (23, 'OUTBOX_COMMUNITY_GROUP', NULL, '## 🔔 {{mdsafe .MemberName}} 커뮤니티 글 ({{.Count}})
{{- $n := 0}}
{{- range $item := .Items}}
{{- if $item.ContentText}}
{{- $n = add $n 1}}
{{$n}}. {{mdsafe (truncate 40 $item.ContentText)}}
{{- if $item.URL}}
   [커뮤니티 글 보기]({{$item.URL}})
{{- end}}
{{- else if $item.URL}}
{{- $n = add $n 1}}
{{$n}}. [커뮤니티 글 보기]({{$item.URL}})
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (11, 'CMD_LIVE_STREAMS', NULL, '{{- if eq .Count 0 -}}
🔴 방송 중인 스트림이 없습니다.
{{- else -}}
## 🔴 라이브 ({{.Count}})
{{range .Streams}}
- **{{mdsafe .ChannelName}}**{{if gt .ViewerCount 0}} ({{formatNumberKR .ViewerCount}}명){{end}}
{{- if and .Title .URL}}
  [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
  {{mdsafe .Title}}
{{- else if .URL}}
  {{.URL}}
{{- end}}
{{- end -}}
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (12, 'CMD_UPCOMING_STREAMS', NULL, '{{- if eq .Count 0 -}}
📅 {{.Hours}}시간 이내 예정된 방송이 없습니다.
{{- else -}}
## 📅 예정 방송 ({{.Hours}}시간 이내, {{.Count}})
{{range .Streams}}
- **{{mdsafe .ChannelName}}**
  ⏰ {{.TimeInfo}}
{{- if and .Title .URL}}
  [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
  {{mdsafe .Title}}
{{- else if .URL}}
  {{.URL}}
{{- end}}
{{- end -}}
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (42, 'CMD_STATS_GAINERS', NULL, '## 📊 구독자 증가 순위{{if .Period}} ({{.Period}}){{end}}
{{range .Gainers}}
{{.Rank}}. **{{mdsafe .MemberName}}** +{{.Delta}}명{{if .Current}} (현재 {{.Current}}명){{end}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (43, 'CMD_CALENDAR', NULL, '{{- if eq .Count 0 -}}
📅 {{.Year}}년 {{.Month}}월 등록된 기념일이 없습니다.
{{- else -}}
## 📅 {{.Year}}년 {{.Month}}월 기념일 ({{.Count}})
{{- range .Days}}

**{{printf "%02d/%02d" .Month .Day}}**
{{- range .Entries}}
{{- if .IsBirthday}}
- 🎂 {{mdsafe .Name}} 생일
{{- else}}
- 🎉 {{mdsafe .Name}} 데뷔 {{.Years}}주년
{{- end}}
{{- end}}
{{- end}}
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (44, 'CMD_MEMBER_NOT_LIVE', NULL, '{{mdsafe .MemberName}}은(는) 현재 방송 중이 아닙니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (45, 'CMD_MEMBER_NO_UPCOMING', NULL, '{{mdsafe .MemberName}}은(는) {{.Hours}}시간 이내 예정된 방송이 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (46, 'CMD_MEMBER_NOT_FOUND', NULL, '❌ ''{{mdsafe .MemberName}}'' 멤버를 찾을 수 없습니다.', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (50, 'CMD_AMBIGUOUS_MEMBER', NULL, '동일한 이름의 멤버가 여러 명 있습니다.
{{range .Candidates}}{{.Index}}. {{mdsafe .Name}}
{{end}}
예) `{{.Prefix}}{{.CommandExample}}` {{mdsafe .FirstName}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (24, 'CMD_MAJOR_EVENT_WEEKLY_SUMMARY', NULL, '## 📅 이번 주 행사 ({{.Count}})
{{- if .LLMSummary}}

{{.LLMSummary}}
{{- end}}
{{range $index, $event := .Events}}
{{- if and $event.Title $event.Link}}
{{add $index 1}}. [{{mdsafe $event.Title}}]({{$event.Link}})
{{- else if $event.Title}}
{{add $index 1}}. {{mdsafe $event.Title}}
{{- else}}
{{add $index 1}}. {{$event.Link}}
{{- end}}
{{- if $event.DateStr}}
   ⏰ {{$event.DateStr}}
{{- end}}
{{- if $event.Members}}
   {{mdsafe $event.Members}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (51, 'ALARM_DISPATCH_NOTIFICATION', NULL, '## {{if .IsStarting}}🔴 **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} 시작{{else if .IsScheduled}}⏰ **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} 예정{{else}}⏰ **{{mdsafe .MemberName}}** {{if .IsPremiere}}선행공개{{else}}방송{{end}} {{.MinutesUntil}}분 전{{end}}
{{- $url := .URL}}
{{- $parts := split $url " | "}}
{{- $shortLinkURL := hasPrefix $url "https://short.holoshi.com/l/"}}
{{- $youtubeURL := or $shortLinkURL (hasPrefix $url "https://www.youtube.com/watch?") (hasPrefix $url "https://youtube.com/watch?") (hasPrefix $url "https://m.youtube.com/watch?") (hasPrefix $url "https://www.youtube.com/live/") (hasPrefix $url "https://youtube.com/live/") (hasPrefix $url "https://youtu.be/")}}
{{- $trustedURL := or $youtubeURL (hasPrefix $url "https://www.twitch.tv/") (hasPrefix $url "https://twitch.tv/") (hasPrefix $url "https://chzzk.naver.com/live/")}}
{{- $delimiterSafe := and (not (contains $url "\t")) (not (contains $url "\n")) (not (contains $url "\r")) (not (contains $url "(")) (not (contains $url ")")) (not (contains $url "[")) (not (contains $url "]")) (not (contains $url "<")) (not (contains $url ">")) (not (contains $url "\\"))}}
{{- $safeURL := and $url $trustedURL $delimiterSafe (not (contains $url " ")) (not (contains $url "|"))}}
{{- $composite := and (eq (len $parts) 2) $youtubeURL (hasPrefix (index $parts 1) "https://chzzk.naver.com/live/") $delimiterSafe (not (contains (index $parts 0) " ")) (not (contains (index $parts 1) " "))}}
{{- $linkable := and .Title $safeURL}}
{{- if $linkable}}
[{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
{{printf "\u200b"}}{{mdsafe .Title}}
{{- end}}
{{- if .ScheduleMessage}}
{{printf "\u200b"}}{{mdsafe .ScheduleMessage}}
{{- end}}
{{- if and .URL (not $linkable)}}
{{if or $safeURL $composite}}{{.URL}}{{else}}{{printf "\u200b"}}{{mdsafe (replace (replace .URL "\n" " ") "\r" " ")}}{{end}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (9, 'CMD_ALARM_LIST', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
예) `{{.Prefix}}알람 추가 페코라`
{{- else -}}
## 🔔 알람 ({{.Count}})
{{range $index, $alarm := .Alarms}}
{{add $index 1}}. **{{mdsafe $alarm.MemberName}}**{{if $alarm.TypesLabel}} ({{mdsafe $alarm.TypesLabel}}){{end}}
{{- if $alarm.NextStream}}
{{- if eq $alarm.NextStream.Status "live"}}
   🔴 방송 중
{{- if and $alarm.NextStream.Title $alarm.NextStream.URL}}
   [{{mdsafe $alarm.NextStream.Title}}]({{$alarm.NextStream.URL}})
{{- else if $alarm.NextStream.Title}}
   {{mdsafe $alarm.NextStream.Title}}
{{- else if $alarm.NextStream.URL}}
   {{$alarm.NextStream.URL}}
{{- end}}
{{- else if eq $alarm.NextStream.Status "upcoming"}}
   ⏰ {{if $alarm.NextStream.StartingSoon}}곧 시작{{else}}{{$alarm.NextStream.ScheduledKST}}{{if $alarm.NextStream.TimeDetail}} ({{$alarm.NextStream.TimeDetail}}){{end}}{{end}}
{{- if and $alarm.NextStream.Title $alarm.NextStream.URL}}
   [{{mdsafe $alarm.NextStream.Title}}]({{$alarm.NextStream.URL}})
{{- else if $alarm.NextStream.Title}}
   {{mdsafe $alarm.NextStream.Title}}
{{- else if $alarm.NextStream.URL}}
   {{$alarm.NextStream.URL}}
{{- end}}
{{- end}}
{{- end}}
{{- end}}
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (10, 'CMD_ALARM_NOTIFICATION', NULL, '⏰ **{{mdsafe .ChannelName}}** 방송 예정
{{- if .ScheduledTimeKST}}
- {{.ScheduledTimeKST}} 시작
{{- else}}
- 곧 시작
{{- end}}
{{- if .ScheduleMessage}}
- {{mdsafe .ScheduleMessage}}
{{- end}}
{{- if .Title}}
- {{mdsafe .Title}}
{{- end}}
{{- if .URL}}

{{.URL}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (32, 'CMD_ALARM_LIVE_STARTED', NULL, '🔴 **{{mdsafe .ChannelName}}** 방송 시작
{{- if .ScheduledTimeKST}}
- {{.ScheduledTimeKST}} 시작
{{- end}}
{{- if .Title}}
- {{mdsafe .Title}}
{{- end}}
{{- if .URL}}

{{.URL}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (47, 'CMD_ALARM_NOTIFICATION_GROUP', NULL, '## 🔔 방송 알림 ({{.Count}})
{{if le .MinutesUntil 0}}방송이 시작되었습니다.{{else if eq (len .ScheduledTimes) 0}}곧 시작합니다.{{else if eq (len .ScheduledTimes) 1}}⏰ {{index .ScheduledTimes 0}}{{else}}⏰ {{join .ScheduledTimes ", "}}{{end}}
{{- range .Entries}}
{{.Index}}. **{{mdsafe (default "알 수 없는 채널" .ChannelName)}}**{{if .ScheduledKST}} ({{.ScheduledKST}}){{end}}
{{- if and .Title .URL}}
   [{{mdsafe .Title}}]({{.URL}})
{{- else if .Title}}
   {{mdsafe .Title}}
{{- else if .URL}}
   {{.URL}}
{{- end}}
{{- end}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (18, 'CMD_ALARM_CLEARED', NULL, '{{- if eq .Count 0 -}}
🔔 설정된 알람이 없습니다.
{{- else -}}
✅ 알람 **{{.Count}}개**를 모두 해제했습니다.
{{- end -}}', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (35, 'CMD_MEMBER_NEWS_SUBSCRIBED', NULL, '✅ 뉴스 알림을 켰습니다.
- 발송: **매주 월요일 09:00 KST**', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');
INSERT INTO public.notification_templates (id, template_key, channel_id, body, created_at, updated_at) VALUES (25, 'CMD_MAJOR_EVENT_SUBSCRIBED', NULL, '✅ 행사 알림을 켰습니다.
- 발송: **매주 행사 요약**', '2000-01-01 00:00:00+00', '2000-01-01 00:00:00+00');


--
-- Data for Name: youtube_channel_latest_stats; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_channel_profiles; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_channel_stats_snapshots; Type: TABLE DATA; Schema: public; Owner: -
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
-- Data for Name: youtube_content_alarm_tracking; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_content_watermarks; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_sessions; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_live_viewer_samples; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_milestone_approaching; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_milestones; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_notification_delivery; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_notification_delivery_telemetry; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_notification_outbox; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_stats_changes; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_stats_history; Type: TABLE DATA; Schema: public; Owner: -
--



--
-- Data for Name: youtube_stream_stats; Type: TABLE DATA; Schema: public; Owner: -
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

SELECT pg_catalog.setval('public.members_id_seq', 18, true);


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

SELECT pg_catalog.setval('public.notification_templates_id_seq', 159, true);


--
-- Name: youtube_milestone_approaching_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.youtube_milestone_approaching_id_seq', 1, false);


--
-- Name: youtube_milestones_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.youtube_milestones_id_seq', 1, false);


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
-- Name: youtube_stats_changes_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.youtube_stats_changes_id_seq', 1, false);


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
-- Name: alarms alarms_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarms
    ADD CONSTRAINT alarms_pkey PRIMARY KEY (id);


--
-- Name: alarms alarms_room_channel_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarms
    ADD CONSTRAINT alarms_room_channel_unique UNIQUE (room_id, channel_id);


--
-- Name: auth_password_reset_tokens auth_password_reset_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_password_reset_tokens
    ADD CONSTRAINT auth_password_reset_tokens_pkey PRIMARY KEY (token_hash);


--
-- Name: auth_users auth_users_email_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_users
    ADD CONSTRAINT auth_users_email_key UNIQUE (email);


--
-- Name: auth_users auth_users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_users
    ADD CONSTRAINT auth_users_pkey PRIMARY KEY (id);


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
-- Name: message_strings ux_message_strings; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.message_strings
    ADD CONSTRAINT ux_message_strings UNIQUE (namespace, key);


--
-- Name: youtube_channel_latest_stats youtube_channel_latest_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_latest_stats
    ADD CONSTRAINT youtube_channel_latest_stats_pkey PRIMARY KEY (channel_id);


--
-- Name: youtube_channel_profiles youtube_channel_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_profiles
    ADD CONSTRAINT youtube_channel_profiles_pkey PRIMARY KEY (channel_id);


--
-- Name: youtube_channel_stats_snapshots youtube_channel_stats_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_channel_stats_snapshots
    ADD CONSTRAINT youtube_channel_stats_snapshots_pkey PRIMARY KEY (channel_id, captured_at);


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
-- Name: youtube_content_alarm_tracking youtube_content_alarm_tracking_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_alarm_tracking
    ADD CONSTRAINT youtube_content_alarm_tracking_pkey PRIMARY KEY (kind, canonical_content_id);


--
-- Name: youtube_content_watermarks youtube_content_watermarks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_content_watermarks
    ADD CONSTRAINT youtube_content_watermarks_pkey PRIMARY KEY (channel_id, watermark_type);


--
-- Name: youtube_live_sessions youtube_live_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_sessions
    ADD CONSTRAINT youtube_live_sessions_pkey PRIMARY KEY (video_id);


--
-- Name: youtube_live_viewer_samples youtube_live_viewer_samples_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_live_viewer_samples
    ADD CONSTRAINT youtube_live_viewer_samples_pkey PRIMARY KEY (video_id, captured_at);


--
-- Name: youtube_milestone_approaching youtube_milestone_approaching_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_milestone_approaching
    ADD CONSTRAINT youtube_milestone_approaching_pkey PRIMARY KEY (id);


--
-- Name: youtube_milestone_approaching youtube_milestone_approaching_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_milestone_approaching
    ADD CONSTRAINT youtube_milestone_approaching_unique UNIQUE (channel_id, milestone_value);


--
-- Name: youtube_milestones youtube_milestones_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_milestones
    ADD CONSTRAINT youtube_milestones_pkey PRIMARY KEY (id);


--
-- Name: youtube_milestones youtube_milestones_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_milestones
    ADD CONSTRAINT youtube_milestones_unique UNIQUE (channel_id, type, value);


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
-- Name: youtube_stats_changes youtube_stats_changes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_stats_changes
    ADD CONSTRAINT youtube_stats_changes_pkey PRIMARY KEY (id);


--
-- Name: youtube_stats_history youtube_stats_history_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_stats_history
    ADD CONSTRAINT youtube_stats_history_pkey PRIMARY KEY ("time", channel_id);


--
-- Name: youtube_stream_stats youtube_stream_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_stream_stats
    ADD CONSTRAINT youtube_stream_stats_pkey PRIMARY KEY (video_id);


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
-- Name: idx_alarm_dispatch_deliveries_room_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarm_dispatch_deliveries_room_created ON public.alarm_dispatch_deliveries USING btree (room_id, created_at DESC);


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
-- Name: idx_alarms_alarm_types_gin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarms_alarm_types_gin ON public.alarms USING gin (alarm_types);


--
-- Name: idx_alarms_channel_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarms_channel_created ON public.alarms USING btree (channel_id, created_at);


--
-- Name: idx_alarms_channel_member_latest; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarms_channel_member_latest ON public.alarms USING btree (channel_id, created_at DESC) WHERE ((member_name IS NOT NULL) AND (member_name <> ''::text));


--
-- Name: idx_alarms_room_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alarms_room_created ON public.alarms USING btree (room_id, created_at);


--
-- Name: idx_auth_reset_tokens_user_unused; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_reset_tokens_user_unused ON public.auth_password_reset_tokens USING btree (user_id) WHERE (used_at IS NULL);


--
-- Name: idx_bot_command_executions_status_claimed; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_command_executions_status_claimed ON public.bot_command_executions USING btree (claimed_at, id) WHERE (status = 'claimed'::text);


--
-- Name: idx_bot_command_executions_terminal_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_command_executions_terminal_updated ON public.bot_command_executions USING btree (updated_at, id) WHERE (status = ANY (ARRAY['succeeded'::text, 'failed'::text, 'outcome_unknown'::text]));


--
-- Name: idx_bot_reply_outbox_due_available; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_due_available ON public.bot_reply_outbox USING btree (available_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'retryable_pre_dispatch'::text, 'outcome_unknown'::text]));


--
-- Name: idx_bot_reply_outbox_lease_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_bot_reply_outbox_lease_expiry ON public.bot_reply_outbox USING btree (lease_until, id) WHERE (status = ANY (ARRAY['submitting'::text, 'accepted'::text]));


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
-- Name: idx_template_revisions_template_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_template_revisions_template_created ON public.notification_template_revisions USING btree (template_id, created_at DESC);


--
-- Name: idx_ycat_channel_detected; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycat_channel_detected ON public.youtube_content_alarm_tracking USING btree (channel_id, detected_at DESC);


--
-- Name: idx_ycat_delivery_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycat_delivery_status ON public.youtube_content_alarm_tracking USING btree (delivery_status, detected_at DESC);


--
-- Name: idx_ycat_detected_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycat_detected_at ON public.youtube_content_alarm_tracking USING btree (detected_at DESC);


--
-- Name: idx_ycat_kind_content; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycat_kind_content ON public.youtube_content_alarm_tracking USING btree (kind, content_id);


--
-- Name: idx_ycp_channel_first_seen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycp_channel_first_seen ON public.youtube_community_posts USING btree (channel_id, first_seen_at DESC);


--
-- Name: idx_ycsas_authorized_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycsas_authorized_at ON public.youtube_community_shorts_alarm_states USING btree (authorized_at DESC) WHERE (authorized_at IS NOT NULL);


--
-- Name: idx_ycsas_delivery_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycsas_delivery_status ON public.youtube_community_shorts_alarm_states USING btree (delivery_status, detected_at DESC);


--
-- Name: idx_ycsas_detected_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycsas_detected_at ON public.youtube_community_shorts_alarm_states USING btree (detected_at DESC);


--
-- Name: idx_ycsas_kind_content; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_ycsas_kind_content ON public.youtube_community_shorts_alarm_states USING btree (kind, content_id);


--
-- Name: idx_ycss_captured_at_brin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycss_captured_at_brin ON public.youtube_channel_stats_snapshots USING brin (captured_at);


--
-- Name: idx_ycssp_channel_detected; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ycssp_channel_detected ON public.youtube_community_shorts_source_posts USING btree (channel_id, detected_at DESC);


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
-- Name: idx_yls_channel_last_seen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_channel_last_seen ON public.youtube_live_sessions USING btree (channel_id, last_seen_at DESC);


--
-- Name: idx_yls_ended_channel_sort_video; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_ended_channel_sort_video ON public.youtube_live_sessions USING btree (channel_id, COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at) DESC, video_id DESC) WHERE (status = 'ENDED'::text);


--
-- Name: idx_yls_ended_cleanup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_ended_cleanup ON public.youtube_live_sessions USING btree (ended_at, video_id) WHERE ((status = 'ENDED'::text) AND (ended_at IS NOT NULL));


--
-- Name: idx_yls_ended_sort_video; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_ended_sort_video ON public.youtube_live_sessions USING btree (COALESCE(ended_at, started_at, scheduled_start_time, last_seen_at) DESC, video_id DESC) WHERE (status = 'ENDED'::text);


--
-- Name: idx_yls_live_first_seen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_live_first_seen ON public.youtube_live_sessions USING btree (live_first_seen_at, channel_id) WHERE (status = 'LIVE'::text);


--
-- Name: idx_yls_status_last_seen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yls_status_last_seen ON public.youtube_live_sessions USING btree (status, last_seen_at DESC);


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
-- Name: idx_youtube_stats_history_channel_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_youtube_stats_history_channel_time ON public.youtube_stats_history USING btree (channel_id, "time" DESC);


--
-- Name: idx_yv_channel_first_seen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yv_channel_first_seen ON public.youtube_videos USING btree (channel_id, first_seen_at DESC);


--
-- Name: idx_yv_channel_is_short; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_yv_channel_is_short ON public.youtube_videos USING btree (channel_id, is_short);


--
-- Name: ux_notification_templates_channel; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ux_notification_templates_channel ON public.notification_templates USING btree (template_key, channel_id) WHERE (channel_id IS NOT NULL);


--
-- Name: ux_notification_templates_default; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ux_notification_templates_default ON public.notification_templates USING btree (template_key) WHERE (channel_id IS NULL);


--
-- Name: bot_command_executions bot_command_execution_terminal_summary_scrub; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_command_execution_terminal_summary_scrub BEFORE INSERT OR UPDATE OF status, result_summary ON public.bot_command_executions FOR EACH ROW WHEN ((new.status = ANY (ARRAY['succeeded'::text, 'failed'::text, 'outcome_unknown'::text]))) EXECUTE FUNCTION public.scrub_bot_command_execution_terminal_summary();


--
-- Name: bot_reply_outbox_replay_audit bot_reply_outbox_replay_audit_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_reply_outbox_replay_audit_immutable BEFORE DELETE OR UPDATE ON public.bot_reply_outbox_replay_audit FOR EACH ROW EXECUTE FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation();


--
-- Name: bot_reply_outbox bot_reply_outbox_replay_claim_audit; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_reply_outbox_replay_claim_audit BEFORE UPDATE ON public.bot_reply_outbox FOR EACH ROW EXECUTE FUNCTION public.append_bot_reply_outbox_replay_claim_audit();


--
-- Name: bot_webhook_inbox bot_webhook_inbox_terminal_payload_scrub; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER bot_webhook_inbox_terminal_payload_scrub BEFORE INSERT OR UPDATE OF status, payload ON public.bot_webhook_inbox FOR EACH ROW WHEN ((new.status = ANY (ARRAY['dead'::text, 'succeeded'::text]))) EXECUTE FUNCTION public.scrub_bot_webhook_inbox_terminal_payload();


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
-- Name: alarm_dispatch_event_collisions alarm_dispatch_event_collisions_existing_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alarm_dispatch_event_collisions
    ADD CONSTRAINT alarm_dispatch_event_collisions_existing_event_id_fkey FOREIGN KEY (existing_event_id) REFERENCES public.alarm_dispatch_events(id) ON DELETE SET NULL;


--
-- Name: auth_password_reset_tokens auth_password_reset_tokens_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_password_reset_tokens
    ADD CONSTRAINT auth_password_reset_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.auth_users(id) ON DELETE CASCADE;


--
-- Name: bot_reply_outbox_replay_audit bot_reply_outbox_replay_audit_outbox_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_reply_outbox_replay_audit
    ADD CONSTRAINT bot_reply_outbox_replay_audit_outbox_id_fkey FOREIGN KEY (outbox_id) REFERENCES public.bot_reply_outbox(id) ON DELETE CASCADE;


--
-- Name: bot_webhook_heads bot_webhook_heads_message_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bot_webhook_heads
    ADD CONSTRAINT bot_webhook_heads_message_id_fkey FOREIGN KEY (message_id) REFERENCES public.bot_webhook_inbox(message_id) ON DELETE CASCADE;


--
-- Name: notification_template_revisions notification_template_revisions_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_template_revisions
    ADD CONSTRAINT notification_template_revisions_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.notification_templates(id) ON DELETE CASCADE;


--
-- Name: youtube_notification_delivery youtube_notification_delivery_outbox_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.youtube_notification_delivery
    ADD CONSTRAINT youtube_notification_delivery_outbox_id_fkey FOREIGN KEY (outbox_id) REFERENCES public.youtube_notification_outbox(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hololive_scraper') THEN
        GRANT SELECT, INSERT, UPDATE ON TABLE public.major_events TO hololive_scraper;
        GRANT USAGE, SELECT ON SEQUENCE public.major_events_id_seq TO hololive_scraper;
        GRANT SELECT ON TABLE public.major_event_subscriptions TO hololive_scraper;
    END IF;
END
$$;

REVOKE ALL ON TABLE public.bot_reply_outbox_replay_audit FROM PUBLIC;
REVOKE ALL ON SEQUENCE public.bot_reply_outbox_replay_audit_id_seq FROM PUBLIC;
REVOKE ALL ON FUNCTION public.grant_bot_reply_outbox_manual_replay(BIGINT, TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.append_bot_reply_outbox_replay_claim_audit() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation() FROM PUBLIC;

DO $$
DECLARE
    grantee_role TEXT;
BEGIN
    FOR grantee_role IN
        SELECT DISTINCT grantee
        FROM information_schema.role_table_grants
        WHERE table_schema = 'public'
          AND table_name = 'bot_reply_outbox_replay_audit'
          AND grantee <> current_user
    LOOP
        EXECUTE format(
            'REVOKE ALL ON TABLE public.bot_reply_outbox_replay_audit FROM %I',
            grantee_role
        );
    END LOOP;

    FOR grantee_role IN
        SELECT DISTINCT grantee
        FROM information_schema.role_usage_grants
        WHERE object_schema = 'public'
          AND object_name = 'bot_reply_outbox_replay_audit_id_seq'
          AND grantee <> current_user
    LOOP
        EXECUTE format(
            'REVOKE ALL ON SEQUENCE public.bot_reply_outbox_replay_audit_id_seq FROM %I',
            grantee_role
        );
    END LOOP;

    FOR grantee_role IN
        SELECT DISTINCT grantee
        FROM information_schema.routine_privileges
        WHERE specific_schema = 'public'
          AND routine_name IN (
              'grant_bot_reply_outbox_manual_replay',
              'append_bot_reply_outbox_replay_claim_audit',
              'reject_bot_reply_outbox_replay_audit_mutation'
          )
          AND grantee <> current_user
    LOOP
        EXECUTE format(
            'REVOKE ALL ON FUNCTION public.grant_bot_reply_outbox_manual_replay(BIGINT, TEXT, TEXT) FROM %I',
            grantee_role
        );
        EXECUTE format(
            'REVOKE ALL ON FUNCTION public.append_bot_reply_outbox_replay_claim_audit() FROM %I',
            grantee_role
        );
        EXECUTE format(
            'REVOKE ALL ON FUNCTION public.reject_bot_reply_outbox_replay_audit_mutation() FROM %I',
            grantee_role
        );
    END LOOP;
END
$$;

COMMIT;
