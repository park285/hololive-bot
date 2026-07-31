BEGIN;

CREATE TABLE IF NOT EXISTS bot_reply_outbox_replay_audit (
    id BIGSERIAL PRIMARY KEY,
    outbox_id BIGINT NOT NULL REFERENCES bot_reply_outbox(id) ON DELETE CASCADE,
    grant_number INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    actor TEXT NOT NULL,
    reason TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_bot_reply_outbox_replay_audit_grant_number
        CHECK (grant_number > 0),
    CONSTRAINT chk_bot_reply_outbox_replay_audit_event_type
        CHECK (event_type IN ('granted', 'replayed')),
    CONSTRAINT chk_bot_reply_outbox_replay_audit_actor
        CHECK (actor ~ '^[A-Za-z0-9._:@-]{1,64}$'),
    CONSTRAINT chk_bot_reply_outbox_replay_audit_reason
        CHECK (
            octet_length(reason) BETWEEN 1 AND 256
            AND reason !~ '[[:cntrl:]]'
        ),
    UNIQUE (outbox_id, grant_number, event_type)
);

CREATE INDEX IF NOT EXISTS idx_bot_reply_outbox_replay_audit_outbox_recorded
    ON bot_reply_outbox_replay_audit (outbox_id, recorded_at ASC, id ASC);

CREATE OR REPLACE FUNCTION grant_bot_reply_outbox_manual_replay(
    requested_outbox_id BIGINT,
    operator_actor TEXT,
    operator_reason TEXT
)
RETURNS TEXT
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
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
$$;

CREATE OR REPLACE FUNCTION append_bot_reply_outbox_replay_claim_audit()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
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

CREATE OR REPLACE FUNCTION reject_bot_reply_outbox_replay_audit_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
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

DROP TRIGGER IF EXISTS bot_reply_outbox_replay_audit_immutable
    ON bot_reply_outbox_replay_audit;

DROP FUNCTION IF EXISTS reject_bot_reply_outbox_replay_audit_update();

CREATE TRIGGER bot_reply_outbox_replay_audit_immutable
    BEFORE UPDATE OR DELETE
    ON bot_reply_outbox_replay_audit
    FOR EACH ROW
    EXECUTE FUNCTION reject_bot_reply_outbox_replay_audit_mutation();

DROP TRIGGER IF EXISTS bot_reply_outbox_replay_claim_audit
    ON bot_reply_outbox;

CREATE TRIGGER bot_reply_outbox_replay_claim_audit
    BEFORE UPDATE
    ON bot_reply_outbox
    FOR EACH ROW
    EXECUTE FUNCTION append_bot_reply_outbox_replay_claim_audit();

REVOKE ALL ON TABLE bot_reply_outbox_replay_audit FROM PUBLIC;
REVOKE ALL ON SEQUENCE bot_reply_outbox_replay_audit_id_seq FROM PUBLIC;
REVOKE ALL ON FUNCTION grant_bot_reply_outbox_manual_replay(BIGINT, TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION append_bot_reply_outbox_replay_claim_audit() FROM PUBLIC;
REVOKE ALL ON FUNCTION reject_bot_reply_outbox_replay_audit_mutation() FROM PUBLIC;

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
