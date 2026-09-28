-- 227의 과거 적용 기록만으로는 현재 backfill state를 증명할 수 없다.
-- 기존 singleton 원본·복구 SQL을 따로 보존한 뒤, 잠금 아래 완료 상태를 다시 확인하고 폐기한다.
-- DROP과 229 적용 receipt는 같은 transaction으로 묶고, 기록 없는 사전 부재는 성공으로 추정하지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';
DO $migration$
DECLARE
    ledger regclass;
    has_227 boolean;
    has_229 boolean;
    state_rows bigint;
    valid_rows bigint;
BEGIN
    ledger := COALESCE(
        to_regclass('public.schema_migrations'),
        to_regclass('hololive_dbtest_internal.schema_migrations')
    );
    IF ledger IS NULL THEN
        RAISE EXCEPTION 'youtube ledger state drop requires a migration receipt ledger';
    END IF;
    EXECUTE format('LOCK TABLE %s IN SHARE MODE', ledger);
    EXECUTE format('SELECT EXISTS (SELECT 1 FROM %s WHERE filename = $1)', ledger)
        INTO has_227 USING '227_youtube_delivery_ledger_backfill_closed.sql';
    EXECUTE format('SELECT EXISTS (SELECT 1 FROM %s WHERE filename = $1)', ledger)
        INTO has_229 USING '229_drop_youtube_delivery_ledger_backfill_state.sql';
    IF NOT has_227 THEN
        RAISE EXCEPTION 'youtube ledger state drop requires applied 227 receipt';
    END IF;
    IF to_regclass('public.youtube_notification_delivery_ledger_state') IS NULL THEN
        IF NOT has_229 THEN
            RAISE EXCEPTION 'youtube ledger state is absent without 229 receipt; inspect outcome before recovery';
        END IF;
        RETURN;
    END IF;
    IF has_229 THEN
        RAISE EXCEPTION '229 receipt exists while youtube ledger state still exists';
    END IF;

    LOCK TABLE public.youtube_notification_delivery_ledger_state IN ACCESS EXCLUSIVE MODE;
    SELECT count(singleton), count(singleton) FILTER (WHERE
        singleton IS TRUE AND schema_version = 1 AND completed_at IS NOT NULL
        AND delivery_high_water_id >= 0 AND outbox_high_water_id >= 0
        AND delivery_cursor_id = delivery_high_water_id
        AND delivery_verify_cursor_id = delivery_high_water_id
        AND outbox_cursor_id = outbox_high_water_id
        AND legacy_coverage_start_at IS NOT NULL AND coverage_verified_at IS NOT NULL
        AND started_at <= updated_at
        AND legacy_coverage_start_at <= coverage_verified_at
        AND coverage_verified_at <= completed_at AND completed_at <= updated_at
    ) INTO state_rows, valid_rows
    FROM public.youtube_notification_delivery_ledger_state;
    IF EXISTS (
        SELECT 1 FROM public.youtube_notification_delivery_ledger_state WHERE singleton IS NULL
    ) THEN
        RAISE EXCEPTION 'youtube ledger state is incomplete or malformed; no table dropped';
    END IF;
    IF state_rows = 1 AND valid_rows = 1 THEN
        RETURN;
    END IF;
    IF state_rows <> 0 THEN
        RAISE EXCEPTION 'youtube ledger state is incomplete or malformed; no table dropped';
    END IF;

    -- 빈 DB bootstrap만 state 미생성을 허용한다. 세 테이블을 잠가 검사와 DROP 사이의 신규 쓰기를 막는다.
    LOCK TABLE public.youtube_notification_outbox, public.youtube_notification_delivery,
        public.youtube_notification_delivery_ledger IN SHARE MODE;
    IF EXISTS (SELECT 1 FROM public.youtube_notification_outbox)
       OR EXISTS (SELECT 1 FROM public.youtube_notification_delivery)
       OR EXISTS (SELECT 1 FROM public.youtube_notification_delivery_ledger) THEN
        RAISE EXCEPTION 'youtube ledger state has no singleton while delivery data exists';
    END IF;
END
$migration$;
DROP TABLE IF EXISTS public.youtube_notification_delivery_ledger_state;
-- 프로덕션 runner도 같은 (filename) conflict arbiter로 Record하므로, DROP과 적용 receipt를
-- 하나의 commit에 묶는다. dbtest의 checksum 통합 ledger는 자체 runner가 파일 적용 뒤 기록한다.
DO $receipt$
BEGIN
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        INSERT INTO public.schema_migrations (filename)
        VALUES ('229_drop_youtube_delivery_ledger_backfill_state.sql')
        ON CONFLICT (filename) DO NOTHING;
    END IF;
END
$receipt$;
COMMIT;
