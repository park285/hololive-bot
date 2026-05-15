# 04. Risk Register

## R1. Post-send retry duplicates

### 원인

`MarkSending` 이후 `SendMessage` 실패를 retry로 처리하면, 실제 외부 전송이 성공했지만 응답만 실패한 경우 중복 알림이 발생할 수 있습니다.

### 영향

- 같은 방송 알림 중복 발송.
- 사용자 신뢰 저하.
- dedupe ledger가 이미 sent가 아닌 retry로 남아 상태 해석이 어려워짐.

### 대응

- `MarkSending` 이전 실패와 이후 실패를 분리합니다.
- PG consumer path에서 post-send failure는 `Quarantine`으로 보냅니다.
- Valkey legacy path는 기존 retry 정책을 유지합니다.
- `MarkDispatched` 실패는 retry하지 않습니다. row를 `sending`에 남겨 stale sending recovery가 quarantine하게 합니다.

### 검증

- Unit: `SendMessage` 실패가 `MarkSending` 이후 발생하면 `Quarantine` 호출.
- Integration: sending row가 `quarantined`로 이동.
- Metric: `alarm_dispatch_pg_quarantined_total` 증가.

## R2. Idle PG polling storm

### 원인

alarm-worker 내장 runner가 empty batch 후 25ms sleep으로 PG `ClaimDue`를 반복합니다.

### 영향

- idle 상태에서도 DB QPS 증가.
- DB CPU와 connection pool 잡음 증가.
- scraper/admin/webhook query와 간섭 가능.

### 대응

- PG mode에서 `alarm:dispatch:wakeup`을 `BRPOP`으로 기다립니다.
- wakeup timeout 시 adaptive backoff를 적용합니다.
- wakeup disabled/unavailable 시 `ALARM_DISPATCH_POLL_INTERVAL_MS` 기반 bounded polling으로 fallback합니다.

### 검증

- Unit: idle waiter wait 호출.
- Integration: empty 상태에서 claim QPS 감소.
- Metric: `alarm_dispatch_runner_empty_polls_total`, `alarm_dispatch_runner_idle_wait_seconds`.

## R3. Wakeup loss

### 원인

Valkey wakeup token TTL, Valkey 장애, token suppression, 네트워크 장애.

### 영향

- 알림 row는 PG에 있지만 consumer wakeup이 늦어짐.

### 대응

- wakeup은 최적화일 뿐 source of truth가 아닙니다.
- PG fallback polling이 반드시 due row를 claim해야 합니다.
- wakeup failure metric과 alert를 둡니다.

### 검증

- Valkey wakeup disabled 상태에서 PG row가 poll interval 내 처리되는지 확인.

## R4. Terminal table bloat

### 원인

`sent`, `dlq`, `quarantined`, `cancelled` row를 무기한 보관.

### 영향

- table/index bloat.
- autovacuum 부담.
- status count와 retention query 지연.
- backup/WAL 크기 증가.

### 대응

- retention cleanup을 maintenance runner 또는 운영 job으로 주기화합니다.
- PG advisory lock으로 단일 실행을 보장합니다.
- chunk limit으로 delete합니다.
- existing retention indexes를 사용합니다.

### 검증

- retention integration test.
- `pg_stat_user_tables`, table size, dead tuple 관측.

## R5. Mode mismatch

### 원인

publisher와 consumer mode가 서로 맞지 않음.

### 영향

- `pg_first/valkey`: PG pending row stranded.
- `shadow/pg`: observation row를 잘못 claim할 위험.
- empty publisher + pg consumer: consumer는 대기하지만 publisher가 PG에 안 씀.

### 대응

- 기존 mode pair validation 유지.
- runbook gate 강화.
- compose default와 runtime validation 동시 확인.

### 검증

- `pg_first+valkey` startup error.
- `pg+empty publisher` startup error.
- runbook에서 mode env dump 확인.

## R6. Recovery policy drift

### 원인

standalone dispatcher와 alarm-worker 내장 runner가 서로 다른 lease/recovery 설정을 씀.

### 영향

- 같은 환경변수인데 runtime별 복구 시간 차이.
- stale sending quarantine 지연 또는 과도한 quarantine.

### 대응

- alarm-worker 내장 PG consumer에도 lease, recovery interval, recovery batch size를 env에서 주입합니다.

### 검증

- unit test: env 값이 consumer option에 반영되는지.
- integration: expired leased recovery interval 동작.

## R7. Retention job itself causing load

### 원인

큰 delete batch, lock 없이 여러 runner 동시 실행, peak time cleanup.

### 영향

- DB lock wait.
- WAL spike.
- autovacuum pressure.
- alarm dispatch latency 증가.

### 대응

- advisory lock.
- chunk limit.
- query timeout.
- off-peak interval.
- one status at a time.
- 반복 delete는 다음 interval로 넘김.

### 검증

- retention limit 1000 이하.
- query timeout 설정.
- retention duration metric.

## R8. Observability blind spots

### 원인

count metric은 있지만 oldest age, idle wait, quarantine reason이 없음.

### 영향

- pending count가 작아도 오래된 알림을 놓칠 수 있음.
- delayed dispatch를 빠르게 인지하기 어려움.
- quarantine 원인 분석 지연.

### 대응

- backlog gauge 추가.
- oldest age gauge 추가.
- post-send quarantine reason metric 추가.
- alert rule 추가.

### 검증

- dashboard에 status count와 oldest age 표시.
- alert firing simulation.
