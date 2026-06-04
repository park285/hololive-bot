# PG18 전범위 성능 / I/O / 인덱스 작업 문서 — hololive-bot

작성일: 2026-06-04
대상 저장소: `park285/hololive-bot`
대상 런타임: `holo-postgres`, `hololive-bot`, `hololive-admin-api`, `hololive-alarm-worker`, `llm-scheduler`, `youtube-producer`
상태: **결정 완료 / 구현 가능 / 운영 검증 절차 포함**

## 1. 목적

이 문서는 PostgreSQL 18 전환 후 `hololive-bot` 인프라에서 성능, I/O, 인덱스, 관측, 커넥션 풀, VACUUM/ANALYZE 운영까지 한 번에 정렬하기 위한 작업 기준이다.

핵심 목표는 다음과 같다.

1. PostgreSQL 18의 AIO, `pg_stat_io`, `pg_stat_statements`, skip scan 관측 능력을 실제 운영 판단에 연결한다.
2. 512MB 제한을 가진 `holo-postgres`에서 메모리 과약정을 제거한다.
3. 이미 좋은 실행계획을 보이는 쿼리에는 불필요한 인덱스를 추가하지 않는다.
4. 장기적으로 데이터가 증가했을 때 어떤 지표를 보고 어떤 인덱스를 추가할지 사전에 결정한다.
5. 모든 변경은 롤백 가능하고, `docker-compose.prod.yml` 기준 운영 절차와 충돌하지 않아야 한다.

공식 PostgreSQL 18 기준 문서:

- PostgreSQL 18 release notes: https://www.postgresql.org/docs/18/release-18.html
- PostgreSQL 18 resource configuration: https://www.postgresql.org/docs/18/runtime-config-resource.html
- PostgreSQL 18 cumulative statistics / `pg_stat_io`: https://www.postgresql.org/docs/18/monitoring-stats.html
- `pg_stat_statements`: https://www.postgresql.org/docs/18/pgstatstatements.html
- `pg_trgm`: https://www.postgresql.org/docs/18/pgtrgm.html

## 2. 현재 상태 요약

현재 `docker-compose.prod.yml`의 `holo-postgres`는 `postgres:18` 계열을 사용하고 있고, PG18 Track A에서 다음이 이미 적용되어 있다.

- `shared_preload_libraries=pg_stat_statements`
- `pg_stat_statements.track=all`
- `pg_stat_statements.max=5000`
- `compute_query_id=on`
- `track_io_timing=on`
- `track_wal_io_timing=on`
- `shared_buffers=128MB`
- `effective_cache_size=256MB`
- `maintenance_work_mem=64MB`
- `work_mem=4MB`
- `io_method=worker`
- `io_workers=3`
- `effective_io_concurrency=16`
- `maintenance_io_concurrency=16`

기존 Track A 문서 기준으로 다음도 확인되어 있다.

- 의존 5개 서비스 healthy 유지
- 재연결 에러 0건
- `VACUUM (ANALYZE)` 부하 중 OOM 없음
- `pg_stat_statements 1.12` 수집 동작
- `pg_stat_io(object='wal')` 동작
- 주요 claim 쿼리의 `EXPLAIN ANALYZE` 캡처 완료

따라서 이 문서의 결론은 **PG18 서버 GUC 자체는 이미 올바른 방향으로 적용되어 있으며, 남은 핵심 작업은 커넥션 풀 예산 명시화, autovacuum 정책 명시화, 인덱스 추가/비추가 결정의 정본화**이다.

## 3. 불변 원칙

### 3.1. 운영 DB 변경은 3단계로 나눈다

1. 관측/설정 변경: `docker-compose.prod.yml`의 PostgreSQL `command` 또는 service env 변경
2. 스키마 변경: migration SQL 추가
3. 코드 경로 변경: Go 코드의 SQL 또는 repository 로직 변경

세 단계를 한 커밋/한 배포에 섞지 않는다. 장애가 났을 때 원인 분리가 어려워지기 때문이다.

### 3.2. manifest migration에는 `CREATE INDEX CONCURRENTLY`를 넣지 않는다

`hololive/hololive-kakao-bot-go/scripts/migrations`의 manifest 실행 경로는 부팅/배포 절차와 연결된다. 운영 중 대형 인덱스를 안전하게 만들려면 별도 maintenance script 또는 수동 DBA 절차에서 `CREATE INDEX CONCURRENTLY`를 실행한다.

