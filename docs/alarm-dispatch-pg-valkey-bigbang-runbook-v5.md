# Alarm Dispatch PG + Valkey 하이브리드 빅뱅 전환 Runbook v5

기준 커밋: `a15f44730c38f69234c2d0ae173d793c2e30bc7e`

이 문서는 canary 없이 `pg_first/pg + Valkey wakeup` 구조를 한 번에 적용하는 빅뱅 전환 절차입니다. 빅뱅 전환은 점진 검증이 없기 때문에, 안전성은 다음 네 가지에 의존합니다.

1. 전환 직전 legacy Valkey queue residue를 없애거나 명시적으로 처리한다.
2. PostgreSQL integration gate가 실제 DB에서 성공했음을 확인한다.
3. publisher와 dispatcher를 같은 배포 창에서 함께 전환한다.
4. 전환 직후 문제가 생기면 “그냥 valkey_only로 되돌리는 것”이 아니라, PG ledger에 이미 들어간 delivery 상태를 먼저 확인한다.

명령은 repository root에서 실행합니다. 별도 언급이 없으면 production compose 기준은 다음입니다.

```bash
export COMPOSE='docker compose -f docker-compose.prod.yml'
export VALKEY='docker compose -f docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock'
```

Osaka overlay 환경에서는 운영 env에 맞게 `COMPOSE_ENV_FILE=./.env.osaka docker compose --env-file .env.osaka -f docker-compose.prod.yml -f docker-compose.osaka.yml ...` 형태로 치환합니다.

---

## 1. 빅뱅 전환의 핵심 원칙

기존 안전 전환 계획은 shadow → canary → gradual rollout이었습니다. 사용자가 canary 없이 즉시 전환하기로 결정했기 때문에, 계획은 다음처럼 바뀝니다.

```text
기존 점진 계획:
  shadow 관측
  작은 canary
  점진 확대
  production default

빅뱅 계획:
  발행 일시 정지
  legacy queue 완전 확인
  integration gate 확인
  env 동시 전환
  dispatcher PG mode 기동
  alarm-worker pg_first 기동
  즉시 관측
  필요 시 stop-the-line rollback
```

빅뱅에서는 “문제가 생기면 조금만 되돌린다”가 어렵습니다. 특히 `pg_first`로 publish된 row는 PG에 durable하게 남고, alarm-worker 쪽 dedupe mark도 이미 수행될 수 있습니다. 따라서 전환 후에는 PG pending row를 무시한 채 바로 legacy Valkey mode로 돌아가면 알림 유실 또는 중복 위험이 생길 수 있습니다.

---

## 2. 현재 설계 상태 요약

현재 최신 구조는 다음을 만족합니다.

```text
PG:
  alarm_dispatch_events
  alarm_dispatch_deliveries
  durable ledger / source of truth

Valkey:
  alarm:dispatch:wakeup
  payload 없는 wakeup token
  durable queue가 아님

publisher:
  ALARM_DISPATCH_PUBLISH_MODE=pg_first
  PG insert commit 후 wakeup token 발행

dispatcher:
  ALARM_DISPATCH_CONSUMER_MODE=pg
  wakeup 있으면 즉시 PG scan
  wakeup 없거나 Valkey 장애 시 fallback PG scan
```

현재 코드상 주요 rollout gate는 대부분 닫혀 있습니다.

```text
- dispatcher PG mode는 Compose에서 Valkey health에 묶이지 않음
- wakeup 전용 Valkey client 사용
- ALARM_DISPATCH_MAX_BATCH env override 가능
- recovery interval/batch size env override 가능
- /ready에서 wakeup_enabled / wakeup_connected / wakeup_degraded 분리
- PG consumer metric 추가
- PostgreSQL integration test workflow 추가
- ReleaseLeased는 workerID 조건으로 retry 전환
```

---

## 3. 절대 지켜야 할 불변식

### 3.1 PG가 source of truth입니다

