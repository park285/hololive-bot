# Service: llm-scheduler

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-llm-sched` |
| Binary | `llm-scheduler` |
| Compose service | `llm-scheduler` |
| Port | `30003` |
| Health endpoint | `http://127.0.0.1:30003/health` |
| Ready endpoint | `http://127.0.0.1:30003/ready` |

## Role

Major event, member news, LLM scheduling, digest generation, and delivery orchestration을 담당합니다.

## Owns

- Major event subscription and notification scheduling
- Member news subscription and digest generation
- Internal trigger endpoints for scheduled notifications
- LLM summary cache and delivery orchestration

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| membernews | HTTP JSON | `/internal/membernews/*` | `bot` |
| majorevent | HTTP JSON | `/internal/majorevent/*` | `bot` |
| trigger | HTTP JSON | `/internal/trigger/*` | `admin-api` |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | subscriptions, summaries, outbox | scheduling and digest operations fail |
| Valkey | cache/config PubSub | stale settings or cache misses |
| Iris/cliproxy | external delivery/LLM where configured | delivery or summary generation degradation |

## Must not own

- Kakao webhook ingress
- Alarm checker runtime
- Dispatch queue consumption

## Startup requirements

- PostgreSQL and Valkey availability
- Internal API key configuration for protected routes
- CLIPROXY/LLM settings where enabled

## Shutdown behavior

- Stop HTTP server and scheduler workers gracefully.
- Preserve delivery/outbox state in PostgreSQL.

## Observability

- Logs: `docker compose -f docker-compose.prod.yml logs -f llm-scheduler`
- Health: `http://127.0.0.1:30003/health`
- Ready: `http://127.0.0.1:30003/ready`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/llm-scheduler.md`
