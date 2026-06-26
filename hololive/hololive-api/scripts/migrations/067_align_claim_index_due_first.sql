-- 067_align_claim_index_due_first.sql
-- claim 경로 ORDER BY가 (next_attempt_at, created_at, id)로 확정되어(b656bec7),
-- 기존 (next_attempt_at, created_at) prefix 인덱스를 id tie-break까지 포함한 완전 매칭 인덱스로 교체한다.
-- partial 인덱스도 빌드 시 전체 힙을 스캔하므로(WHERE 평가), 비-CONCURRENT CREATE는 테이블 전체
-- 크기(SENT/FAILED 누적 포함)에 비례한 SHARE lock으로 claim write를 차단한다. CONCURRENTLY로 lock 없이 교체한다.
-- apply-all.sh는 psql -f(파일 단위 autocommit, single-transaction 미사용)로 적용하므로 CONCURRENTLY를 쓸 수 있다.
-- 첫 적용 후 \d 로 invalid index 잔재를 확인하고, 잔재가 있으면 DROP 후 재실행한다(IF NOT EXISTS는 이름만 비교).

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_ynd_pending_due_created_id
    ON youtube_notification_delivery (next_attempt_at, created_at, id)
    WHERE status = 'PENDING';

DROP INDEX CONCURRENTLY IF EXISTS idx_ynd_pending_next;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yno_pending_due_created_id
    ON youtube_notification_outbox (next_attempt_at, created_at, id)
    WHERE status = 'PENDING';

DROP INDEX CONCURRENTLY IF EXISTS idx_yno_pending_next_created;