작은 테이블 또는 빈 테이블 기준의 idempotent migration에는 `CREATE INDEX IF NOT EXISTS`만 허용한다.

### 3.3. 인덱스는 “느릴 수 있어서”가 아니라 “느린 증거가 있어서” 추가한다

PG18의 `pg_stat_statements`, `EXPLAIN (ANALYZE, BUFFERS)`, `pg_stat_io`를 기준으로 판단한다. 현재 데이터가 shared buffer 안에 있고 execution time이 sub-ms라면 추가 인덱스는 오히려 쓰기 비용과 VACUUM 비용만 늘릴 수 있다.

## 4. 최종 결정 사항

### 결정 H1 — PostgreSQL image pinning

운영 환경 변수에서는 `POSTGRES_IMAGE=postgres:18.4`로 pinning한다. Compose fallback은 `postgres:18`을 유지해도 되지만, production env는 patch version까지 고정한다.

이유:

- `postgres:18`은 patch release를 자동으로 따라가므로 재현성이 낮다.
- 운영 장애 분석에서는 정확한 minor/patch 버전이 중요하다.
- patch 업그레이드는 별도 maintenance window에서 진행한다.

적용 위치:

```env
# /run/hololive-bot/env 또는 운영 env 렌더링 원본
POSTGRES_IMAGE=postgres:18.4
```

### 결정 H2 — PG18 GUC는 현재 Track A 값을 유지한다

`docker-compose.prod.yml`의 `holo-postgres.command` 값은 현재 값을 정본으로 둔다.

정본 값:

```yaml
command:
  - postgres
  - "-c"
  - "shared_preload_libraries=pg_stat_statements"
  - "-c"
  - "pg_stat_statements.track=all"
  - "-c"
  - "pg_stat_statements.max=5000"
  - "-c"
  - "compute_query_id=on"
  - "-c"
  - "track_io_timing=on"
  - "-c"
  - "track_wal_io_timing=on"
  - "-c"
  - "shared_buffers=128MB"
  - "-c"
  - "effective_cache_size=256MB"
  - "-c"
  - "maintenance_work_mem=64MB"
  - "-c"
  - "work_mem=4MB"
  - "-c"
  - "io_method=worker"
  - "-c"
  - "io_workers=3"
  - "-c"
  - "effective_io_concurrency=16"
  - "-c"
  - "maintenance_io_concurrency=16"
```

변경하지 않는 항목:

- `max_connections`: 현재는 기본 100 유지. 아래 pool cap 적용 후 이론상 동시 커넥션이 100을 넘지 않게 되므로 낮출 필요가 없다.
- `cache_statement`: 운영 기본은 계속 `exec`. 지금은 top query 평균 지연이 sub-ms라 statement cache의 이득보다 per-connection cache 메모리 비용이 더 중요하다.
- `io_method=io_uring`: 공식 Docker image와 호스트 liburing 조건이 명확히 검증되기 전까지 사용하지 않는다. 정본은 `worker`이다.

### 결정 H3 — pool cap을 서비스별로 명시한다

현재 문서 기준 이론상 pool 합은 `4서비스 × 기본 25 + alarm-worker 8 = 108`로 `max_connections=100`을 넘을 수 있다. 실측은 33conn 수준이라 즉시 장애 위험은 낮지만, 이론상 과약정은 제거해야 한다.

`docker-compose.prod.yml`에서 각 서비스 env에 다음을 명시한다.

