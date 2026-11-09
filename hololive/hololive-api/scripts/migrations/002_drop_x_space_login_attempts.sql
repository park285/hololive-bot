-- epoch-2에서만 존재하던 자동 로그인 시도 기록을 폐기합니다.
BEGIN;
DROP TABLE IF EXISTS public.x_space_login_attempts;
COMMIT;
