# Deployment Baseline

## Scope

현재 production baseline은 단일 호스트 `docker-compose.prod.yml`입니다. 이 문서는 runtime/infra 구성의 요약 기준이며, 실제 배포 절차는 `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`를 따릅니다.

## Non-Goals

- k8s/k3s 재도입 설계
- Docker Compose 절차 중복
- service env 전체 목록 복제

## Runtime Services

| Runtime | Compose service | Port | Env groups | Volumes | Depends on |
|---|---|---:|---|---|---|
| `bot` | `hololive-bot` | 30001 | app file log, Iris, cache, PostgreSQL, major event, cliproxy | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey, docker-proxy |
| `admin-api` | `hololive-admin-api` | 30006 | app file log, cache, PostgreSQL | `data`, `logs`, Valkey socket | PostgreSQL, migration, Valkey |
| `alarm-worker` | `hololive-alarm-worker` | 30007 | app file log, cache, PostgreSQL | `data`, `logs`, Valkey socket | PostgreSQL, migration, Valkey |
| `dispatcher-go` | `dispatcher-go` | 30020 | app file log, cache, Iris | `logs`, `runtime-config`, certs, Valkey socket | Valkey, `hololive-bot` health |
| `llm-scheduler` | `llm-scheduler` | 30003 | app file log, Iris, cache, PostgreSQL, major event, cliproxy | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey |
| `stream-ingester` | `stream-ingester` | 30004 | app file log, Iris, cache, PostgreSQL, scraper, major event, cliproxy | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey |
| `youtube-scraper` | `youtube-scraper` | 30005 | app file log, Iris, cache, PostgreSQL, scraper, major event, cliproxy | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey |

## Infra Services

| Service | Purpose | Current notes |
|---|---|---|
| `holo-postgres` | Primary PostgreSQL | Host-networked, port 5433 |
| `hololive-db-migrate` | Migration job | Must complete before app services |
| `valkey-cache` | Cache, queue, Pub/Sub | TCP and Unix socket, password required |
| `admin-dashboard` | Dashboard frontend | Port 30190, not part of Go runtime count |
| `docker-proxy` | Restricted Docker API proxy | Used instead of mounting the Docker socket directly |
| `deunhealth` | Autoheal sidecar | Restarts unhealthy labeled containers |

## External Boundaries

| Boundary | Used by | Contract doc |
|---|---|---|
| Iris / Redroid KakaoTalk automation | `bot`, `dispatcher-go`, ingestion runtimes where configured | `contracts/iris-boundary.md` |
| PostgreSQL | Most runtime services | schema/migration files under `hololive/hololive-kakao-bot-go/scripts/migrations` |
| Valkey | cache, alarm queue, config Pub/Sub | `QUEUE_AND_PUBSUB_CONTRACTS.md` |
| CLIPROXY/OpenAI-compatible LLM proxy | `bot`, `llm-scheduler`, ingestion runtimes where configured | 검토 필요 |

## Validation

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/ci-boundary-gate.sh
```

## Related Files

- `docker-compose.prod.yml`
- `docs/current/PROJECT_MAP.md`
- `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
