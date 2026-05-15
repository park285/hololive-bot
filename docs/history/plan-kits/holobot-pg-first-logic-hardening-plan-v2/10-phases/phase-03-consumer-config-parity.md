# Phase 03. PG Consumer Config Parity

## 목적

standalone dispatcher와 alarm-worker 내장 runner의 PG consumer 설정을 동일한 의미로 맞춥니다.

## 문제

standalone dispatcher는 lease, recovery interval, recovery batch size를 config에서 주입합니다. alarm-worker 내장 runner는 기본값에 의존합니다.

## 결정

alarm-worker 내장 PG consumer에도 동일 env를 주입합니다.

```text
ALARM_DISPATCH_LEASE_SECONDS=60
ALARM_DISPATCH_RECOVERY_INTERVAL_MS=30000
ALARM_DISPATCH_RECOVERY_BATCH_SIZE=100
ALARM_DISPATCH_MAX_BATCH=50
ALARM_DISPATCH_MAX_BATCHES_PER_WAKE=20
```

## Touch paths

```text
hololive/hololive-alarm-worker/internal/app/build_egress.go
hololive/hololive-alarm-worker/internal/app/env.go
docker-compose.prod.yml
```

## Compose defaults

```yaml
ALARM_DISPATCH_MAX_BATCH: ${ALARM_DISPATCH_MAX_BATCH:-50}
ALARM_DISPATCH_LEASE_SECONDS: ${ALARM_DISPATCH_LEASE_SECONDS:-60}
ALARM_DISPATCH_RECOVERY_INTERVAL_MS: ${ALARM_DISPATCH_RECOVERY_INTERVAL_MS:-30000}
ALARM_DISPATCH_RECOVERY_BATCH_SIZE: ${ALARM_DISPATCH_RECOVERY_BATCH_SIZE:-100}
ALARM_DISPATCH_MAX_BATCHES_PER_WAKE: ${ALARM_DISPATCH_MAX_BATCHES_PER_WAKE:-20}
```

## DB pool budget

alarm-worker는 여러 기능을 함께 들 수 있으므로 pool을 명시합니다.

```yaml
POSTGRES_POOL_MIN_CONNS: ${ALARM_WORKER_POSTGRES_POOL_MIN_CONNS:-1}
POSTGRES_POOL_MAX_CONNS: ${ALARM_WORKER_POSTGRES_POOL_MAX_CONNS:-8}
POSTGRES_POOL_MAX_IDLE_CONNS: ${ALARM_WORKER_POSTGRES_POOL_MAX_IDLE_CONNS:-4}
```

## 테스트

- env parsing test.
- build runner with pg mode test.
- invalid env fallback test.
- max batch and recovery option wiring test.

## 완료 기준

- standalone dispatcher와 alarm-worker 내장 runner가 같은 env 의미를 갖습니다.
- alarm-worker compose에 PG dispatch tuning env가 보입니다.
- 기본값으로도 안전하게 동작합니다.
