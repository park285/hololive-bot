# Runbook: admin-api

## Role

`hololive-admin-api`는 dashboard-facing admin HTTP control plane입니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30006/health` returns success |
| Ready | 검토 필요 |
| Logs | no repeated DB/cache/auth/trigger errors |
| Queue | does not consume dispatch queues |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | admin reads/writes fail |
| Valkey | yes | cache/session/config behavior degrades |
| `llm-scheduler` | partial | trigger operations fail |
| Alarm internal API handler | partial | alarm admin operations fail |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | HTTP port | yes |
| `LLM_SCHEDULER_INTERNAL_URL` | trigger provider | partial |
| `SERVICES_*_HEALTH_URL` | dashboard health aggregation | partial |

## Logs

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-admin-api
```

## Metrics

- 검토 필요.

## Common failure modes

### 1. Dashboard API unavailable

Symptoms:
- Dashboard calls fail.
- `hololive-admin-api` is unhealthy.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml ps hololive-admin-api
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 hololive-admin-api
curl http://127.0.0.1:30006/health
```

Mitigation:
- Check DB/Valkey health and auth/session configuration.

Rollback:
- Redeploy previous `hololive-admin-api` image/config using `rollback.md`.

### 2. Manual trigger fails

Symptoms:
- Major event/member news trigger endpoint returns failure.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 hololive-admin-api
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs --tail=200 llm-scheduler
```

Mitigation:
- Confirm `llm-scheduler` health and trigger contract compatibility.

Rollback:
- Roll back the changed trigger provider or consumer.

## Smoke test

```bash
curl http://127.0.0.1:30006/health
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `hololive-admin-api` image/config.
- Recheck dashboard health and trigger/alarm facade behavior after rollback.

## Related contracts

- `../contracts/trigger.md`
- `../contracts/alarm.md`
