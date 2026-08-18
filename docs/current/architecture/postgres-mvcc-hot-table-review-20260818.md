# PostgreSQL MVCC Hot Table 리뷰 및 개선 계획 — 2026-08-18

## 1. 문서 목적

이 문서는 `hololive-bot`의 PostgreSQL 사용 패턴을 MVCC(Multi-Version Concurrency Control),
HOT(Heap-Only Tuple), VACUUM, 인덱스 유지 비용 관점에서 다시 검토한 결과를 정리합니다.

검토 대상은 단순히 크기가 큰 테이블이 아닙니다. 다음 조건을 만족하는 테이블을 우선했습니다.

1. 같은 row를 반복해서 `UPDATE`한다.
2. 상태, lease, retry 시각처럼 인덱스 predicate 또는 인덱스 key에 들어간 컬럼을 바꾼다.
3. 여러 worker가 `FOR UPDATE SKIP LOCKED` 또는 fence 조건으로 동시에 접근한다.
4. retention `DELETE`가 반복되어 dead tuple을 지속적으로 만든다.
5. 오래 열린 transaction 하나가 VACUUM horizon을 붙잡을 때 운영 영향이 큰 경로다.

이번 변경은 운영 DB 통계 없이 위험한 스키마 변경을 밀어 넣지 않는 것을 원칙으로 합니다.
따라서 다음 두 종류의 변경만 포함합니다.

- 장기 `idle in transaction` 세션을 5분 뒤 종료하는 데이터베이스 기본값
- MVCC 비용과 후속 인덱스 제거 여부를 판단할 수 있는 운영 증적 수집 확대

`fillfactor`, hot table별 autovacuum storage parameter, 인덱스 제거는 이 문서의 판정 조건을
충족한 뒤 별도 migration으로 진행합니다.

---

## 2. 결론

현재 PostgreSQL을 교체해야 할 구조는 아닙니다. 오히려 `source_observations`를 append-only
관측 원장으로 두고, queue와 lease를 별도 상태 테이블로 분리한 방향은 PostgreSQL에 적합합니다.

다만 쓰기 비용은 다음 두 테이블에 집중됩니다.

1. `source_observation_queue`
2. `youtube_collection_job_leases`

두 테이블은 상태 전환 시 인덱스에 포함된 컬럼을 바꾸므로 HOT UPDATE를 거의 기대하기 어렵습니다.
즉 `fillfactor`만 낮춰서는 근본적인 write amplification이 줄지 않습니다.

반면 `source_collection_checkpoints`는 primary key를 유지한 채 비인덱스 값 위주로 갱신하므로
page 여유가 있다면 HOT UPDATE 효과를 받을 가능성이 큽니다. 이 테이블은 먼저 HOT 비율을
측정한 뒤 `fillfactor`를 판단하는 편이 맞습니다.

운영상 가장 위험한 공통 실패 모드는 오래 열린 transaction입니다. 하나의 세션이 transaction을
연 상태로 idle이 되면, 해당 snapshot보다 새로운 dead tuple을 VACUUM이 제거하지 못할 수 있습니다.
따라서 이번 변경에서는 `idle_in_transaction_session_timeout=5min`을 database default로 설정합니다.

---

## 3. 실제 데이터 흐름을 MVCC 관점에서 보기

YouTube 관측 경로를 단순화하면 다음과 같습니다.

```text
youtube-collector
    |
    | lease acquire / renew / complete
    v
youtube_collection_job_leases
    |
    | PublishBatch transaction
    v
source_observations              INSERT 중심
source_observation_queue         INSERT + 반복 UPDATE
source_collection_checkpoints    UPSERT
    |
    | claim / consume / complete
    v
source_observation_queue         PENDING -> PROCESSING -> PROCESSED
    |
    | retention batch
    v
DELETE -> dead tuple -> VACUUM
```

PostgreSQL에서 일반적인 `UPDATE`는 기존 tuple을 제자리에서 덮어쓰는 작업이 아닙니다.
새 tuple version을 만들고 이전 version을 나중에 VACUUM이 회수합니다.

따라서 정상 observation 한 건도 queue에서는 대략 다음 version을 만듭니다.

```text
v1: PENDING
v2: PROCESSING      -- v1 dead
v3: PROCESSED       -- v2 dead
retention DELETE    -- v3 dead
VACUUM              -- 회수
```

retry가 발생하면 `PENDING <-> PROCESSING` 전환 횟수만큼 version과 인덱스 변경이 추가됩니다.

---

## 4. 테이블별 상세 리뷰