전환 후 실제 dispatch 대상은 PG `alarm_dispatch_deliveries`입니다.

```text
Valkey에 wakeup token이 있음:
  PG를 보라는 신호일 뿐입니다.

Valkey wakeup token이 없음:
  알림이 없다는 뜻이 아닙니다.

PG delivery row가 있음:
  dispatch source of truth입니다.
```

### 3.2 Valkey에는 payload를 싣지 않습니다

`alarm:dispatch:wakeup` 값은 의미 없는 `"1"` token입니다. dispatcher는 token payload를 믿지 않고, 항상 PG를 scan합니다.

### 3.3 publisher와 dispatcher mode는 반드시 함께 바뀌어야 합니다

허용 조합은 다음뿐입니다.

```text
legacy:
  ALARM_DISPATCH_PUBLISH_MODE=valkey_only
  ALARM_DISPATCH_CONSUMER_MODE=valkey

shadow:
  ALARM_DISPATCH_PUBLISH_MODE=shadow
  ALARM_DISPATCH_CONSUMER_MODE=valkey

최종:
  ALARM_DISPATCH_PUBLISH_MODE=pg_first
  ALARM_DISPATCH_CONSUMER_MODE=pg
```

금지 조합입니다.

```text
publisher=pg_first, consumer=valkey
publisher=valkey_only/shadow, consumer=pg
```

### 3.4 전환 후 rollback은 단순하지 않습니다

`pg_first`로 이미 publish된 delivery가 있으면, alarm-worker가 해당 알림을 “published”로 mark했을 수 있습니다. 이때 PG pending row를 버리고 legacy Valkey mode로 돌아가면 dedupe 때문에 같은 알림이 다시 안 나갈 수 있습니다.

따라서 rollback은 반드시 다음 순서로 합니다.

```text
1. alarm-worker를 먼저 멈춤
2. PG delivery 상태 확인
3. PG pending/retry/sending 처리 방침 결정
4. 그 후 legacy mode로 되돌림
```

---

## 4. 빅뱅 전환 전 Hard Gate

아래 항목 중 하나라도 실패하면 전환하지 않습니다.

### 4.1 최신 커밋 확인

```bash
git rev-parse HEAD
```

기대값:

```text
a15f44730c38f69234c2d0ae173d793c2e30bc7e
```

또는 이 커밋 이후 동일 변경이 포함된 최신 HEAD여야 합니다.

### 4.2 PostgreSQL integration test 실제 실행

중요합니다. `TEST_DATABASE_URL`이 없어 skip된 성공은 통과가 아닙니다.

```bash
TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/dispatch_test?sslmode=disable \
  go test -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
```

성공 기준:

```text
PASS
```

실패 또는 skip이면 전환하지 않습니다.

### 4.3 migration 적용 확인

최소한 다음 migration이 적용되어야 합니다.

```text
058_create_alarm_dispatch_outbox.sql
059_harden_alarm_dispatch_outbox.sql
```

확인 SQL:

```sql
SELECT to_regclass('public.alarm_dispatch_events') AS events_table,
       to_regclass('public.alarm_dispatch_deliveries') AS deliveries_table,
       to_regclass('public.alarm_dispatch_admin_actions') AS admin_actions_table;

SELECT indexname
FROM pg_indexes
WHERE tablename IN (
  'alarm_dispatch_events',
  'alarm_dispatch_deliveries',
  'alarm_dispatch_admin_actions'
)
ORDER BY indexname;
```

확인해야 할 핵심 index:

```text
idx_alarm_dispatch_deliveries_due
idx_alarm_dispatch_deliveries_leased_expired
idx_alarm_dispatch_deliveries_sending_stale
idx_alarm_dispatch_deliveries_event_id
idx_alarm_dispatch_deliveries_room_created
idx_alarm_dispatch_deliveries_status_created
idx_alarm_dispatch_deliveries_sent_retention
idx_alarm_dispatch_deliveries_dlq_retention
idx_alarm_dispatch_deliveries_quarantined_retention
idx_alarm_dispatch_deliveries_cancelled_retention
idx_alarm_dispatch_events_created
idx_alarm_dispatch_admin_actions_delivery_created
```

