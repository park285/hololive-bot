# Hololive MSA 전체 비판 리뷰 및 최종 개선안 — 2026-06-25

## 목적

이 문서는 hololive-bot 전체 MSA를 운영 관점에서 다시 정리한 최종 개선안이다. 범위는 YouTube producer/scraper, YouTube outbox/delivery, community/shorts alarm-state claim, alarm dispatch outbox, alarm worker egress, PostgreSQL 운영 관측까지 포함한다.

중요한 결론은 하나다. 현재 시스템은 이미 단순 봇 구조가 아니라 `scrape -> normalize -> persist -> outbox -> room delivery -> alarm dispatch -> egress`로 분리된 이벤트 파이프라인이다. 따라서 더 이상 함수 단위 최적화가 아니라, 각 MSA 경계에서 다음 계약을 강제해야 한다.

1. 같은 외부 이벤트는 한 번만 canonical identity로 저장한다.
2. 한 post는 한 번만 발송 권한을 claim한다.
3. 한 delivery row는 한 worker만 처리한다.
4. 성공/실패 상태 반영은 row loop가 아니라 set-based SQL로 수행한다.
5. cleanup과 retention은 요청 hot path에서 분리한다.
6. 운영 검증은 로그가 아니라 `pg_stat_statements`, dead tuple, backlog, claim latency로 판단한다.

## 전체 MSA 흐름

현재 구조를 운영 경계 기준으로 보면 다음과 같다.

```text
YouTube producer
  - live/videos/shorts/community poller
  - channel route / global budget / job guard
  - canonical content identity 생성
  - tracking/outbox/persisted state 기록

YouTube outbox / delivery
  - youtube_notification_outbox claim
  - youtube_notification_delivery room fan-out
  - per-room delivery dispatch
  - community/shorts pre-send alarm-state claim
  - mark sent/retry/permanent failure

Tracking / alarm state
  - youtube_content_alarm_tracking
  - youtube_community_shorts_alarm_states
  - post-level sent-state and latency classification

Alarm MSA / alarm dispatch outbox
  - scheduled alarm check
  - alarm_dispatch_deliveries claim
  - karing/kakaolink egress
  - terminal state / DLQ / retention / maintenance

Runtime DB and observability
  - pool cap
  - hot table autovacuum
  - pg_stat_statements
  - dead tuple and backlog thresholds
```

이 중 가장 위험한 경계는 `delivery 성공 -> tracking/alarm state 반영`과 `발송 전 community/shorts claim`이다. 두 영역은 이미 #140, #141에서 각각 개선되었다.

- #140: `MarkAlarmSentBatch`를 set-based bulk SQL 경로로 전환했다.
- #141: `TryClaimAlarmState`를 `INSERT ... ON CONFLICT ... WHERE ... RETURNING` 기반 단일 원자 claim으로 전환했다.

이 문서는 그다음 운영 단계에서 어떤 부분을 계속 고정해야 하는지 설명한다.

## 현 상태 평가

### 잘 되어 있는 부분

#### 1. Delivery claim은 운영형 큐 패턴에 가깝다

`youtube_notification_delivery` claim은 pending row를 due 순서로 고르고 `FOR UPDATE SKIP LOCKED`로 worker 간 경합을 줄이는 구조다. 이 패턴은 다중 worker에서 blocking을 줄이고, 중복 처리를 피하는 방향으로 맞다.

최종 목표도 이 구조를 유지하는 것이다. `locked_at IS NULL` 전용 partial index를 추가하는 것은 성능상 유혹적이지만, claim/release 때마다 index membership이 흔들려 write amplification이 커질 수 있다. 현재처럼 `status='PENDING'` partial index로 두고, `locked_at` 조건은 filter로 유지하는 편이 안전하다.

#### 2. Delivery status update는 이미 batch화되어 있다

`MarkSentBatchIfLocked`, `MarkFailedRetryBatchIfLocked`, `MarkPermanentFailureBatchIfLocked`는 lock token 배열을 `unnest`로 전달해 한 번에 update한다. 이는 row 수만큼 DB round-trip이 발생하던 구조에서 벗어난 좋은 방향이다.

#### 3. Community/shorts alarm state가 post 단위 single-flight 역할을 한다

`youtube_community_shorts_alarm_states`는 `(kind, post_id)` primary key를 가지고, `authorized_at`을 claim token처럼 사용한다. 즉 room delivery가 여러 개여도 post 기준으로 실제 발송 권한은 하나만 잡도록 설계되어 있다. 이 구조는 알람 중복 발송 방지의 핵심이다.

