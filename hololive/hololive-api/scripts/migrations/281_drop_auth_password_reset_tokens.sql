-- 관리자 비밀번호 재설정 토큰 테이블을 지운다. 재설정 route는 2026-10-02 #559부터 토큰을 발급·소비하지 않고 503만
-- 반환했고, 이 migration과 같은 변경에서 route·서비스·SQL을 지웠다. 이전 이미지도 이 테이블을 503 경로에서 읽지
-- 않으므로 rollback 이미지의 동작은 바뀌지 않는다. 2026-10-11 운영 조회: 0행.
-- 행이 있으면 #559 이후 누군가 토큰을 썼다는 뜻이므로 지우지 않고 실패한다. CASCADE 없이 지워 예상 밖 의존 객체가
-- 있어도 실패한다. auth_users를 가리키는 FK와 idx_auth_reset_tokens_user_unused는 테이블과 함께 지워진다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DO $migration$
BEGIN
    IF pg_catalog.to_regclass('public.auth_password_reset_tokens') IS NULL THEN
        RETURN;
    END IF;
    -- 검사와 DROP이 같은 잠금을 쓰게 처음부터 ACCESS EXCLUSIVE를 잡는다.
    LOCK TABLE public.auth_password_reset_tokens IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM public.auth_password_reset_tokens) THEN
        RAISE EXCEPTION 'retired table public.auth_password_reset_tokens has rows; table kept';
    END IF;
    DROP TABLE IF EXISTS public.auth_password_reset_tokens;
END
$migration$;
COMMIT;
