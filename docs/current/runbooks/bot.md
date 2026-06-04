# Runbook: bot

## Role

`hololive-bot`는 Kakao/Iris webhook ingress와 사용자 명령 routing을 담당합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30001/health` returns success |
| Ready | 검토 필요 |
| Logs | no repeated webhook, Iris, DB, or Valkey errors |
| Queue | does not own dispatch queue draining |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | commands and reads fail |
| Valkey | yes | cache/config operations degrade |
| Iris | yes | Kakao ingress/reply fails |
| `llm-scheduler` | partial | membernews/majorevent commands fail |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `SERVER_PORT` | HTTP/H3 port | yes |
| `HOLOLIVE_HTTP_TRANSPORTS` | enabled transports | yes |
| `IRIS_*` | Iris URL/certs/tokens | yes |
| `LLM_SCHEDULER_INTERNAL_URL` | scheduler internal API | partial |

## Logs

```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f hololive-bot
```

## Metrics

- 검토 필요.

## Common failure modes

### 1. Health check fails

Symptoms:
- Compose marks `hololive-bot` unhealthy.
- Webhook replies stop.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml ps hololive-bot
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=200 hololive-bot
curl http://127.0.0.1:30001/health
```

Mitigation:
- Check PostgreSQL, Valkey, Iris env/cert availability.
- Redeploy only after confirming config is correct.

Rollback:
- Use `docs/current/runbooks/rollback.md` and redeploy the previous `hololive-bot` image/config.

### 2. Member news or major event commands fail

Symptoms:
- Command path returns scheduler/internal API errors.

Diagnosis:
```bash
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=200 hololive-bot
./scripts/deploy/compose.sh -f docker-compose.prod.yml logs --tail=200 llm-scheduler
curl http://127.0.0.1:30003/health
```

Mitigation:
- Validate `LLM_SCHEDULER_INTERNAL_URL` and scheduler health.

Rollback:
- Roll back the runtime that introduced the contract or config change.

## Smoke test

```bash
curl http://127.0.0.1:30001/health
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `hololive-bot` image/config.
- Recheck Iris webhook/reply and scheduler-dependent commands after rollback.

## Related contracts

- `../contracts/iris-boundary.md`
- `../contracts/membernews.md`
- `../contracts/majorevent.md`
- `../contracts/alarm.md`
