# DB 사용량 최적화 고도화 리뷰 — 2026-06-08

## 적용 범위

이 패치는 hololive-bot 운영 DB 사용량을 줄이기 위한 안전 적용 1차 변경이다.

반영 항목은 다음과 같다.

1. Go runtime별 PostgreSQL pool cap 명시
2. youtube_notification_delivery lock-token batch update 개선
3. delivery/outbox claim 정렬 기준과 기존 partial index prefix 정렬
4. hot table autovacuum storage parameter migration 추가
5. 대형 인덱스 후보를 manifest 밖 maintenance SQL로 분리

## 리뷰 결론

현재 PostgreSQL GUC 자체는 512MB envelope 기준으로 큰 방향이 맞다. 남은 병목은 설정값보다 앱 쿼리 호출 패턴에 더 가깝다.

가장 큰 문제는 batch 함수 내부에 row 단위 UPDATE loop가 남아 있는 부분이다. `MarkSentBatchIfLocked`, `MarkFailedRetryBatchIfLocked`, `MarkPermanentFailureBatchIfLocked`는 호출자는 batch로 쓰지만 실제 DB round-trip은 row 수만큼 발생한다. 이 패치에서는 해당 경로를 `unnest($1::bigint[], $2::timestamptz[])` 기반 단일 UPDATE로 바꾼다.

두 번째 문제는 서비스별 pool cap 부재다. 공통 코드 기본값은 서비스당 최대 25 connection까지 열 수 있으므로, 5개 runtime을 합치면 운영 max_connections 예산을 과하게 점유할 수 있다. 이 패치에서는 compose에 서비스별 pool cap을 명시한다.

세 번째 문제는 hot table dead tuple 관리다. 상태 전환과 lock 갱신이 잦은 table은 기본 autovacuum scale factor로는 통계 갱신과 dead tuple 회수가 늦을 수 있다. 이 패치에서는 데이터 변경 없는 storage parameter migration만 추가한다.

## 반영 후 검증 쿼리

### 상위 쿼리

```sql
SELECT
  calls,
  ROUND(mean_exec_time::numeric, 3) AS mean_ms,
  ROUND(max_exec_time::numeric, 3) AS max_ms,
  rows,
  shared_blks_hit,
  shared_blks_read,
  temp_blks_read,
  temp_blks_written,
  LEFT(REGEXP_REPLACE(query, '\s+', ' ', 'g'), 220) AS query
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 30;
```

### dead tuple / table size

```sql
SELECT
  relname,
  pg_size_pretty(pg_total_relation_size(relid)) AS total_size,
  n_live_tup,
  n_dead_tup,
  last_autovacuum,
  last_autoanalyze
FROM pg_stat_user_tables
ORDER BY pg_total_relation_size(relid) DESC
LIMIT 30;
```

### 커넥션 현황

```sql
SELECT
  usename,
  application_name,
  state,
  COUNT(*)
FROM pg_stat_activity
WHERE datname = 'hololive'
GROUP BY usename, application_name, state
ORDER BY COUNT(*) DESC;
```

### hot table reloptions 확인

```sql
SELECT relname, reloptions
FROM pg_class
WHERE relname IN (
  'alarm_dispatch_deliveries',
  'youtube_notification_outbox',
  'youtube_notification_delivery',
  'youtube_content_alarm_tracking'
)
ORDER BY relname;
```

## 성공 기준

- 일반 운영 중 PostgreSQL connection 40 이하
- `youtube_notification_delivery` claim/update path의 total_exec_time 감소
- `alarm_dispatch_deliveries`와 `youtube_notification_delivery`의 dead tuple 증가 속도 감소
- claim query mean_exec_time 5ms 이하 유지
- temp file 발생 0 또는 무시 가능한 수준

## 롤백

### code/compose 롤백

```bash
git checkout -- \
  deploy/compose/docker-compose.prod.yml \
  hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store/delivery_repository_lock.go \
  hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store/delivery_repository.go \
  hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch/dispatcher_claim.go
```

### autovacuum storage parameter 롤백

```sql
ALTER TABLE IF EXISTS alarm_dispatch_deliveries RESET (
    autovacuum_vacuum_scale_factor,
    autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor,
    autovacuum_analyze_threshold
);

ALTER TABLE IF EXISTS youtube_notification_outbox RESET (
    autovacuum_vacuum_scale_factor,
    autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor,
    autovacuum_analyze_threshold
);

ALTER TABLE IF EXISTS youtube_notification_delivery RESET (
    autovacuum_vacuum_scale_factor,
    autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor,
    autovacuum_analyze_threshold
);

ALTER TABLE IF EXISTS youtube_content_alarm_tracking RESET (
    autovacuum_vacuum_scale_factor,
    autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor,
    autovacuum_analyze_threshold
);
```