### 4.4 legacy Valkey residue 확인

```bash
$VALKEY LLEN alarm:dispatch:queue
$VALKEY ZCARD alarm:dispatch:retry
$VALKEY LLEN alarm:dispatch:dlq
```

권장 기준:

```text
alarm:dispatch:queue = 0
alarm:dispatch:retry = 0
alarm:dispatch:dlq는 내용 확인 후 명시 처리
```

`retry`가 남아 있으면 빅뱅 전환을 보류하는 것이 원칙입니다. retry에 future schedule이 남아 있는데 dispatcher를 PG mode로 바꾸면 legacy retry가 더 이상 처리되지 않을 수 있습니다.

### 4.5 Iris transport/base URL 확인

```bash
printenv IRIS_TRANSPORT
cat runtime-config/iris_base_url
```

규칙:

```text
IRIS_TRANSPORT=h3/http2 -> https://...
IRIS_TRANSPORT=h2c      -> http://...
```

불일치하면 dispatcher readiness가 실패할 수 있습니다.

### 4.6 전환 env 확인

빅뱅 기본값:

```text
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
ALARM_DISPATCH_WAKEUP_ENABLED=true
ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=500
ALARM_DISPATCH_MAX_BATCH=50
ALARM_DISPATCH_PARALLELISM=2
ALARM_DISPATCH_MAX_BATCHES_PER_WAKE=20
ALARM_DISPATCH_POLL_INTERVAL_MS=1000
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_RECOVERY_INTERVAL_MS=30000
ALARM_DISPATCH_RECOVERY_BATCH_SIZE=100
```

첫 빅뱅에서는 `PARALLELISM=2`를 권장합니다. 안정화 후 4로 올립니다.

---

## 5. 빅뱅 배포 절차

### 5.1 현재 상태 스냅샷

```bash
date
git rev-parse HEAD
$COMPOSE ps
```

DB 상태:

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;
```

Valkey 상태:

```bash
$VALKEY LLEN alarm:dispatch:queue
$VALKEY ZCARD alarm:dispatch:retry
$VALKEY LLEN alarm:dispatch:dlq
```

### 5.2 alarm-worker 발행 정지

새 legacy Valkey publish가 들어오지 않게 alarm-worker를 먼저 멈춥니다.

```bash
$COMPOSE stop hololive-alarm-worker
```

확인:

```bash
$COMPOSE ps hololive-alarm-worker
```

### 5.3 legacy Valkey queue drain 확인

dispatcher가 아직 legacy `valkey` consumer mode라면 남은 active queue를 처리하도록 둡니다.

```bash
watch -n 2 "$VALKEY LLEN alarm:dispatch:queue; $VALKEY ZCARD alarm:dispatch:retry"
```

기준:

```text
LLEN alarm:dispatch:queue = 0
ZCARD alarm:dispatch:retry = 0
```

`retry`가 0이 되지 않으면 전환 전 명시적으로 결정해야 합니다.

```text
선택 A:
  retry가 자연 drain될 때까지 기다림

선택 B:
  retry/DLQ 내용을 운영자가 수동 처리

선택 C:
  유실/지연 위험을 승인하고 계속 진행
  단, 이 경우 runbook에 명시 기록
