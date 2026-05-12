# Runbook: alarm-worker

## Role

`hololive-alarm-worker`는 alarm checker/scheduler와 dispatch queue publishing을 담당합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30007/health` returns success |
| Ready | 검토 필요 |
| Logs | scheduler/checker loops run without repeated DB/cache errors |
| Queue | publishes to `alarm:dispatch:queue` when alarm events are due |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | alarm state lookup fails |
| Valkey | yes | dispatch queue/cache/PubSub fail |
| Iris | no | send is owned by `dispatcher-go` |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | HTTP health port | yes |
| `NOTIFICATION_SCHEDULER_ROLE` | scheduler enablement | yes |
| `CACHE_*` | Valkey connection | yes |
| `POSTGRES_*` | DB connection | yes |

## Logs

```bash
docker compose -f docker-compose.prod.yml logs -f hololive-alarm-worker
```

## Metrics

- Alarm checker/publisher metrics: 검토 필요.

## Common failure modes

### 1. Alarm queue stops growing despite due events

Symptoms:
- Expected alarms are not dispatched.
- `dispatcher-go` has no new queue entries.

Diagnosis:
```bash
docker compose -f docker-compose.prod.yml logs --tail=300 hololive-alarm-worker
docker compose -f docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock LLEN alarm:dispatch:queue
```

Mitigation:
- Check PostgreSQL, Valkey, scheduler role, and alarm state.

Rollback:
- Roll back the alarm-worker image/config that changed checker or queue publishing behavior.

### 2. Settings update not applied

Symptoms:
- Alarm advance minutes remains stale.

Diagnosis:
```bash
docker compose -f docker-compose.prod.yml logs --tail=200 hololive-alarm-worker
```

Mitigation:
- Verify `config:update` subscriber wiring and perform source-of-truth refresh if available.

Rollback:
- Roll back settings publisher/consumer change.

## Smoke test

```bash
curl http://127.0.0.1:30007/health
```

## Related contracts

- `../contracts/alarm.md`
- `../contracts/settings.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
