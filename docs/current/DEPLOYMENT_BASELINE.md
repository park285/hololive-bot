# Deployment Baseline

## Scope

현재 production baseline은 단일 호스트 `deploy/compose/docker-compose.prod.yml`입니다. 이 문서는 runtime/infra 구성의 요약 기준이며, 실제 배포 절차는 `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`를 따릅니다.

## Non-Goals

- k8s/k3s 재도입 설계
- Docker Compose 절차 중복
- service env 전체 목록 복제

## Runtime Services

| Runtime | Compose service | Port | Env groups | Volumes | Depends on |
|---|---|---:|---|---|---|
| `bot` | `hololive-bot` | 30001 | app file log, Iris, cache, PostgreSQL, major event, cliproxy | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey, docker-proxy |
| `admin-api` | `hololive-admin-api` | 30006 | app file log, cache, PostgreSQL | `data`, `logs`, Valkey socket | PostgreSQL, migration, Valkey |
| `alarm-worker` | `hololive-alarm-worker` | 30007 | app file log, Iris, cache, PostgreSQL | `data`, `logs`, `runtime-config`, certs, Valkey socket | PostgreSQL, migration, Valkey |
| `llm-scheduler` | `llm-scheduler` | 30003 | app file log, cache, PostgreSQL, major event, cliproxy | `data`, `logs`, Valkey socket | PostgreSQL, migration, Valkey |
| `youtube-producer` | `youtube-producer` | 30005 | app file log, cache, PostgreSQL, scraper, major event, cliproxy | `data`, `logs`, Valkey socket | PostgreSQL, migration, Valkey |

## Infra Services

| Service | Purpose | Current notes |
|---|---|---|
| `holo-postgres` | Primary PostgreSQL | Bridge-networked; live-compat publishes `<tailnet-central>:5433` explicitly to container `5432`; TLS `ssl=on`; PKI server certificate rendered under `/run/hololive-bot/postgres-tls/` |
| `hololive-db-migrate` | Migration job | Runs before app services; uses `PGSSLMODE=verify-full` and `/run/hololive-bot/certs/postgres-ca.pem` |
| `valkey-cache` | Cache, queue, Pub/Sub | TCP and Unix socket, password required |
| `admin-dashboard` | Dashboard frontend | Port 30190, not part of Go runtime count |
| `docker-proxy` | Restricted Docker API proxy | Used instead of mounting the Docker socket directly |
| `deunhealth` | Autoheal sidecar | Restarts unhealthy labeled containers |

## External Boundaries

| Boundary | Used by | Contract doc |
|---|---|---|
| Iris / Redroid KakaoTalk automation | `bot`, `alarm-worker` | `contracts/iris-boundary.md` |
| PostgreSQL | Most runtime services | schema/migration files under `hololive/hololive-kakao-bot-go/scripts/migrations` |
| Valkey | cache, alarm queue, config Pub/Sub | `QUEUE_AND_PUBSUB_CONTRACTS.md` |
| CLIPROXY/OpenAI-compatible LLM proxy | `bot`, `llm-scheduler`, `youtube-producer` where configured | 검토 필요 |

## PostgreSQL TLS Baseline

Production PostgreSQL access is certificate-verified end to end. `holo-postgres`
loads an OpenBao PKI server certificate with the name/IP set
`holo-postgres`, `host.docker.internal`, `localhost`, `<tailnet-central>`, and
`127.0.0.1`; certificate TTL is `720h`. The central OpenBao Agent writes the
server material to `/run/hololive-bot/postgres-tls/` and sends `SIGHUP` to
`holo-postgres` after renewal.

The production client set uses `verify-full` with
`/run/hololive-bot/certs/postgres-ca.pem`: `bot`, `admin-api`, `alarm-worker`,
`llm-scheduler`, central `youtube-producer`, `youtube-producer-c`,
`hololive-db-migrate`, Seoul `youtube-producer-b`, and staged Osaka APs
`youtube-producer-a`/`youtube-producer-d` when they are rolled out.

Operational evidence from the 2026-06-07 transition showed all 35 TCP
PostgreSQL connections on TLSv1.3 and `0` plaintext TCP connections. One Unix
domain socket monitor connection remained outside the TCP TLS scope.

## Validation

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/ci-boundary-gate.sh
```

## Related Files

- `deploy/compose/docker-compose.prod.yml`
- `docs/current/PROJECT_MAP.md`
- `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