#### 4. Alarm dispatch outbox가 별도 MSA 경계로 분리되어 있다

karing/kakaolink egress를 직접 요청 처리 경로에서 호출하지 않고 dispatch outbox와 worker로 분리한 점은 맞다. 외부 egress 지연, 재시도, DLQ, retention을 애플리케이션 핵심 경로와 분리할 수 있기 때문이다.

### 비판적으로 본 위험 지점

#### 위험 1. Claim 경계가 여러 단계로 나뉘면 race를 놓치기 쉽다

발송 전 claim에서 `INSERT/UPDATE` 후 다시 reload해서 확인하는 방식은 논리적으로 가능하지만, round-trip이 늘고, 아주 짧은 순간의 상태 변화를 별도 query로 재해석해야 한다. 그래서 #141에서 write statement의 `RETURNING` 결과로 claim 판정을 확정하도록 변경했다.

최종 원칙은 다음과 같다.

```text
상태를 바꾸는 SQL이 그 상태 변경의 성공/실패 판정도 같이 반환해야 한다.
```

즉 claim, release, terminal transition은 가능하면 `RETURNING` 또는 affected row count를 사용하고, 별도 reload는 관측/복구 경로로 제한해야 한다.

#### 위험 2. 성공 반영이 row-loop면 outbox 최적화가 무의미해진다

Delivery row 자체를 batch update해도, 그 뒤 tracking/alarm state 반영이 mark별 loop면 병목이 뒤로 이동한다. #140의 핵심은 이 병목을 제거한 것이다.

최종 원칙은 다음과 같다.

```text
batch로 dispatch된 결과는 batch로 상태 반영한다.
```

여기에는 `youtube_content_alarm_tracking`, `youtube_community_shorts_alarm_states`, latency classification, telemetry persistence가 모두 포함된다.

#### 위험 3. Scraper와 alarm outbox 사이의 identity contract가 흐려지면 중복 발송이 생긴다

YouTube는 community URL, short video ID, canonical post ID가 서로 다른 표기로 들어올 수 있다. 이 때문에 시스템 전체에서 다음 식별자를 엄격히 구분해야 한다.

```text
raw content id       : 외부 payload에서 들어온 원본 또는 준원본 값
content_id           : 내부 outbox/tracking join에 쓰는 normalized 값
canonical_content_id : post당 1회 발송 보장을 위한 canonical identity
post_id              : alarm state primary key로 쓰는 canonical identity
```

최종 목표는 producer/scraper 단계에서 `canonical_content_id`를 반드시 확정하고, downstream에서는 가능하면 raw fallback을 줄이는 것이다. raw fallback은 legacy repair와 migration window에만 허용해야 한다.

#### 위험 4. Cleanup/retention이 hot path에 섞이면 p95/p99가 흔들린다

MSA hot path에서 retention delete나 expired cleanup이 같이 실행되면 평균 latency보다 tail latency가 먼저 망가진다. Hololive MSA에서 cleanup은 다음 원칙을 가져야 한다.

```text
요청 처리 경로: claim, update, insert, egress에 필요한 최소 write만 수행
maintenance 경로: expired cleanup, retention delete, dead tuple 관리 수행
```

Alarm dispatch, YouTube telemetry, delivery terminal rows는 모두 retention 후보이다. 단, 삭제는 반드시 작은 batch로 수행하고 `ORDER BY retention key LIMIT N` 형태로 쪼개야 한다.

#### 위험 5. MSA별 pool cap을 합산해서 보지 않으면 max_connections를 잠식한다

서비스 하나의 `POSTGRES_POOL_MAX_CONNS`만 보면 안전해 보여도, producer, alarm worker, bot runtime, admin dashboard, maintenance job이 동시에 뜨면 총합이 커진다. 현재 pool cap을 둔 방향은 맞지만, 운영 판단은 개별 서비스가 아니라 합산 budget으로 해야 한다.

권장 상한은 다음 식으로 계산한다.

```text
postgres_reserved_connections
  >= producer_max_conns
   + alarm_worker_max_conns
   + kakao_bot_max_conns
   + admin_dashboard_max_conns
   + one_off_maintenance_max_conns
   + autovacuum/replication/admin reserve
```

## 최종 개선 목표 아키텍처

### 1단계: 이미 반영된 핵심 개선

#### #140 — sent-state bulk 반영

- `MarkAlarmSentBatch`를 row loop에서 bulk SQL로 전환한다.
- tracking update, alarm-state finalization, missing-state repair를 한 statement 안에서 처리한다.
- authorization mismatch가 있으면 transaction을 rollback한다.

