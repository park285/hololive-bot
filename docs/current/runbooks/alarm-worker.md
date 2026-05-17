# Runbook: alarm-worker

## Role

`hololive-alarm-worker`는 alarm checker/scheduler, dispatch queue publishing, dispatch queue consumption, generic notification delivery outbox consumption, YouTube outbox egress를 담당합니다.
또한 `ALARM_WORKER_EGRESS_LEASE_ENABLED=true`일 때 `notification:egress-owner:alarm-worker` lease를 잡은 인스턴스만 proactive notification egress를 시작합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30007/health` returns success |
| Ready | 검토 필요 |
| Logs | scheduler/checker loops run without repeated DB/cache errors |
| Queue | publishes to and consumes from `alarm:dispatch:queue` when alarm events are due |
| Delivery outbox | consumes `notification_delivery_outbox` rows for major event/member news proactive sends |
| Egress lease | `notification:egress-owner:alarm-worker` is held by the active alarm-worker when proactive egress is enabled |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | alarm state lookup fails |
| Valkey | yes | dispatch queue/cache/PubSub fail |
| Iris | yes | proactive notification egress |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | HTTP health port | yes |
| `NOTIFICATION_SCHEDULER_ROLE` | scheduler enablement | yes |
| `YOUTUBE_OUTBOX_DISPATCHER_ENABLED` | YouTube outbox egress enablement | production yes |
| `DELIVERY_DISPATCHER_ENABLED` | generic notification delivery outbox egress enablement | production yes |
| `ALARM_WORKER_EGRESS_LEASE_ENABLED` | single-owner proactive egress lease | production yes |
| `ALARM_DISPATCH_KARING_ENABLED` | alarm dispatch queue egress uses Karing content-list templates | production yes |
| `CACHE_*` | Valkey connection | yes |
| `POSTGRES_*` | DB connection | yes |

## Logs

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f hololive-alarm-worker
```

## Metrics

- Alarm checker/publisher metrics: 검토 필요.

## Common failure modes

### 1. Alarm queue stops growing despite due events

Symptoms:
- Expected alarms are not dispatched.
- YouTube outbox dispatcher has no new send errors.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=300 hololive-alarm-worker
./scripts/deploy/compose.sh -f docker-compose.prod.yml exec valkey-cache valkey-cli -s /var/run/valkey/valkey-cache.sock LLEN alarm:dispatch:queue
```

Mitigation:
- Check PostgreSQL, Valkey, scheduler role, and alarm state.
- Verify the egress lease is held by the active alarm-worker and not by a stale owner.

Rollback:
- Roll back the alarm-worker image/config that changed checker or queue publishing behavior.

### 2. Settings update not applied

Symptoms:
- Alarm advance minutes remains stale.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=200 hololive-alarm-worker
```

Mitigation:
- Verify `config:update` subscriber wiring and perform source-of-truth refresh if available.

Rollback:
- Roll back settings publisher/consumer change.

## Smoke test

```bash
curl http://127.0.0.1:30007/health
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `hololive-alarm-worker` image/config.
- Preserve and inspect `alarm:dispatch:*` queues before replaying or deleting queue data.

## Related contracts

- `../contracts/alarm.md`
- `../contracts/settings.md`
- `../QUEUE_AND_PUBSUB_CONTRACTS.md`