```

### 5.4 env 변경

`.env` 또는 배포 환경에 다음 값을 반영합니다.

```text
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
ALARM_DISPATCH_WAKEUP_ENABLED=true
ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=500
ALARM_DISPATCH_MAX_BATCH=50
ALARM_DISPATCH_PARALLELISM=2
ALARM_DISPATCH_MAX_BATCHES_PER_WAKE=20
ALARM_DISPATCH_POLL_INTERVAL_MS=1000
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_RECOVERY_INTERVAL_MS=30000
ALARM_DISPATCH_RECOVERY_BATCH_SIZE=100
```

### 5.5 migration 적용

```bash
$COMPOSE up --build hololive-db-migrate
```

실패하면 여기서 중단합니다. alarm-worker를 재시작하지 않습니다.

### 5.6 dispatcher-go를 PG mode로 기동

```bash
$COMPOSE up -d --build dispatcher-go
```

readiness 확인:

```bash
curl -s http://127.0.0.1:30020/ready | jq .
```

기대 응답:

```json
{
  "status": "ready",
  "dispatch_loop_running": true,
  "consumer_mode": "pg",
  "postgres_connected": true,
  "iris_connected": true,
  "wakeup_enabled": true,
  "wakeup_connected": true,
  "wakeup_degraded": false
}
```

Valkey wakeup이 장애라면 다음도 허용 가능합니다.

```json
{
  "status": "ready",
  "consumer_mode": "pg",
  "postgres_connected": true,
  "wakeup_enabled": true,
  "wakeup_connected": false,
  "wakeup_degraded": true
}
```

이 경우 latency는 fallback polling에 의존합니다. 하지만 PG mode의 correctness는 유지됩니다.

### 5.7 alarm-worker를 pg_first로 기동

```bash
$COMPOSE up -d --build hololive-alarm-worker
```

확인:

```bash
$COMPOSE logs -f hololive-alarm-worker dispatcher-go
```

---

## 6. 전환 직후 1시간 관측

### 6.1 5분 단위 SQL 상태 확인

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;
```

due backlog:

```sql
SELECT count(*)
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry')
  AND next_attempt_at <= NOW();
```

stale sending 후보:

```sql
SELECT count(*)
FROM alarm_dispatch_deliveries
WHERE status = 'sending'
  AND sending_started_at < NOW() - INTERVAL '60 seconds';
```

### 6.2 반드시 봐야 할 metric

publisher:

```text
alarm_dispatch_publish_requested_deliveries_total{mode="pg_first"}
alarm_dispatch_publish_processed_deliveries_total{mode="pg_first"}
alarm_dispatch_publish_inserted_deliveries_total{mode="pg_first"}
alarm_dispatch_publish_duplicate_deliveries_total{mode="pg_first"}
alarm_dispatch_publish_hash_conflict_total{mode="pg_first"}
alarm_dispatch_wakeup_sent_total
alarm_dispatch_wakeup_suppressed_total
alarm_dispatch_wakeup_failed_total
alarm_dispatch_wakeup_expire_failed_total
```

dispatcher PG:

```text
alarm_dispatch_pg_claimed_total
alarm_dispatch_pg_mark_sending_failed_total
alarm_dispatch_pg_mark_sent_failed_total
alarm_dispatch_pg_quarantined_total
alarm_dispatch_pg_dlq_total
alarm_dispatch_pg_retry_scheduled_total
```

recovery:

```text
alarm_dispatch_recovery_last_success_timestamp_seconds
alarm_dispatch_recovery_failed_total{type}
alarm_dispatch_recovery_rows_total{type}
```

### 6.3 위험 신호

즉시 확인해야 하는 상황입니다.

```text
hash conflict > 0
mark_sending_failed 증가
mark_sent_failed 증가
quarantined 예상 외 증가
pending/retry backlog가 계속 증가
dispatcher /ready not_ready
postgres_connected=false
iris_connected=false
```

---

## 7. 빅뱅 전환 후 장애 대응

### 7.1 publisher에서 PG insert 실패

증상:

```text
alarm-worker 로그에 insert pending outbox 실패
publish_hash_conflict 증가
processed_deliveries가 requested보다 낮음
```

대응:

```bash
$COMPOSE stop hololive-alarm-worker
```

