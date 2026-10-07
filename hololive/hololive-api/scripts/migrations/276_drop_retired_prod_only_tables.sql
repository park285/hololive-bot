-- 운영에만 남은 빈 테이블 streams와 alarm_dispatch_outbox를 지운다. 두 테이블은 어떤 migration·코드도 만들거나
-- 읽지 않는다. epoch-1 114는 alarm_dispatch_outbox를 라이브 전용 고아로 기록하고 처분을 미뤘다.
-- 2026-10-08 서울 운영 조회: 두 테이블 모두 0행, 시퀀스 미사용, 다른 객체의 의존 없음.
-- 행이 있으면 누군가 쓰기 시작했다는 뜻이므로 지우지 않고 실패한다. CASCADE 없이 지워 예상 밖 의존 객체가 있어도
-- 실패한다. 각 테이블이 소유한 id 시퀀스는 테이블과 함께 지워진다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
BEGIN
    IF pg_catalog.to_regclass('public.streams') IS NULL THEN
        RETURN;
    END IF;
    -- 검사와 DROP이 같은 잠금을 쓰게 처음부터 ACCESS EXCLUSIVE를 잡는다.
    LOCK TABLE public.streams IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM public.streams) THEN
        RAISE EXCEPTION 'retired table public.streams has rows; table kept';
    END IF;
    DROP TABLE IF EXISTS public.streams;
END
$migration$;
DO $migration$
BEGIN
    IF pg_catalog.to_regclass('public.alarm_dispatch_outbox') IS NULL THEN
        RETURN;
    END IF;
    LOCK TABLE public.alarm_dispatch_outbox IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM public.alarm_dispatch_outbox) THEN
        RAISE EXCEPTION 'retired table public.alarm_dispatch_outbox has rows; table kept';
    END IF;
    DROP TABLE IF EXISTS public.alarm_dispatch_outbox;
END
$migration$;
COMMIT;
