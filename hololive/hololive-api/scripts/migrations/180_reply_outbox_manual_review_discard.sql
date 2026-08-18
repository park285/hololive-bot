DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_status_vocab_next'
    ) AND NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_status_vocab'
          AND pg_get_constraintdef(oid) LIKE '%discarded%'
    ) THEN
        ALTER TABLE public.bot_reply_outbox
            ADD CONSTRAINT chk_bot_reply_outbox_status_vocab_next CHECK (
                status IN (
                    'pending',
                    'submitting',
                    'accepted',
                    'handoff_completed',
                    'retryable_pre_dispatch',
                    'outcome_unknown',
                    'dead',
                    'permanent_conflict',
                    'manual_review',
                    'discarded'
                )
            ) NOT VALID;
    END IF;
END
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_status_vocab_next'
          AND NOT convalidated
    ) THEN
        ALTER TABLE public.bot_reply_outbox
            VALIDATE CONSTRAINT chk_bot_reply_outbox_status_vocab_next;
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_state_shape_next'
    ) AND NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_state_shape'
          AND pg_get_constraintdef(oid) LIKE '%discarded%'
    ) THEN
        ALTER TABLE public.bot_reply_outbox
            ADD CONSTRAINT chk_bot_reply_outbox_state_shape_next CHECK (
                (
                    status NOT IN ('submitting', 'accepted')
                    OR (
                        claim_token IS NOT NULL
                        AND lease_until IS NOT NULL
                        AND first_attempt_at IS NOT NULL
                    )
                )
                AND (status <> 'accepted' OR length(iris_request_id) > 0)
                AND (
                    status IN ('handoff_completed', 'dead', 'permanent_conflict', 'discarded')
                    OR payload IS NOT NULL
                )
            ) NOT VALID;
    END IF;
END
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_state_shape_next'
          AND NOT convalidated
    ) THEN
        ALTER TABLE public.bot_reply_outbox
            VALIDATE CONSTRAINT chk_bot_reply_outbox_state_shape_next;
    END IF;
END
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_status_vocab_next'
    ) THEN
        ALTER TABLE public.bot_reply_outbox
            DROP CONSTRAINT IF EXISTS chk_bot_reply_outbox_status_vocab;
        ALTER TABLE public.bot_reply_outbox
            RENAME CONSTRAINT chk_bot_reply_outbox_status_vocab_next
            TO chk_bot_reply_outbox_status_vocab;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'public.bot_reply_outbox'::regclass
          AND conname = 'chk_bot_reply_outbox_state_shape_next'
    ) THEN
        ALTER TABLE public.bot_reply_outbox
            DROP CONSTRAINT IF EXISTS chk_bot_reply_outbox_state_shape;
        ALTER TABLE public.bot_reply_outbox
            RENAME CONSTRAINT chk_bot_reply_outbox_state_shape_next
            TO chk_bot_reply_outbox_state_shape;
    END IF;
END
$$;

BEGIN;

CREATE TABLE IF NOT EXISTS bot_reply_outbox_resolution_audit (
    id BIGSERIAL PRIMARY KEY,
    outbox_id BIGINT NOT NULL UNIQUE REFERENCES bot_reply_outbox(id) ON DELETE CASCADE,
    decision TEXT NOT NULL,
    observed_iris_state TEXT NOT NULL,
    actor TEXT NOT NULL,
    reason TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_bot_reply_outbox_resolution_audit_decision
        CHECK (decision = 'discarded_without_replay'),
    CONSTRAINT chk_bot_reply_outbox_resolution_audit_iris_state
        CHECK (observed_iris_state IN (
            'queued',
            'preparing',
            'prepared',
            'sending',
            'handoff_completed',
            'failed',
            'outcome_unknown',
            'not_found'
        )),
    CONSTRAINT chk_bot_reply_outbox_resolution_audit_actor
        CHECK (actor ~ '^[A-Za-z0-9._:@-]{1,64}$'),
    CONSTRAINT chk_bot_reply_outbox_resolution_audit_reason
        CHECK (
            octet_length(reason) BETWEEN 1 AND 256
            AND reason !~ '[[:cntrl:]]'
    )
);

