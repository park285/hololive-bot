# Service: bot

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-kakao-bot-go` |
| Binary | `bot` |
| Compose service | `hololive-bot` |
| Port | `30001` |
| Health endpoint | `https://127.0.0.1:30001/health` |
| Ready endpoint | 검토 필요 |

## Role

Kakao/Iris webhook ingress와 사용자 명령 routing을 담당하는 main bot runtime입니다.

## Owns

- Kakao/Iris webhook ingress
- User-facing command routing and reply orchestration
- Bot-side clients for major event, member news, and alarm operations

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Iris webhook boundary | external HTTP/H3 | 검토 필요 | Iris / Redroid |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | command and domain data | command failures, stale reads |
| Valkey | cache/config/coordination | degraded command and cache behavior |
| Iris | KakaoTalk ingress/reply automation | webhook/reply delivery failure |
| `llm-scheduler` | membernews/majorevent internal APIs | digest/subscription commands fail |
| Alarm API | alarm CRUD/query | alarm commands fail |

## Must not own

- Alarm checker/scheduler loops owned by `alarm-worker`
- Proactive alarm dispatch queue consumption owned by `alarm-worker`
- Dashboard control plane owned by `admin-api`

## Startup requirements

- Iris URL/cert/token configuration
- PostgreSQL and Valkey availability
- Internal API base URLs for scheduler and alarm services

## Shutdown behavior

- Stop HTTP/H3 ingress gracefully.
- Do not drain or mutate dispatch queues during bot shutdown.

## Observability

- Logs: `docker compose -f docker-compose.prod.yml logs -f hololive-bot`
- Health: `https://127.0.0.1:30001/health`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/bot.md`