### 4.1 `source_observations`: append-only 성격은 유지해야 함

정의 위치:

- `hololive/hololive-api/scripts/migrations/144_source_observation_outbox.sql`
- `hololive/hololive-shared/pkg/service/youtube/sourceobservation/queries/repository_publish_set_0032_32.sql`

관측 결과는 기존 row를 갱신하기보다 새 row로 `INSERT`됩니다. 동일 identity가 이미 있으면
`DUPLICATE`, evidence hash가 다르면 `COLLISION`으로 분기합니다.

이 구조의 장점은 다음과 같습니다.

- 수집 evidence를 immutable하게 유지한다.
- 반복 UPDATE로 인한 version chain이 생기지 않는다.
- publish replay와 collision 진단이 단순하다.
- queue 상태 churn을 원장 테이블과 분리한다.

이 테이블의 MVCC 비용은 주로 retention `DELETE`에서 발생합니다. 현재 retention은 최대 1,000건
batch와 `SKIP LOCKED`를 사용하므로 방향은 적절합니다. 대규모 단일 transaction으로 바꾸지 않아야 합니다.

개선 판단 지표:

- `n_tup_del`
- `n_dead_tup`
- `last_autovacuum`
- 전체 relation 크기
- retention backlog age

`source_observations`는 UPDATE 최적화 대상이 아니라 retention과 VACUUM 처리량 대상입니다.

### 4.2 `source_observation_queue`: 가장 전형적인 non-HOT 상태 테이블

관련 SQL:

- `repository_claim_0012_12.sql`
- `repository_complete_0015_15.sql`
- `repository_retry_0017_17.sql`
- `repository_dead_letter_0018_18.sql`
- `repository_retention_delete_queue_0076_76.sql`

상태 전환은 다음과 같습니다.

```text
PENDING
  -> PROCESSING
  -> PROCESSED

PENDING
  -> PROCESSING
  -> PENDING        retry
  -> PROCESSING
  -> DEAD_LETTER
```

현재 partial index는 상태별 hot path를 정확히 지원합니다.

- `status='PENDING'`에서 `(available_at, observation_id)`
- `status='PROCESSING'`에서 `(lease_expires_at, observation_id)`
- terminal 상태에서 `(status, updated_at, observation_id)`

조회 성능에는 맞는 설계지만, 상태가 바뀔 때 row가 partial index 간에 이동합니다.
따라서 다음 UPDATE는 HOT이 될 수 없습니다.

```text
PENDING -> PROCESSING
PROCESSING -> PROCESSED
PROCESSING -> PENDING
PROCESSING -> DEAD_LETTER
```

`fillfactor`는 같은 page에 새 tuple을 넣을 공간을 제공할 뿐입니다. 인덱스 key 또는 predicate가
변경되면 PostgreSQL은 어차피 인덱스를 갱신해야 하므로, 이 테이블에 `fillfactor`를 먼저 적용하는 것은
write amplification의 근본 해결이 아닙니다.

우선 확인할 지표:

- `n_tup_upd`
- `n_tup_hot_upd`
- `hot_update_pct`
- `n_dead_tup`
- `dead_tuple_pct`
- claim/recovery/terminal partial index의 `idx_scan`
- `last_autovacuum`, `autovacuum_count`

예상되는 정상 패턴은 HOT 비율이 낮더라도 autovacuum이 충분히 자주 따라오는 상태입니다.
HOT 비율이 낮다는 사실만으로 장애라고 판단하면 안 됩니다.

### 4.3 `youtube_collection_job_leases`: 작은 테이블이어도 write churn이 큼

관련 SQL:

- `repository_lease_acquire_0144_08.sql`
- `repository_lease_renew_0144_09.sql`
- `repository_lease_release_0144_10.sql`
- `repository_lease_complete_0144_11.sql`
- `repository_lease_defer_0144_12.sql`

같은 job row가 반복해서 다음 상태를 오갑니다.

```text
IDLE -> ACTIVE -> IDLE
IDLE -> ACTIVE -> DEFERRED -> ACTIVE
ACTIVE -> ACTIVE              lease renew
```

변경되는 컬럼에는 다음이 포함됩니다.

- `slot_state`
- `next_due_at`
- `retry_not_before`
- `lease_expires_at`
- `fence_epoch`
- `owner_instance`
- `updated_at`

`idx_youtube_collection_job_due`는 아래 컬럼을 모두 key로 가집니다.

```text
slot_state,
next_due_at,
retry_not_before,
lease_expires_at,
job_key
```

