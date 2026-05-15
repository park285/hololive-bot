# Service: admin-api

## Runtime identity

| Field | Value |
|---|---|
| Module | `hololive-admin-api` |
| Binary | `admin-api` |
| Compose service | `hololive-admin-api` |
| Port | `30006` |
| Health endpoint | `http://127.0.0.1:30006/health` |
| Ready endpoint | 검토 필요 |

## Role

Admin dashboard와 운영자용 HTTP control plane을 담당합니다.

## Owns

- Dashboard-facing admin HTTP API
- Operational trigger client facade
- Admin read/write orchestration over shared domain services

## Provides

| Contract | Type | Path/Event/Queue | Consumers |
|---|---|---|---|
| Admin HTTP API | HTTP JSON | 검토 필요 | `admin-dashboard` |

## Consumes

| Dependency | Purpose | Failure impact |
|---|---|---|
| PostgreSQL | admin reads/writes | dashboard operations fail |
| Valkey | cache/config state | degraded admin behavior |
| `llm-scheduler` | trigger/membernews/majorevent operations | trigger operations fail |
| Alarm API | alarm management | alarm admin operations fail |

## Must not own

- Kakao webhook ingress
- Long-running alarm checker loops
- Queue dispatch to Iris

## Startup requirements

- PostgreSQL and Valkey availability
- `LLM_SCHEDULER_INTERNAL_URL`
- Runtime config and secrets from Compose env file

## Shutdown behavior

- Stop HTTP server gracefully.
- Leave background scheduling ownership to dedicated runtimes.

## Observability

- Logs: `./scripts/deploy/compose.sh -f docker-compose.prod.yml logs -f hololive-admin-api`
- Health: `http://127.0.0.1:30006/health`
- Metrics: 검토 필요

## Related documents

- Project Map: `../PROJECT_MAP.md`
- Contract Map: `../CONTRACT_MAP.md`
- Runbook: `../runbooks/admin-api.md`