#### #141 — pre-send claim 원자화

- `TryClaimAlarmState`를 `INSERT ... ON CONFLICT ... WHERE ... RETURNING`으로 정리한다.
- claim 성공 판정과 write를 하나의 SQL로 묶는다.
- 이미 sent인 row는 claim되지 않는다.

이 두 PR이 합쳐지면 community/shorts 중복 발송 방지 경계는 다음 구조가 된다.

```text
before send:
  claim row with authorized_at by single atomic write

after send success:
  finalize matching authorized_at by bulk set-based write

after stale/failure:
  release matching authorized_at only
```

### 2단계: Producer/scraper identity contract 고정

Producer/scraper 계층에서 해야 할 최종 작업은 다음이다.

1. community/shorts poller output에 `canonical_content_id`를 명시적으로 포함한다.
2. batchrepo 저장 시 `content_id`, `canonical_content_id`, `post_id` 매핑을 한 곳에서만 수행한다.
3. outbox payload에는 canonical id와 raw id를 모두 넣되, downstream primary decision은 canonical id만 사용한다.
4. raw fallback은 legacy repair용으로만 허용하고 metric을 남긴다.

관측 metric 후보는 다음이다.

```text
youtube_identity_raw_fallback_total{kind}
youtube_identity_canonical_mismatch_total{kind}
youtube_outbox_duplicate_suppressed_total{kind}
youtube_tracking_upsert_conflict_total{kind}
```

### 3단계: Alarm dispatch outbox terminal transition 표준화

Alarm dispatch outbox는 다음 transition만 허용해야 한다.

```text
PENDING -> IN_PROGRESS -> SENT
PENDING -> IN_PROGRESS -> RETRY_PENDING
PENDING -> IN_PROGRESS -> DLQ
PENDING -> CANCELLED
```

금지해야 할 transition은 다음이다.

```text
SENT -> PENDING
DLQ -> PENDING without explicit requeue reason
CANCELLED -> IN_PROGRESS
```

Repository layer에 transition guard를 두고, repository test에는 모든 허용/금지 transition matrix를 추가한다. 현재 기능이 동작하더라도 이 matrix가 없으면 새 egress가 추가될 때 상태 전이가 느슨해질 수 있다.

### 4단계: Cleanup/retention batch policy 표준화

모든 retention delete는 아래 구조로 맞춘다.

```sql
WITH picked AS (
    SELECT id
    FROM <table>
    WHERE <terminal timestamp> < now() - interval '<retention>'
    ORDER BY <terminal timestamp> ASC, id ASC
    LIMIT $1
)
DELETE FROM <table> t
USING picked p
WHERE t.id = p.id;
```

테이블별 권장 기준은 다음이다.

| 테이블 | retention key | 권장 batch |
|---|---|---:|
| youtube_notification_delivery | COALESCE(sent_at, created_at) | 500~2000 |
| youtube_notification_delivery_telemetry | logged_at | 500~2000 |
| alarm_dispatch_deliveries | terminal timestamp | 500~2000 |
| alarm_dispatch_events | created_at | 1000~5000 |

한 번에 많이 지우는 것보다 자주 조금씩 지우는 것이 PostgreSQL MVCC와 autovacuum에 유리하다.

### 5단계: SLO와 rollback 기준 확정

운영 SLO 후보는 다음이다.

```text
YouTube producer detection -> outbox insert p95 <= 5s
outbox pending backlog normal <= 1000 rows
outbox claim mean_exec_time <= 5ms
room delivery mark-sent batch mean_exec_time <= 10ms
community/shorts duplicate send count = 0
alarm dispatch pending backlog normal <= 1000 rows
alarm dispatch DLQ growth rate = 0 during normal operation
PostgreSQL active connections <= 80% of planned envelope
hot table n_dead_tup / n_live_tup <= 0.2 during normal traffic
```

Rollback 기준은 다음처럼 명확해야 한다.

```text
claim mismatch error 급증 -> #141 rollback 또는 feature flag로 old confirm path 재활성화
bulk mark sent error 급증 -> #140 rollback 또는 single-row fallback temporarily enable
backlog 급증 + DB CPU high -> worker concurrency down, producer poll rate down
external egress error 급증 -> alarm dispatch retry backoff up, DLQ threshold review
```

## 우선순위가 높은 후속 코드 PR

### A. Producer/scraper canonical identity hardening

대상 후보:

```text
hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers/community_poller.go
hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers/shorts_poller.go
hololive/hololive-shared/pkg/service/youtube/poller/internal/batchrepo/repository_batch_*.go
hololive/hololive-youtube-producer/internal/runtime/polling/*.go
```