따라서 acquire, renew, complete, defer 대부분이 이 인덱스를 다시 써야 합니다. 특히 renew는
논리적으로 만료 시각 하나를 연장하는 작업이지만 heap tuple, WAL, index entry를 함께 만듭니다.

#### 넓은 due index의 제거 후보 판정

현재 subject candidate SQL과 global candidate SQL은 lease row를 `job_key`로 조인합니다.
`job_key`는 primary key이므로 정적 코드만 보면 넓은 due index가 후보 조회에 직접 필요할 가능성은 낮습니다.

그러나 이번 변경에서 바로 삭제하지 않습니다. 이유는 다음과 같습니다.

1. 운영 통계의 `idx_scan`을 아직 확인하지 않았다.
2. planner가 실제 target cardinality에서 어떤 join 순서를 선택하는지 운영 EXPLAIN이 없다.
3. stats가 최근 reset된 상태의 `idx_scan=0`은 제거 근거가 아니다.
4. index 제거는 rollback 시 재생성 비용과 lock/IO 계획이 필요하다.

삭제 migration을 만들기 위한 필수 조건은 다음과 같습니다.

- `pg_stat_database.stats_reset`이 바뀌지 않은 연속 관측 구간
- 대표 부하가 포함된 최소 수일의 snapshot
- `idx_youtube_collection_job_due.idx_scan`이 0 또는 무시 가능한 수준
- subject/global candidate 쿼리의 `EXPLAIN (ANALYZE, BUFFERS)`가 PK lookup으로 안정적
- index 제거 후 canary에서 acquire latency와 DB write latency가 악화되지 않음

조건을 만족하면 별도 migration에서 다음을 검토합니다.

```sql
DROP INDEX CONCURRENTLY IF EXISTS idx_youtube_collection_job_due;
```

이 변경은 lease renew의 index write 하나를 제거할 수 있어 실제 write amplification 감소 효과가 큽니다.

### 4.4 `source_collection_checkpoints`: HOT 가능성을 측정할 대상

checkpoint는 다음 key로 conflict를 판정합니다.

```text
provider,
observation_kind,
subject_key,
scope_sha256
```

UPSERT 시 위 key는 유지하고 다음 값들을 갱신합니다.

- latest observation/evidence
- schedule/success 시각
- latency
- continuity/cursor
- error 상태
- `updated_at`

추가 secondary index가 갱신 컬럼을 포함하지 않는다면 HOT UPDATE가 가능한 형태입니다.
다만 같은 heap page에 새 tuple을 넣을 공간이 있어야 합니다.

판정 순서:

1. 충분한 UPDATE 누적 후 `n_tup_upd` 확인
2. `n_tup_hot_upd / n_tup_upd` 계산
3. HOT 비율이 지속적으로 낮고 page split/bloat 증거가 있을 때만 `fillfactor` 검토
4. 적용 전후 relation size와 buffer write를 함께 비교

실무 시작 기준으로 HOT 비율 80% 미만을 조사 신호로 사용할 수 있지만, 이는 보편적인 정답이 아니라
이 프로젝트의 운영 비교 기준입니다. row 크기, page fullness, update 패턴과 함께 판단해야 합니다.

### 4.5 `alarm_dispatch_deliveries`와 기존 outbox: 이미 올바른 선례가 있음

기존 migration은 상태 전환과 lock 갱신이 잦은 다음 테이블에 더 공격적인 autovacuum 기준을 적용합니다.

- `alarm_dispatch_deliveries`
- `youtube_notification_outbox`
- `youtube_notification_delivery`
- `notification_delivery_outbox`
- `youtube_community_shorts_alarm_states`
- `youtube_notification_delivery_telemetry`

특히 telemetry migration은 predicate 컬럼 변경으로 non-HOT UPDATE가 발생한다는 점을 명시합니다.
새 queue와 lease 테이블도 같은 계열이지만, 이번에는 먼저 동일한 통계를 수집하고 실제 발생 속도를 확인합니다.

---

## 5. 오래 열린 transaction이 가장 위험한 공통 원인인 이유

PostgreSQL은 snapshot에서 볼 가능성이 있는 과거 tuple을 VACUUM으로 제거할 수 없습니다.
애플리케이션이 다음 상태로 오래 남으면 문제가 됩니다.

```text
BEGIN
SELECT ...
-- client가 다음 요청을 보내지 않음
-- COMMIT/ROLLBACK도 하지 않음
```

이 세션은 CPU를 쓰지 않아도 다음 영향을 만들 수 있습니다.

