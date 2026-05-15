# Deploy Gate

## Before deploy

```bash
go test ./hololive/hololive-alarm-worker/internal/app -count=1
go test ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
go test ./hololive/hololive-dispatcher-go/internal/app -count=1
```

```bash
TEST_DATABASE_URL=postgres://... go test -tags=integration ./hololive/hololive-shared/pkg/service/alarm/dispatchoutbox -count=1
```

## Required SQL

```sql
SELECT status, count(*)
FROM alarm_dispatch_deliveries
GROUP BY status
ORDER BY status;
```

```sql
SELECT COALESCE(EXTRACT(EPOCH FROM NOW() - MIN(next_attempt_at)), 0)
FROM alarm_dispatch_deliveries
WHERE status = 'pending'
  AND next_attempt_at <= NOW();
```

## Required Valkey checks

```bash
valkey-cli LLEN alarm:dispatch:queue
valkey-cli ZCARD alarm:dispatch:retry
valkey-cli LLEN alarm:dispatch:dlq
```

## Required env checks

```bash
docker compose -f docker-compose.prod.yml config   | grep -E 'ALARM_DISPATCH_(PUBLISH_MODE|CONSUMER_MODE|WAKEUP|MAX_BATCH|POLL|LEASE|RECOVERY|RETENTION)'
```

## Stop conditions

Stop rollout if any of these occur.

- `alarm_dispatch_publish_hash_conflict_total` increases.
- `alarm_dispatch_pg_mark_sent_failed_total` increases.
- pending oldest age > 180 seconds.
- quarantine increases unexpectedly.
- legacy Valkey residue is non-zero and unaccounted.
- mode pair is forbidden.