그다음 원인을 확인합니다.

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status;
```

hash conflict라면 event_key/payload 설계 문제입니다. 즉시 legacy로 되돌리면 같은 알림 dedupe 상태 때문에 유실/중복 판단이 어려울 수 있습니다. 먼저 PG ledger 상태를 확인해야 합니다.

### 7.2 dispatcher가 PG row를 처리하지 못함

증상:

```text
pending/retry 증가
claimed 증가 없음
/ready not_ready
postgres_connected=false
```

대응:

```bash
curl -s http://127.0.0.1:30020/ready | jq .
$COMPOSE logs -f dispatcher-go
```

Postgres 문제면 dispatcher를 재시작하기 전 DB health를 먼저 확인합니다.

```bash
$COMPOSE ps holo-postgres
```

### 7.3 Iris send는 성공했지만 MarkSent 실패

증상:

```text
alarm_dispatch_pg_mark_sent_failed_total 증가
sending 증가
나중에 quarantined 증가
```

의미:

```text
Iris에는 이미 보냈을 수 있음.
retry하면 중복 발송 위험.
```

대응:

```text
quarantine 정책 유지
수동 재전송 금지
Iris idempotency 없는 상태에서는 자동 retry 금지
```

---

## 8. Rollback Runbook

### 8.1 Clean rollback: alarm-worker를 pg_first로 시작하기 전

이 단계에서는 아직 PG pending row가 새로 쌓이지 않았으므로 rollback이 쉽습니다.

```bash
$COMPOSE stop dispatcher-go
```

.env를 legacy로 되돌립니다.

```text
ALARM_DISPATCH_PUBLISH_MODE=valkey_only
ALARM_DISPATCH_CONSUMER_MODE=valkey
```

재시작:

```bash
$COMPOSE up -d --build dispatcher-go hololive-alarm-worker
```

### 8.2 Dirty rollback: pg_first publish가 이미 발생한 후

주의: 이 경우 단순 rollback은 위험합니다.

먼저 새 publish를 멈춥니다.

```bash
$COMPOSE stop hololive-alarm-worker
```

PG 상태 확인:

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;
```

판단 기준:

```text
sent:
  이미 발송됨

pending/retry:
  아직 발송 안 됐을 가능성이 큼
  하지만 alarm-worker dedupe는 이미 published로 mark되었을 수 있음

sending:
  결과 불명
  자동 retry 금지
  quarantine 또는 수동 확인 필요

quarantined:
  결과 불명
  수동 확인 필요
```

권장 dirty rollback 절차:

```text
1. alarm-worker 중지 유지
2. dispatcher PG mode를 가능한 한 유지해서 pending/retry를 drain
3. pending/retry가 0이 되면 dispatcher 중지
4. env를 valkey_only/valkey로 되돌림
5. dispatcher-go와 alarm-worker 재기동
```

만약 PG dispatcher가 장애라서 drain이 불가능하면:

```text
1. pending/retry/sending row를 상태별로 export
2. 운영자가 알림 지연/유실 가능성을 승인
3. 필요한 경우 admin requeue 또는 manual notification
4. 그 후 legacy mode 복구
```

절대 하지 말아야 할 것:

```text
PG pending/retry가 남아 있는데 무조건 legacy valkey_only로 되돌리고 끝내기
sending row를 retry로 되돌리기
quarantined row를 자동 재전송하기
```

---

## 9. 빅뱅 이후 안정화

첫 30분:

```text
ALARM_DISPATCH_PARALLELISM=2 유지
ALARM_DISPATCH_MAX_BATCH=50 유지
quarantined / mark_sent_failed 집중 관찰
```

1시간 안정 후:

```text
pending backlog가 안정적이면 PARALLELISM=4 검토
wakeup_degraded=false 유지 여부 확인
recovery_failed_total 증가 여부 확인
```

하루 안정 후:

```text
legacy Valkey active queue가 계속 0인지 확인
PG retention job 적용 여부 확인
DLQ/quarantine 운영 절차 정리
```

