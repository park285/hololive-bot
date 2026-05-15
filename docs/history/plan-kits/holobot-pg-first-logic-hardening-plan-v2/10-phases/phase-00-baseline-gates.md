# Phase 00. Baseline Gates

## 목적

로직 개선 전 현재 운영 상태를 계량화하고, 계약이 바뀌지 않는다는 기준선을 세웁니다.

## Touch paths

직접 코드 변경 없음.

Read-only reference:

```text
docs/current/contracts/alarm.md
docs/current/contracts/valkey_ephemeral_contract.md
docker-compose.prod.yml
hololive/hololive-shared/pkg/service/alarm/queue/publisher.go
hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/*
hololive/hololive-alarm-worker/internal/app/*
```

## 해야 할 일

### 1. 계약 snapshot

다음을 기록합니다.

```bash
git diff -- docs/current/contracts/alarm.md
git diff -- docs/current/contracts/valkey_ephemeral_contract.md
git diff -- hololive/hololive-shared/pkg/contracts/alarm
```

기대값: diff 없음.

### 2. mode env snapshot

```bash
docker compose --env-file .env -f docker-compose.prod.yml config   | grep -E 'ALARM_DISPATCH_(PUBLISH_MODE|CONSUMER_MODE|WAKEUP|MAX_BATCH|POLL|LEASE|RECOVERY)'
```

확인할 값:

```text
ALARM_DISPATCH_PUBLISH_MODE=pg_first
ALARM_DISPATCH_CONSUMER_MODE=pg
ALARM_DISPATCH_WAKEUP_ENABLED=true
```

### 3. legacy Valkey residue 확인

```bash
valkey-cli LLEN alarm:dispatch:queue
valkey-cli ZCARD alarm:dispatch:retry
valkey-cli LLEN alarm:dispatch:dlq
```

원칙:

- residue가 있으면 cutover/logic deploy 전에 처리 방침을 문서화합니다.
- residue를 PG로 replay하지 않습니다.
- shadowed row를 pending으로 promote하지 않습니다.

### 4. PG status baseline

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;
```

### 5. oldest age baseline

```sql
SELECT
  status,
  EXTRACT(EPOCH FROM NOW() - MIN(next_attempt_at)) AS oldest_due_age_seconds
FROM alarm_dispatch_deliveries
WHERE status IN ('pending', 'retry')
GROUP BY status
ORDER BY status;
```

```sql
SELECT
  EXTRACT(EPOCH FROM NOW() - MIN(sending_started_at)) AS oldest_sending_age_seconds
FROM alarm_dispatch_deliveries
WHERE status = 'sending';
```

## 완료 기준

- 계약 diff가 없습니다.
- mode pair가 `pg_first/pg` 또는 계획된 단계와 일치합니다.
- legacy Valkey residue 정책이 정해졌습니다.
- PG status baseline이 기록되었습니다.
- pending/retry/sending oldest age baseline이 기록되었습니다.
