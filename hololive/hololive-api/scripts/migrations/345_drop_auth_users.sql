-- 독립 대시보드 로그인 계정 테이블을 지운다. 이 migration과 같은 변경에서 /api/auth/*·/oauth/callback route와
-- auth service(Valkey 세션·bcrypt·로그인 rate limit)를 지웠다. Iris Console은 자기 세션으로 사용자를 인증하고
-- admin plane에는 /api/holo/*를 API key로만 호출하므로 이 테이블을 읽는 현행 호출자가 없다.
-- 행이 있어도 지운다. DROP 전 행 보존은 운영 백업 정책이 소유한다. CASCADE 없이 지워 예상 밖 의존 객체가 있으면
-- 실패한다. 이전 이미지는 /api/auth/*에서 이 테이블을 읽으므로 이 migration 뒤로는 그 이미지로 되돌리지 않는다.
BEGIN;
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';
DROP TABLE IF EXISTS public.auth_users;
COMMIT;