---

## 10. 최종 판단

빅뱅 전환은 canary보다 위험합니다. 하지만 현재 코드 상태에서는 다음 조건을 만족하면 즉시 전환이 가능할 정도로 rollout gate가 많이 닫혀 있습니다.

```text
- integration test 실제 성공
- legacy Valkey queue/retry residue 0
- migration 058/059 적용
- Iris URL/transport 정합성 확인
- dispatcher /ready PG mode ready
- 전환 후 metrics/SQL 관측 가능
- dirty rollback 절차 숙지
```

핵심은 하나입니다.

```text
빅뱅 전환 후 문제가 생기면 alarm-worker를 먼저 멈추고,
PG ledger 상태를 확인한 다음 rollback 여부를 결정합니다.
```

PG가 source of truth로 바뀐 순간부터는 Valkey legacy mode로 단순 복귀하는 것이 항상 안전하지 않습니다.

---

## 11. Close-out Checklist

전환 작업은 아래 기록이 남아야 닫습니다.

```text
실행 일시:
실행자:
기준 커밋:
적용 compose 파일:
적용 env 파일:
integration test 결과:
migration 058/059 확인 결과:
전환 전 legacy queue/retry/dlq 수:
dispatcher /ready 응답:
전환 후 5분 SQL 상태:
전환 후 30분 SQL 상태:
전환 후 60분 SQL 상태:
rollback 필요 여부:
잔여 조치:
```

닫힘 조건:

```text
- alarm-worker는 pg_first로 기동 중
- dispatcher-go는 pg consumer mode로 ready
- alarm_dispatch_deliveries due pending/retry가 지속 증가하지 않음
- sending stale 후보가 증가하지 않음
- hash conflict / mark_sending_failed / mark_sent_failed가 0 또는 원인 확인됨
- quarantined / dlq 증가는 운영자가 확인함
- legacy alarm:dispatch:queue와 alarm:dispatch:retry는 0
- dirty rollback 판단에 필요한 PG 상태 snapshot이 저장됨
```

위 조건을 만족하지 못하면 runbook을 닫지 않고, `7. 빅뱅 전환 후 장애 대응` 또는 `8. Rollback Runbook`으로 돌아갑니다.

---

## 12. Execution Record: 2026-05-13 KST

이번 전환은 중앙 production compose 기준으로 닫았습니다.