```yaml
hololive-bot:
  environment:
    POSTGRES_POOL_MIN_CONNS: ${BOT_POSTGRES_POOL_MIN_CONNS:-1}
    POSTGRES_POOL_MAX_CONNS: ${BOT_POSTGRES_POOL_MAX_CONNS:-4}
    POSTGRES_POOL_MAX_IDLE_CONNS: ${BOT_POSTGRES_POOL_MAX_IDLE_CONNS:-2}

hololive-admin-api:
  environment:
    POSTGRES_POOL_MIN_CONNS: ${ADMIN_API_POSTGRES_POOL_MIN_CONNS:-1}
    POSTGRES_POOL_MAX_CONNS: ${ADMIN_API_POSTGRES_POOL_MAX_CONNS:-4}
    POSTGRES_POOL_MAX_IDLE_CONNS: ${ADMIN_API_POSTGRES_POOL_MAX_IDLE_CONNS:-2}

llm-scheduler:
  environment:
    POSTGRES_POOL_MIN_CONNS: ${LLM_SCHEDULER_POSTGRES_POOL_MIN_CONNS:-1}
    POSTGRES_POOL_MAX_CONNS: ${LLM_SCHEDULER_POSTGRES_POOL_MAX_CONNS:-4}
    POSTGRES_POOL_MAX_IDLE_CONNS: ${LLM_SCHEDULER_POSTGRES_POOL_MAX_IDLE_CONNS:-2}

youtube-producer:
  environment:
    POSTGRES_POOL_MIN_CONNS: ${YOUTUBE_PRODUCER_POSTGRES_POOL_MIN_CONNS:-2}
    POSTGRES_POOL_MAX_CONNS: ${YOUTUBE_PRODUCER_POSTGRES_POOL_MAX_CONNS:-8}
    POSTGRES_POOL_MAX_IDLE_CONNS: ${YOUTUBE_PRODUCER_POSTGRES_POOL_MAX_IDLE_CONNS:-4}

hololive-alarm-worker:
  environment:
    POSTGRES_POOL_MIN_CONNS: ${ALARM_WORKER_POSTGRES_POOL_MIN_CONNS:-1}
    POSTGRES_POOL_MAX_CONNS: ${ALARM_WORKER_POSTGRES_POOL_MAX_CONNS:-8}
    POSTGRES_POOL_MAX_IDLE_CONNS: ${ALARM_WORKER_POSTGRES_POOL_MAX_IDLE_CONNS:-4}
```

결과:

- 앱 서비스 이론상 합: 4 + 4 + 4 + 8 + 8 = 28
- migration/admin 접속 여유 포함해도 `max_connections=100` 내에서 충분하다.
- `work_mem=4MB`의 worst-case 동시 사용 리스크가 크게 줄어든다.

### 결정 H4 — `pg_stat_statements` extension 생성 경로는 현재 유지한다

fresh volume은 다음 파일로 처리한다.

```sql
-- hololive/hololive-kakao-bot-go/scripts/init-db/05-create-pg-stat-statements.sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

기존 운영 볼륨은 init-db가 재실행되지 않으므로 1회 수동 실행만 허용한다.

```bash
docker exec -i holo-postgres psql \
  -U "${POSTGRES_ADMIN_USER:-postgres_admin}" \
  -d hololive \
  -p 5433 \
  -c 'CREATE EXTENSION IF NOT EXISTS pg_stat_statements;'
```

### 결정 H5 — autovacuum table storage parameter를 명시한다

다음 테이블은 상태 전환, retry, lock 갱신으로 dead tuple이 누적되기 쉽다.

- `alarm_dispatch_deliveries`
- `youtube_notification_outbox`
- `youtube_notification_delivery`
- `youtube_content_alarm_tracking`

새 migration 파일을 추가한다.

권장 파일명:

```text
hololive/hololive-kakao-bot-go/scripts/migrations/0XX_pg18_autovacuum_hot_tables.sql
```

내용:

```sql
-- PG18 hot table autovacuum policy.
-- 목적: 상태 전환/lock 갱신이 잦은 큐성 테이블의 dead tuple 회수와 통계 갱신 지연을 줄인다.

ALTER TABLE IF EXISTS alarm_dispatch_deliveries SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);

ALTER TABLE IF EXISTS youtube_notification_outbox SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);

ALTER TABLE IF EXISTS youtube_notification_delivery SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);

ALTER TABLE IF EXISTS youtube_content_alarm_tracking SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_vacuum_threshold = 100,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 100
);
```

이 변경은 데이터 자체를 바꾸지 않고 storage parameter만 바꾼다. 롤백은 `RESET`으로 한다.

```sql
ALTER TABLE IF EXISTS alarm_dispatch_deliveries RESET (
    autovacuum_vacuum_scale_factor,
    autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor,
    autovacuum_analyze_threshold
);
```

### 결정 H6 — 현재 claim 인덱스는 유지하고, blind index 추가는 금지한다

현재 `alarm_dispatch_deliveries`는 다음 partial index가 이미 있다.

```sql
CREATE INDEX IF NOT EXISTS idx_alarm_dispatch_deliveries_due
    ON alarm_dispatch_deliveries (next_attempt_at ASC, id ASC)
    WHERE status IN ('pending', 'retry');
