-- 110_drop_ndo_sent_cleanup_index.sql
-- 109의 idx_ndo_terminal_cleanup이 대체한 구 cleanup partial index 제거.

DROP INDEX CONCURRENTLY IF EXISTS idx_ndo_sent_cleanup;
