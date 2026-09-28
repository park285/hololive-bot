-- 지원 중인 API/rollback 이미지가 모두 terminal writer에서 payload를 직접 비운다.
-- 호환 scrub trigger만 폐기하고, terminal payload CHECK는 검증된 상태로 유지한다.
-- 적용 기록 없는 사전 부재·예상과 다른 catalog 형태는 성공으로 추정하지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
SET LOCAL search_path = public, pg_catalog;

DO $migration$
DECLARE
    migration_ledger regclass;
    has_230 boolean;
    inbox_oid oid;
    scrub_function_oid oid;
    scrub_trigger_oid oid;
    expected_trigger constant text := $trigger_def$CREATE TRIGGER bot_webhook_inbox_terminal_payload_scrub BEFORE INSERT OR UPDATE OF status, payload ON bot_webhook_inbox FOR EACH ROW WHEN (new.status = ANY (ARRAY['dead'::text, 'succeeded'::text])) EXECUTE FUNCTION scrub_bot_webhook_inbox_terminal_payload()$trigger_def$;
    expected_check constant text := $check_def$CHECK (((status <> ALL (ARRAY['dead'::text, 'succeeded'::text])) OR (payload = '{}'::jsonb)))$check_def$;
    expected_function_body constant text := $function_body$
BEGIN
    IF NEW.payload IS DISTINCT FROM '{}'::jsonb THEN
        RAISE WARNING 'bot_webhook_inbox terminal payload was scrubbed by the compatibility trigger; a writer that does not clear payload is running';
    END IF;
    NEW.payload := '{}'::jsonb;
    RETURN NEW;
END
$function_body$;
BEGIN
    migration_ledger := COALESCE(
        to_regclass('public.schema_migrations'),
        to_regclass('hololive_dbtest_internal.schema_migrations')
    );
    IF migration_ledger IS NULL THEN
        RAISE EXCEPTION 'bot inbox scrub removal requires a migration receipt ledger';
    END IF;
    EXECUTE format('LOCK TABLE %s IN SHARE MODE', migration_ledger);
    EXECUTE format('SELECT EXISTS (SELECT 1 FROM %s WHERE filename = $1)', migration_ledger)
        INTO has_230 USING '230_drop_bot_webhook_inbox_legacy_scrub_trigger.sql';

    LOCK TABLE public.bot_webhook_inbox IN ACCESS EXCLUSIVE MODE;
    SELECT c.oid INTO inbox_oid
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public' AND c.relname = 'bot_webhook_inbox' AND c.relkind = 'r';
    IF inbox_oid IS NULL THEN
        RAISE EXCEPTION 'bot inbox scrub removal requires the expected inbox table';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint con
        WHERE con.conrelid = inbox_oid
          AND con.conname = 'chk_bot_webhook_inbox_terminal_payload_scrubbed'
          AND con.contype = 'c' AND con.convalidated
          AND pg_get_constraintdef(con.oid) = expected_check
    ) THEN
        RAISE EXCEPTION 'bot inbox terminal payload CHECK is absent, unvalidated, or changed';
    END IF;

    scrub_function_oid := to_regprocedure('public.scrub_bot_webhook_inbox_terminal_payload()');
    SELECT t.oid INTO scrub_trigger_oid
    FROM pg_trigger t
    WHERE t.tgrelid = inbox_oid
      AND t.tgname = 'bot_webhook_inbox_terminal_payload_scrub';

    IF has_230 THEN
        IF scrub_trigger_oid IS NOT NULL OR scrub_function_oid IS NOT NULL THEN
            RAISE EXCEPTION '230 receipt exists but bot inbox scrub objects remain';
        END IF;
        RETURN;
    END IF;

    IF scrub_function_oid IS NULL OR scrub_trigger_oid IS NULL OR NOT EXISTS (
        SELECT 1 FROM pg_trigger t
        JOIN pg_proc p ON p.oid = t.tgfoid
        JOIN pg_language l ON l.oid = p.prolang
        WHERE t.oid = scrub_trigger_oid AND t.tgrelid = inbox_oid
          AND NOT t.tgisinternal AND t.tgenabled = 'O'
          AND t.tgfoid = scrub_function_oid
          AND pg_get_triggerdef(t.oid, true) = expected_trigger
          AND p.pronargs = 0 AND p.prorettype = 'pg_catalog.trigger'::regtype
          AND p.prokind = 'f' AND p.provolatile = 'v' AND NOT p.prosecdef
          AND l.lanname = 'plpgsql' AND p.prosrc = expected_function_body
    ) THEN
        RAISE EXCEPTION 'bot inbox scrub trigger/function is absent or changed without 230 receipt';
    END IF;
END
$migration$;

DROP TRIGGER IF EXISTS bot_webhook_inbox_terminal_payload_scrub ON public.bot_webhook_inbox;
DROP FUNCTION IF EXISTS public.scrub_bot_webhook_inbox_terminal_payload() RESTRICT;

-- 프로덕션 runner의 filename 충돌 기준과 같게 DROP 및 적용 receipt를 한 commit에 묶는다.
-- 별도 checksum 기록이 끊기면 원인을 조사하고, 임의로 재실행하거나 checksum을 만들지 않는다.
DO $receipt$
BEGIN
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        INSERT INTO public.schema_migrations (filename)
        VALUES ('230_drop_bot_webhook_inbox_legacy_scrub_trigger.sql')
        ON CONFLICT (filename) DO NOTHING;
    END IF;
END
$receipt$;
COMMIT;
