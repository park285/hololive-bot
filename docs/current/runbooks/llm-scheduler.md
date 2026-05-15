# Runbook: llm-scheduler

## Role

`llm-scheduler`는 major event/membernews scheduling, LLM digest generation, internal trigger APIs를 담당합니다.

## Normal status

| Check | Expected |
|---|---|
| Health | `http://127.0.0.1:30003/health` returns success |
| Ready | `http://127.0.0.1:30003/ready` returns success |
| Logs | no repeated DB/cache/LLM/trigger errors |
| Queue | `notification_delivery_outbox` rows are produced; alarm-worker drains them |

## Dependencies

| Dependency | Required | Failure impact |
|---|---|---|
| PostgreSQL | yes | subscriptions, summaries, outbox fail |
| Valkey | yes | cache and Pub/Sub behavior degrades |
| CLIPROXY | partial | LLM generation fails where enabled |

## Key environment variables

| Env | Purpose | Required |
|---|---|---|
| `LLM_SCHEDULER_PORT` | HTTP port | yes |
| `CLIPROXY_*` | LLM proxy | partial |
| `MAJOREVENT_*` | major event scrape/schedule config | partial |
| `DELIVERY_DISPATCHER_ENABLED=false` | producer-only egress boundary | yes |
| `CACHE_*`, `POSTGRES_*` | state dependencies | yes |

## Logs

```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml logs -f llm-scheduler
```

## Metrics

- 검토 필요.

## Common failure modes

### 1. Internal trigger returns 409

Symptoms:
- Admin trigger reports notification already in progress.

Diagnosis:
```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml logs --tail=300 llm-scheduler
```

Mitigation:
- Wait for active run to finish.
- Investigate stuck scheduler if conflict persists.

Rollback:
- Roll back trigger/scheduler changes only after preserving logs.

### 2. Digest generation fails

Symptoms:
- `bot` member news command fails.
- Provider logs show digest generation errors.

Diagnosis:
```bash
COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml logs --tail=300 llm-scheduler
COMPOSE_ENV_FILE=/run/hololive-bot/env docker compose --env-file /run/hololive-bot/env -f docker-compose.prod.yml logs --tail=100 hololive-bot
```

Mitigation:
- Check PostgreSQL, CLIPROXY, and member news source state.

Rollback:
- Roll back provider or prompt/config change that introduced failures.

## Smoke test

```bash
curl http://127.0.0.1:30003/health
curl http://127.0.0.1:30003/ready
```

## Rollback

- Use `docs/current/runbooks/rollback.md`.
- Redeploy the previous `llm-scheduler` image/config.
- Recheck membernews, majorevent, and trigger contract callers after rollback.

## Related contracts

- `../contracts/membernews.md`
- `../contracts/majorevent.md`
- `../contracts/trigger.md`
- `../contracts/settings.md`
