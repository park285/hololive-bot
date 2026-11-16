-- 그룹 역할을 NOLOGIN으로 바꾼 뒤에 실행해야 끊긴 세션이 다시 로그인하지 못한다. NOLOGIN은 이미 열린 세션을 끊지 않는다.
SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE usename = $1 AND pid <> pg_backend_pid()