```

PG18 EXPLAIN 기준으로 이 인덱스는 정상 사용된다. 따라서 추가 인덱스는 만들지 않는다.

`youtube_notification_delivery`도 다음 partial index가 이미 있다.

```sql
CREATE INDEX IF NOT EXISTS idx_ynd_pending_next
    ON youtube_notification_delivery(next_attempt_at, created_at)
    WHERE status = 'PENDING';
```

현재 데이터 크기에서는 delivery claim이 Seq Scan이어도 정상이다. 테이블이 작으면 Seq Scan이 인덱스보다 빠르다.

`youtube_notification_outbox`는 현재 다음 인덱스가 있다.

```sql
CREATE INDEX IF NOT EXISTS idx_yno_status_created ON youtube_notification_outbox(status, created_at);
CREATE INDEX IF NOT EXISTS idx_yno_status_next_attempt ON youtube_notification_outbox(status, next_attempt_at) WHERE status = 'PENDING';
```

현재 EXPLAIN 재캡처에서는 `idx_yno_status_created` 또는 `idx_yno_status_next_attempt` 계열로 충분하다. 즉시 새 인덱스를 추가하지 않는다.

단, 다음 조건 중 하나가 충족되면 별도 maintenance로 아래 인덱스를 추가한다.

조건:

- `pg_stat_statements`에서 outbox claim query `mean_exec_time > 5ms`
- `EXPLAIN (ANALYZE, BUFFERS)`에서 `Rows Removed by Filter > 1000`
- pending outbox가 10,000 rows 이상 유지
- `pg_stat_io`에서 relation read가 급증하고 shared buffer hit만으로 처리되지 않음

추가 후보:

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_yno_pending_due_created
    ON youtube_notification_outbox (next_attempt_at ASC, created_at ASC, id ASC)
    WHERE status = 'PENDING';
```

이 인덱스는 manifest migration에 넣지 않는다. 운영 중 추가 시에는 별도 maintenance command로 실행한다.

### 결정 H7 — skip scan을 위한 인덱스 재설계는 하지 않는다

PG18은 multicolumn B-tree skip scan을 지원하지만, 현재 주요 쿼리는 다음 중 하나다.

- 단일 PK / BIGSERIAL claim
- status 내장 partial index
- room/channel/time prefix가 명확한 lookup
- 전체 members 조회 후 정렬

따라서 skip scan을 노리고 다중 컬럼 인덱스를 새로 설계하지 않는다. 현재 구조에서 skip scan 미사용은 문제의 신호가 아니라 정상 결론이다.

### 결정 H8 — `uuidv7()`, virtual generated columns, temporal constraint는 즉시 채택하지 않는다

판정:

- `uuidv7()`: 신규 시계열 테이블을 만들 때만 후보. 기존 BIGSERIAL PK를 바꾸지 않는다.
- virtual generated columns: 현재 ROI 없음. 읽기 계산 컬럼이 실제 쿼리 병목으로 확인될 때만 채택한다.
- `WITHOUT OVERLAPS`: `lock_expires_at` lease 중복 방지에 이론상 후보지만, 현재는 앱단 lease + status transition으로 관리한다. 별도 스키마 전환 플랜 전에는 적용하지 않는다.
- `RETURNING old.*/new.*`: audit/diff 단순화 후보. claim/update repository를 리팩터링할 때만 채택한다.

## 5. 실행 순서

### Phase 1 — env / Compose 정렬

1. 운영 env에 `POSTGRES_IMAGE=postgres:18.4` 설정
2. 서비스별 `POSTGRES_POOL_*` env 추가
3. `docker compose config`로 렌더링 확인