```text
실행 일시:
  2026-05-13 08:01:41 KST

실행자:
  Codex, 사용자님 승인 하에 실행

기준 커밋:
  a15f44730c38f69234c2d0ae173d793c2e30bc7e

적용 compose 파일:
  docker-compose.prod.yml

적용 env 파일:
  .env

적용 env 핵심값:
  ALARM_DISPATCH_PUBLISH_MODE=pg_first
  ALARM_DISPATCH_CONSUMER_MODE=pg
  ALARM_DISPATCH_WAKEUP_ENABLED=true
  ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=500
  ALARM_DISPATCH_MAX_BATCH=50
  ALARM_DISPATCH_PARALLELISM=2
  ALARM_DISPATCH_MAX_BATCHES_PER_WAKE=20
  ALARM_DISPATCH_POLL_INTERVAL_MS=1000
  ALARM_DISPATCH_LEASE_SECONDS=60
  ALARM_DISPATCH_RECOVERY_INTERVAL_MS=30000
  ALARM_DISPATCH_RECOVERY_BATCH_SIZE=100

integration test 결과:
  TEST_DATABASE_URL=<redacted> go test -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
  PASS

migration 058/059 확인 결과:
  hololive-db-migrate exit 0
  alarm_dispatch_events / alarm_dispatch_deliveries / alarm_dispatch_admin_actions 존재
  058/059 핵심 index 12개 확인

전환 직전 legacy queue/retry/dlq 수:
  alarm:dispatch:queue = 0
  alarm:dispatch:retry = 0
  alarm:dispatch:dlq = 0

배포 명령:
  docker compose --env-file .env -f docker-compose.prod.yml stop hololive-alarm-worker
  docker compose --env-file .env -f docker-compose.prod.yml up --build hololive-db-migrate
  docker compose --env-file .env -f docker-compose.prod.yml up -d --build dispatcher-go
  docker compose --env-file .env -f docker-compose.prod.yml up -d --build hololive-alarm-worker

배포 영향:
  dispatcher-go 재생성
  hololive-alarm-worker 재생성
  compose dependency에 의해 hololive-bot도 재생성됨

dispatcher /ready 응답:
  status=ready
  consumer_mode=pg
  dispatch_loop_running=true
  postgres_connected=true
  iris_connected=true
  wakeup_enabled=true
  wakeup_connected=true
  wakeup_degraded=false

전환 직후 SQL 상태:
  due pending/retry = 0
  stale sending = 0
  status별 active row 없음

전환 직후 Valkey 상태:
  alarm:dispatch:queue = 0
  alarm:dispatch:retry = 0
  alarm:dispatch:dlq = 0
  alarm:dispatch:wakeup = 0

전환 후 5분 관측:
  observed_at_utc=2026-05-12T23:08:23Z
  dispatcher /ready status=ready
  consumer_mode=pg
  wakeup_degraded=false
  alarm:dispatch:queue = 0
  alarm:dispatch:retry = 0
  alarm:dispatch:dlq = 0
  alarm:dispatch:wakeup = 0
  due pending/retry = 0
  stale sending = 0

runtime health:
  hololive-alarm-worker healthy
  hololive-dispatcher-go healthy
  hololive-kakao-bot-go healthy

fresh log 확인:
  hololive-alarm-worker: Cache store connected, postgres_pool_connected
  dispatcher-go: Cache store connected, postgres_pool_connected
  hololive-bot: Cache store connected, postgres_pool_connected
  fresh error marker 없음: ERR / panic / permission denied / x509 / no such file

metrics 확인:
  dispatcher-go /metrics: 404
  hololive-alarm-worker /metrics: 401
  따라서 이번 close-out의 fresh evidence는 /ready, docker health, SQL, Valkey, filtered logs 기준

rollback 필요 여부:
  불필요

잔여 조치:
  비차단 사후 관찰로 30분/60분 후 due pending/retry와 stale sending 재확인 권장
```

---

## 13. Follow-up Tuning Record: 2026-05-13 KST

초기 빅뱅 안정화 후 dispatcher 병렬도를 상향했습니다.

```text
실행 일시:
  2026-05-13 09:53:28 KST

변경:
  ALARM_DISPATCH_PARALLELISM=2 -> 4

적용 대상:
  dispatcher-go

적용 명령:
  docker compose --env-file .env -f docker-compose.prod.yml up -d --no-deps dispatcher-go

상향 전 확인:
  dispatcher-go healthy
  hololive-alarm-worker healthy
  hololive-kakao-bot-go healthy
  dispatcher /ready status=ready
  dispatcher fresh error marker 없음
  alarm-worker fresh error marker 없음
  alarm:dispatch:queue = 0
  alarm:dispatch:retry = 0
  alarm:dispatch:dlq = 0
  alarm:dispatch:wakeup = 0
  due pending/retry = 0
  stale sending = 0

상향 후 확인:
  dispatcher-go healthy
  dispatcher /ready status=ready
  consumer_mode=pg
  wakeup_degraded=false
  runtime env ALARM_DISPATCH_PARALLELISM=4
  fresh logs: Cache store connected, postgres_pool_connected
  fresh error marker 없음: ERR / WRN / panic / permission denied / x509 / no such file
  alarm:dispatch:queue = 0
  alarm:dispatch:retry = 0
  alarm:dispatch:dlq = 0
  alarm:dispatch:wakeup = 0
  due pending/retry = 0
  stale sending = 0

rollback 필요 여부:
  불필요
```