작업:

- poller output struct에 canonical id를 명시한다.
- batchrepo에서 raw/canonical mismatch metric을 기록한다.
- outbox insert 전 canonical id empty를 hard error로 막는다.
- legacy raw fallback 발생 시 warning metric을 남긴다.

### B. Alarm dispatch transition matrix test

대상 후보:

```text
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_transitions.go
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_transitions_partial_test.go
```

작업:

- 허용 transition matrix를 table-driven test로 고정한다.
- 금지 transition이 rows affected 0인지 확인한다.
- requeue는 explicit reason이 있을 때만 허용한다.

### C. Maintenance SQL 표준화

대상 후보:

```text
scripts/maintenance/
docs/current/runbooks/
```

작업:

- hot path별 observability SQL 추가
- retention batch SQL 추가
- psql autocommit 필요 여부 명시
- pg_stat_statements snapshot 절차 문서화

### D. End-to-end duplicate alarm invariant test

대상 후보:

```text
hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch/*_test.go
hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation/*_test.go
```

작업:

- 같은 community/shorts post에 대해 여러 room delivery가 동시에 dispatch될 때 실제 sent mark가 하나만 canonical state로 확정되는지 검증한다.
- stale claim release 후 다른 worker가 claim할 수 있는지 검증한다.
- sent state가 이미 있으면 delivery가 already-sent 경로로 빠지는지 검증한다.

## 운영 검증 체크리스트

### 배포 전

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/tracking/internal/observation

go test ./hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch

go test ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox

go test ./hololive/hololive-youtube-producer/internal/runtime/...
```

### 배포 직후

```sql
SELECT
  calls,
  round(mean_exec_time::numeric, 3) AS mean_ms,
  round(max_exec_time::numeric, 3) AS max_ms,
  left(regexp_replace(query, '\s+', ' ', 'g'), 240) AS query
FROM pg_stat_statements
WHERE query ILIKE '%youtube_content_alarm_tracking%'
   OR query ILIKE '%youtube_community_shorts_alarm_states%'
   OR query ILIKE '%youtube_notification_delivery%'
   OR query ILIKE '%alarm_dispatch%'
ORDER BY total_exec_time DESC
LIMIT 30;
```

### 중복 발송 확인

```sql
SELECT
  kind,
  post_id,
  COUNT(*) AS rows,
  COUNT(*) FILTER (WHERE alarm_sent_at IS NOT NULL) AS sent_rows
FROM youtube_community_shorts_alarm_states
GROUP BY kind, post_id
HAVING COUNT(*) > 1
   OR COUNT(*) FILTER (WHERE alarm_sent_at IS NOT NULL) > 1;
```

정상 결과는 0 rows다.

### Claim stuck 확인

```sql
SELECT
  kind,
  COUNT(*) AS stuck_claims,
  MIN(authorized_at) AS oldest_authorized_at
FROM youtube_community_shorts_alarm_states
WHERE authorized_at IS NOT NULL
  AND alarm_sent_at IS NULL
  AND authorized_at < now() - interval '5 minutes'
GROUP BY kind
ORDER BY stuck_claims DESC;
```

정상 운영에서는 0 또는 매우 작은 수여야 한다.

### Delivery backlog 확인

```sql
SELECT
  status,
  COUNT(*) AS rows,
  MIN(next_attempt_at) AS oldest_next_attempt_at,
  MIN(created_at) AS oldest_created_at
FROM youtube_notification_delivery
GROUP BY status
ORDER BY rows DESC;
```

### Alarm dispatch backlog 확인

```sql
SELECT
  status,
  COUNT(*) AS rows,
  MIN(created_at) AS oldest_created_at
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY rows DESC;
```

## 최종 판단

현재 hololive-bot은 이미 기본적인 MSA 분리와 PostgreSQL 큐 패턴을 상당히 잘 갖추고 있다. 단, 완성도를 결정하는 지점은 다음 네 가지다.

1. canonical identity를 producer에서 확정하고 downstream에서 흔들지 않는 것
2. community/shorts 발송 권한을 post 기준으로 정확히 한 번만 claim하는 것
3. 성공/실패 반영을 row loop가 아니라 set-based SQL로 유지하는 것
4. cleanup과 retention을 hot path 밖으로 밀어내는 것

#140과 #141은 2번과 3번을 직접 강화했다. 이 문서의 후속 PR들은 1번과 4번, 그리고 alarm dispatch transition matrix를 고정하는 방향으로 진행하면 된다.
