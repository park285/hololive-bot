-- 067_align_claim_index_due_first.sql
-- claim 경로 ORDER BY가 (next_attempt_at, created_at, id)로 확정되어(b656bec7),
-- 기존 (next_attempt_at, created_at) prefix 인덱스를 id tie-break까지 포함한 완전 매칭 인덱스로 교체한다.
-- WHERE status='PENDING' partial 대상은 처리 대기분이라 작고, 일반 CREATE/DROP의 lock 시간은 짧다.
-- PENDING 규모가 비정상적으로 큰 환경은 scripts/maintenance의 CONCURRENTLY 경로를 운영 중 별도 적용한다.

CREATE INDEX IF NOT EXISTS idx_ynd_pending_due_created_id
    ON youtube_notification_delivery (next_attempt_at, created_at, id)
    WHERE status = 'PENDING';

DROP INDEX IF EXISTS idx_ynd_pending_next;

CREATE INDEX IF NOT EXISTS idx_yno_pending_due_created_id
    ON youtube_notification_outbox (next_attempt_at, created_at, id)
    WHERE status = 'PENDING';

DROP INDEX IF EXISTS idx_yno_pending_next_created;
