# Runbook: dispatcher-go

## Role

`dispatcher-go`는 `alarm:dispatch:queue`를 drain하고 Iris로 알림을 발송합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30020/ready` returns success |
| Ready | `http://127.0.0.1:30020/ready` |
| Logs | no repeated Iris send, retry, DLQ, or Valkey errors |
| Queue | active queue drains; retry/DLQ only grows for real failures |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | no | not the primary dependency |
| Valkey | yes | cannot consume queue or manage retry/DLQ |
| Iris | yes | send attempts fail and retry/DLQ may grow |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `DISPATCHER_PORT` | readiness port | yes |
| `ALARM_DISPATCH_QUEUE_KEY` | active queue key | yes |
| `ALARM_DISPATCH_MAX_BATCH` | drain batch size | yes |
| `IRIS_*` | Iris transport/certs/tokens | yes |

## Logs

```bash
docker compose -f docker-compose.prod.yml logs -f dispatcher-go
```

## Metrics

- Queue drain/retry/DLQ counters are defined under `hololive-shared/pkg/service/alarm/queue/metrics.go`.

## Common failure modes

### 1. Queue grows and dispatch stops

Symptoms:
- `alarm:dispatch:queue` length increases.
- No successful Iris sends in logs.

Diagnosis:
```bash
docker compose -f docker-compose.prod.yml logs --tail=300 dispatcher-go
docker compose -f docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock LLEN alarm:dispatch:queue
docker compose -f docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock ZCARD alarm:dispatch:retry
docker compose -f docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock LLEN alarm:dispatch:dlq
```

Mitigation:
- Check Iris connectivity, certs, and token config.
- Check Valkey connectivity and queue key configuration.

Rollback:
- Roll back dispatcher or Iris config changes. Preserve DLQ before replay.

### 2. DLQ grows

Symptoms:
- `alarm:dispatch:dlq` length increases.

Diagnosis:
```bash
docker compose -f docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock LRANGE alarm:dispatch:dlq 0 10
```

Mitigation:
- Follow `dlq-replay.md`.

Rollback:
- Do not replay until the producing contract or Iris failure is understood.

## Smoke test

```bash
curl http://127.0.0.1:30020/ready
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `dispatcher-go` image/config.
- Preserve DLQ/retry contents before replaying queue entries.

## Related contracts

- `../contracts/alarm.md`
- `../contracts/iris-boundary.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
