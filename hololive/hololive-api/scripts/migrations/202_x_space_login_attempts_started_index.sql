-- 최근 로그인 횟수 조회가 장기 운영의 전체 원장을 스캔하지 않게 합니다.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x_space_login_attempts_started_at ON x_space_login_attempts (started_at);
