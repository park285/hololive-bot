# Service: hololive-api

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-api` |
| Binary | `hololive-api` |
| Compose service | `hololive-api` |
| Port | `30001` (bot) / `30003` (llm) / `30006` (admin) |
| Health endpoint | `https://127.0.0.1:30001/health` through container `./bin/healthcheck` |
| Ready endpoint | `https://127.0.0.1:30003/internal/ready` through container `./bin/healthcheck --api-key-env API_SECRET_KEY` (llm plane dependencies) |

## Role

bot/admin/llm plane을 한 프로세스에서 호스팅하는 통합 runtime입니다.

- Bot plane: Kakao/Iris webhook ingress와 사용자 명령 routing, reply orchestration.
- LLM plane: major event/member news scheduling, LLM digest 생성, internal subscription/trigger 제공.
- Admin plane: dashboard-facing admin HTTP control plane, trigger client facade, alarm HTTP 호환 facade.

## Owns

- Kakao/Iris webhook ingress, user-facing command routing and reply orchestration (bot plane)
- Major event/member news subscription, digest generation, internal trigger endpoints, LLM summary cache and notification intent production (llm plane)
- Dashboard-facing admin HTTP API, operational trigger client facade, alarm HTTP compatibility facade during migration (admin plane)
- Bot-side clients for major event, member news, and alarm operations

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Iris webhook boundary | external HTTP/H3 | webhook/reply/send | Iris / Redroid |
| membernews | HTTP JSON | `/internal/membernews/*` | `hololive-api` (bot plane) |
| majorevent | HTTP JSON | `/internal/majorevent/*` | `hololive-api` (bot plane) |
| trigger | HTTP JSON | `/internal/trigger/*` | `hololive-api` (admin plane) |
| Admin HTTP API | HTTP JSON | 검토 필요 | `admin-dashboard` |
| settings.update | Valkey Pub/Sub | `config:update` | `hololive-api`, `alarm-worker`, `youtube-producer` |
| alarm HTTP compatibility | HTTP JSON | `/internal/alarm/*` | migration callers (target owner is `alarm-worker`) |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | command/domain/admin data, subscriptions, summaries, outbox | command/admin/scheduling failures, stale reads |
| Valkey | cache/config/session/coordination/PubSub | degraded command, admin, and cache behavior |
| Iris | KakaoTalk ingress/reply automation | webhook/reply delivery failure |
| cliproxy/LLM | external summary generation where configured | summary generation degradation |
| Alarm API | alarm CRUD/query | alarm commands and admin operations fail |

## Must not own

- Alarm checker/scheduler loops owned by `alarm-worker`
- Proactive alarm dispatch queue consumption owned by `alarm-worker`
- Proactive Iris/Kakao notification egress owned by `alarm-worker`

## Startup requirements

- Iris URL/cert/token configuration
- PostgreSQL and Valkey availability
- Internal API base URLs and key configuration for scheduler, trigger, and alarm services
- CLIPROXY/LLM settings where enabled

## Shutdown behavior

- Stop HTTP/H3 ingress and scheduler workers gracefully.
- Do not drain or mutate dispatch queues during shutdown.
- Preserve delivery/outbox state in PostgreSQL.

## Observability

- Logs: `./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f hololive-api`
- Health: `https://127.0.0.1:30001/health`, `https://127.0.0.1:30003/health`, `https://127.0.0.1:30006/health` through container `./bin/healthcheck`
- Ready: `https://127.0.0.1:30003/internal/ready` through container `./bin/healthcheck --api-key-env API_SECRET_KEY`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Process trust domain: `../architecture/hololive-api-trust-domain.md`
- Runbook: `../runbooks/hololive-api.md`