- dead tuple 회수 지연
- relation/index bloat 증가
- freeze horizon 전진 지연
- autovacuum 반복 작업 증가
- 오래된 row version을 읽어야 하는 scan 비용 증가
- lock을 잡고 있었다면 직접적인 blocking

이번 migration은 database default로 다음 값을 설정합니다.

```text
idle_in_transaction_session_timeout = 5min
```

이 설정은 다음 세션을 종료합니다.

- 열린 transaction 안에서
- client query를 기다리며
- 5분 이상 idle인 세션

다음에는 적용되지 않습니다.

- transaction 밖의 일반 idle pool connection
- 실행 중인 active query
- 5분보다 짧은 정상 transaction

설정은 새 session부터 적용되므로 migration 직후 기존 pool connection에는 즉시 반영되지 않습니다.
배포 시 각 애플리케이션의 connection pool을 순차적으로 재기동해야 합니다.

`transaction_timeout`은 active transaction 전체에도 영향을 주어 더 강한 정책이므로 이번 범위에서 설정하지 않습니다.
각 runtime의 context deadline과 실제 장기 작업을 먼저 감사한 뒤 별도로 판단해야 합니다.

---

## 6. 이번 PR에서 추가하는 운영 증적

`./scripts/runtime/pg-hotpath-explain-snapshot.sh`의 기존 claim plan 검증은 유지합니다.
다음 artifact를 추가합니다.

### 6.1 `mvcc-database-state.txt`

수집 항목:

- 현재 database/user
- `idle_in_transaction_session_timeout`
- `transaction_timeout`
- `statement_timeout`
- `datfrozenxid` age
- `datminmxid` age
- `autovacuum_freeze_max_age`
- DB stats reset 시각

이 artifact는 migration이 새 session에 실제 반영됐는지와 freeze 위험을 함께 확인합니다.

### 6.2 `dead-tuples-autovacuum.txt`

기존 출력에 다음을 추가합니다.

- relation total size
- live/dead tuple 및 dead tuple 비율
- insert/update/HOT update/delete 누적량
- HOT update 비율
- manual/auto VACUUM 및 ANALYZE 횟수와 마지막 실행 시각

관측 대상을 새 YouTube convergence 테이블까지 확장합니다.

### 6.3 `mvcc-index-activity.txt`

다음 테이블의 모든 index 사용량과 크기를 수집합니다.

- `source_observation_queue`
- `source_collection_checkpoints`
- `youtube_collection_job_leases`
- `source_observations`

`captured_at`과 `pg_stat_database.stats_reset`도 같이 기록합니다. 이 값이 없으면 `idx_scan=0`을
인덱스 제거 근거로 사용하면 안 됩니다.

### 6.4 `idle-transactions.txt`

현재 `idle in transaction` 및 aborted transaction 세션을 수집합니다.

- pid/user/application/client
- transaction 시작 시각
- idle 시작 시각
- transaction/idle age
- `backend_xmin` 및 age
- wait event와 backend type
- `query_id`

raw query text는 SQL literal이나 사용자 데이터를 포함할 수 있어 수집하지 않습니다. `query_id`로 동일 패턴을
식별하고, 필요한 경우 권한이 통제된 운영 세션에서 별도로 조사합니다.

artifact가 비어 있으면 해당 snapshot 시점에는 idle transaction이 없다는 뜻입니다.
출력이 있다고 즉시 프로세스를 종료하는 자동 gate로 만들지는 않았습니다. 배치/운영 세션의 의도를
사람이 확인해야 하기 때문입니다.

---

## 7. 운영 판정 기준

다음 수치는 자동 장애 기준이 아니라 조사 시작 기준입니다.

### 7.1 queue / lease autovacuum 강화 후보

아래 조건이 두 번 이상의 연속 snapshot에서 유지되면 table reloption migration을 검토합니다.

- `dead_tuple_pct > 10%`
- `n_dead_tup`이 계속 증가
- `last_autovacuum`이 갱신되지 않음 또는 autovacuum 이후에도 backlog가 회복되지 않음
- claim/acquire latency 또는 buffer read/write가 같이 악화

후보 설정:

```sql
ALTER TABLE source_observation_queue SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);

ALTER TABLE youtube_collection_job_leases SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.02,
    autovacuum_analyze_threshold = 50
);
```

작은 lease 테이블에서는 scale factor보다 threshold가 더 중요할 수 있습니다. update 빈도와 실제 row 수를
확인해 threshold 50이 과도한 VACUUM을 만들지 검증해야 합니다.

### 7.2 checkpoint fillfactor 후보

다음 조건이 모두 있을 때만 검토합니다.