검증:

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml config | grep -E 'POSTGRES_IMAGE|POSTGRES_POOL|pg_stat_statements|io_method|shared_buffers'
```

### Phase 2 — PostgreSQL recreate

GUC 중 `shared_preload_libraries`, `shared_buffers`, `io_method`, `io_workers`는 postmaster/server-start context이다. 변경 후 recreate가 필요하다.

```bash
./scripts/deploy/compose-redeploy-service.sh holo-postgres
```

검증:

```sql
SHOW server_version;
SHOW shared_preload_libraries;
SHOW compute_query_id;
SHOW track_io_timing;
SHOW track_wal_io_timing;
SHOW shared_buffers;
SHOW effective_cache_size;
SHOW work_mem;
SHOW maintenance_work_mem;
SHOW io_method;
SHOW io_workers;
SELECT extname, extversion FROM pg_extension WHERE extname = 'pg_stat_statements';
```

### Phase 3 — autovacuum migration

1. `0XX_pg18_autovacuum_hot_tables.sql` 추가
2. manifest에 순서 추가
3. db migration 실행

```bash
./scripts/deploy/compose-redeploy-service.sh hololive-db-migrate
```

검증:

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

### Phase 4 — 통계 갱신

```sql
VACUUM (ANALYZE, VERBOSE) alarm_dispatch_deliveries;
VACUUM (ANALYZE, VERBOSE) youtube_notification_outbox;
VACUUM (ANALYZE, VERBOSE) youtube_notification_delivery;
VACUUM (ANALYZE, VERBOSE) youtube_content_alarm_tracking;
ANALYZE members;
```

### Phase 5 — 쿼리 관측

상위 쿼리:

```sql
SELECT
  calls,
  round(mean_exec_time::numeric, 3) AS mean_ms,
  round(max_exec_time::numeric, 3) AS max_ms,
  rows,
  shared_blks_hit,
  shared_blks_read,
  temp_blks_read,
  temp_blks_written,
  left(regexp_replace(query, '\s+', ' ', 'g'), 180) AS query
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 30;
```

I/O:

```sql
SELECT
  backend_type,
  object,
  context,
  reads,
  read_time,
  writes,
  write_time,
  fsyncs,
  fsync_time,
  read_bytes,
  write_bytes
FROM pg_stat_io
ORDER BY object, backend_type, context;
```

커넥션:

```sql
SELECT usename, application_name, state, count(*)
FROM pg_stat_activity
WHERE datname = 'hololive'
GROUP BY usename, application_name, state
ORDER BY count(*) DESC;
```

인덱스 사용량:

```sql
SELECT
  relname,
  indexrelname,
  idx_scan,
  idx_tup_read,
  idx_tup_fetch
FROM pg_stat_user_indexes
WHERE relname IN (
  'alarm_dispatch_deliveries',
  'youtube_notification_outbox',
  'youtube_notification_delivery',
  'alarm_dispatch_events',
  'members'
)
ORDER BY relname, idx_scan DESC;
```

## 6. 성공 기준

필수:

- 5개 Go runtime이 healthy
- PostgreSQL OOMKilled=false
- `pg_stat_statements` 수집 가능
- `pg_stat_io`에서 relation/wal row 조회 가능
- `pg_stat_activity` 기준 일반 운영 중 동시 연결 40 이하
- `alarm_dispatch_deliveries` claim query mean < 5ms
- outbox claim query mean < 5ms
- temp file 발생 0 또는 무시 가능한 수준

권장:

- PostgreSQL RSS가 steady-state에서 512MB cap의 70% 이하
- `VACUUM (ANALYZE)` 중에도 80% 이하
- `shared_blks_read` 급증 없음
- `idx_scan=0`인 신규 인덱스 없음

## 7. 롤백

### Compose/GUC 롤백

```bash
git revert <compose-change-commit>
./scripts/deploy/compose-redeploy-service.sh holo-postgres
```

데이터 볼륨은 건드리지 않는다.

### pool cap 롤백

서비스 env에서 `POSTGRES_POOL_*` override를 제거하면 기존 기본값으로 돌아간다. 단, 이론상 pool 합이 다시 커지므로 임시 롤백으로만 허용한다.

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
```

## 8. 최종 요약

`hololive-bot`의 PG18 최적화는 이미 Track A에서 핵심 GUC와 관측이 적용되어 있다. 남은 완결 작업은 다음 3가지다.

1. `postgres:18.4` image pinning
2. 서비스별 PostgreSQL pool cap 명시화
3. hot table autovacuum storage parameter 추가

인덱스는 현재 즉시 추가하지 않는다. 이미 측정된 실행계획상 주요 claim 쿼리는 partial index 또는 작은 테이블 Seq Scan으로 정상 동작한다. 새 인덱스는 `pg_stat_statements`와 `EXPLAIN`으로 병목이 확인될 때 maintenance 절차로만 추가한다.