CREATE OR REPLACE FUNCTION enforce_bot_reply_outbox_discard_audit()
RETURNS TRIGGER
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
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

CREATE OR REPLACE FUNCTION discard_bot_reply_outbox_manual_review(
    requested_outbox_id BIGINT,
    operator_actor TEXT,
    operator_reason TEXT,
    observed_iris_state TEXT
)
RETURNS TEXT
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog
AS $$
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
$$;

CREATE OR REPLACE FUNCTION reject_bot_reply_outbox_resolution_audit_mutation()
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

    RAISE EXCEPTION 'bot_reply_outbox_resolution_audit events are immutable'
        USING ERRCODE = '55000';
END
$$;

DROP TRIGGER IF EXISTS bot_reply_outbox_resolution_audit_immutable
    ON bot_reply_outbox_resolution_audit;

CREATE TRIGGER bot_reply_outbox_resolution_audit_immutable
    BEFORE UPDATE OR DELETE
    ON bot_reply_outbox_resolution_audit
    FOR EACH ROW
    EXECUTE FUNCTION reject_bot_reply_outbox_resolution_audit_mutation();

DROP TRIGGER IF EXISTS bot_reply_outbox_discard_audit_required
    ON bot_reply_outbox;

CREATE TRIGGER bot_reply_outbox_discard_audit_required
    BEFORE INSERT OR UPDATE
    ON bot_reply_outbox
    FOR EACH ROW
    EXECUTE FUNCTION enforce_bot_reply_outbox_discard_audit();

REVOKE ALL ON TABLE bot_reply_outbox_resolution_audit FROM PUBLIC;
REVOKE ALL ON SEQUENCE bot_reply_outbox_resolution_audit_id_seq FROM PUBLIC;
REVOKE ALL ON FUNCTION enforce_bot_reply_outbox_discard_audit() FROM PUBLIC;
REVOKE ALL ON FUNCTION discard_bot_reply_outbox_manual_review(BIGINT, TEXT, TEXT, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION reject_bot_reply_outbox_resolution_audit_mutation() FROM PUBLIC;

DO $$
DECLARE
    grantee_role TEXT;
BEGIN
    FOR grantee_role IN
        SELECT DISTINCT grantee
        FROM information_schema.role_table_grants
        WHERE table_schema = 'public'
          AND table_name = 'bot_reply_outbox_resolution_audit'
          AND grantee <> current_user
    LOOP
        EXECUTE format(
            'REVOKE ALL ON TABLE public.bot_reply_outbox_resolution_audit FROM %I',
            grantee_role
        );
    END LOOP;

    FOR grantee_role IN
        SELECT DISTINCT grantee
        FROM information_schema.role_usage_grants
        WHERE object_schema = 'public'
          AND object_name = 'bot_reply_outbox_resolution_audit_id_seq'
          AND grantee <> current_user
    LOOP
        EXECUTE format(
            'REVOKE ALL ON SEQUENCE public.bot_reply_outbox_resolution_audit_id_seq FROM %I',
            grantee_role
        );
    END LOOP;

    FOR grantee_role IN
        SELECT DISTINCT grantee
        FROM information_schema.routine_privileges
        WHERE specific_schema = 'public'
          AND routine_name IN (
              'enforce_bot_reply_outbox_discard_audit',
              'discard_bot_reply_outbox_manual_review',
              'reject_bot_reply_outbox_resolution_audit_mutation'
          )
          AND grantee <> current_user
    LOOP
        EXECUTE format(
            'REVOKE ALL ON FUNCTION public.enforce_bot_reply_outbox_discard_audit() FROM %I',
            grantee_role
        );
        EXECUTE format(
            'REVOKE ALL ON FUNCTION public.discard_bot_reply_outbox_manual_review(BIGINT, TEXT, TEXT, TEXT) FROM %I',
            grantee_role
        );
        EXECUTE format(
            'REVOKE ALL ON FUNCTION public.reject_bot_reply_outbox_resolution_audit_mutation() FROM %I',
            grantee_role
        );
    END LOOP;
END
$$;

COMMIT;
