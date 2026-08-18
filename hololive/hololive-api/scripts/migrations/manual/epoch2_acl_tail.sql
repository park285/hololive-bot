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