- 충분한 `n_tup_upd`
- `hot_update_pct`가 지속적으로 낮음
- checkpoint relation의 dead tuple/size 증가
- indexed column 변경이 없다는 schema 재확인

후보 예시:

```sql
ALTER TABLE source_collection_checkpoints SET (fillfactor = 80);
```

설정만 적용하면 기존 page가 즉시 재배치되지 않습니다. 실제 효과를 보려면 이후 자연스러운 rewrite/update
과정이 필요하며, 강제 rewrite는 별도 유지보수 창에서 판단해야 합니다.

### 7.3 `idx_youtube_collection_job_due` 제거 후보

필수 조건:

- 동일한 `stats_reset` 구간에서 대표 부하 확보
- 연속 snapshot에서 `idx_scan=0` 또는 미미
- subject/global candidate EXPLAIN 확인
- 제거 전후 canary 비교 계획과 재생성 SQL 준비

재생성 SQL:

```sql
CREATE INDEX CONCURRENTLY idx_youtube_collection_job_due
    ON youtube_collection_job_leases (
        slot_state,
        next_due_at,
        retry_not_before,
        lease_expires_at,
        job_key
    );
```

---

## 8. 배포 및 검증 절차

### 8.1 배포 전 baseline

```bash
./scripts/runtime/pg-hotpath-explain-snapshot.sh \
  --output-dir artifacts/pg-hotpath-explain/mvcc-before \
  --stats-window-seconds 60
```

확인할 파일:

```text
mvcc-database-state.txt
dead-tuples-autovacuum.txt
mvcc-index-activity.txt
idle-transactions.txt
claim-statement-window.txt
alarm-dispatch-claim-explain.txt
youtube-outbox-claim-explain.txt
```

### 8.2 migration 적용

`182_postgres_idle_transaction_timeout.sql`이 적용되면 database catalog에 default가 저장됩니다.
현재 연결에는 소급되지 않습니다.

### 8.3 connection pool 순차 재기동

서비스 가용성을 유지하면서 다음 runtime의 pool을 순차 재생성합니다.

1. `hololive-api`
2. `hololive-alarm-worker`
3. central `youtube-collector`
4. host-native collector fleet

각 runtime 재기동 후 health check를 확인합니다.

### 8.4 적용 확인

새 session에서 다음 값이 `5min` 또는 300,000ms인지 확인합니다.

```sql
SHOW idle_in_transaction_session_timeout;

SELECT setting, unit
FROM pg_settings
WHERE name = 'idle_in_transaction_session_timeout';
```

다시 snapshot을 수집합니다.

```bash
./scripts/runtime/pg-hotpath-explain-snapshot.sh \
  --output-dir artifacts/pg-hotpath-explain/mvcc-after \
  --stats-window-seconds 60
```

### 8.5 장기 관측

단일 snapshot으로 인덱스를 제거하지 않습니다. 같은 `stats_reset` 구간에서 최소 수일간 다음을 비교합니다.

- queue/lease dead tuple 증가 속도
- checkpoint HOT 비율
- 넓은 due index의 `idx_scan`
- claim statement mean latency
- autovacuum 실행 간격
- frozen XID age
- idle transaction 재발 여부

---

## 9. 롤백

### 9.1 timeout rollback

```sql
DO $rollback$
BEGIN
    EXECUTE pg_catalog.format(
        'ALTER DATABASE %I RESET idle_in_transaction_session_timeout',
        pg_catalog.current_database()
    );
END
$rollback$;
```

RESET도 새 session부터 적용됩니다. rollback 후 connection pool을 순차 재기동합니다.

### 9.2 운영 snapshot 변경 rollback

snapshot 스크립트 변경은 read-only catalog query만 추가합니다. 데이터나 schema를 변경하지 않습니다.
문제가 있으면 관련 shell 파일만 이전 revision으로 되돌릴 수 있습니다.

---

## 10. 최종 판단

이번 리뷰의 핵심은 “VACUUM을 더 자주 돌린다”가 아닙니다.

1. 오래 열린 snapshot을 먼저 제한합니다.
2. UPDATE가 실제로 HOT인지 non-HOT인지 분리해서 봅니다.
3. index 사용량의 통계 기간을 함께 기록합니다.
4. append-only 원장과 상태 churn 테이블을 같은 방식으로 튜닝하지 않습니다.
5. 스키마 변경은 운영 증적을 확보한 뒤 작은 migration으로 분리합니다.

이 순서를 지키면 PostgreSQL MVCC의 비용을 숨기는 대신 측정하고, `hololive-bot`의 실제 workload에 맞게
안전하게 줄일 수 있습니다.
