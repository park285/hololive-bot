# Canary Cutover Runbook

## 1. 사전 확인

```bash
TEST_DATABASE_URL=postgres://... go test -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox
```

이 명령이 실패하거나 실제 PostgreSQL 없이 skip되면 canary를 시작하지 않습니다.

```bash
valkey-cli LLEN alarm:dispatch:queue
valkey-cli ZCARD alarm:dispatch:retry
valkey-cli LLEN alarm:dispatch:dlq
```

모두 0이 아니면 drain 또는 account 절차를 먼저 수행합니다.
잔여물이 있으면 canary를 시작하지 않습니다. `shadowed` row는 관측 전용이며 `pending`으로 자동 승격하지 않습니다.

## 2. canary env

```text
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
ALARM_DISPATCH_WAKEUP_ENABLED=true
ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=1000
ALARM_DISPATCH_MAX_BATCH=50
ALARM_DISPATCH_PARALLELISM=2
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_POLL_INTERVAL_MS=1000
ALARM_DISPATCH_MAX_BATCHES_PER_WAKE=20
ALARM_DISPATCH_RECOVERY_INTERVAL_MS=30000
ALARM_DISPATCH_RECOVERY_BATCH_SIZE=100
```

## 3. 관측

- publisher batch duration
- requested/inserted/duplicate deliveries
- wakeup sent/suppressed/failed
- recovery last success/failed/rows
- claimed/sent/retry/dlq/quarantine
- PG pool wait
- pending backlog slope
- Iris latency/errors

## 4. 중단 조건

- pending backlog 지속 증가.
- quarantine 급증.
- publisher latency 급증.
- PG pool saturation.
- duplicate send report.
- MarkSent mismatch 급증.

## 5. 주의

shadowed row는 자동 pending 승격하지 않습니다. sending/quarantined row는 자동 replay하지 않습니다.
rollback 시 PG `pending/retry` row도 legacy Valkey queue로 자동 replay하지 않습니다. 필요한 경우 `scripts/runtime/alarm-dispatch-outbox-requeue.sh`로 duplicate risk ack와 audit를 남긴 뒤 제한적으로 처리합니다.
