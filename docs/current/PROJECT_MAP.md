# Project Map

Module and runtime inventory for the `hololive-bot` workspace.

## Module Inventory

| Module | Language | Path | Role | Port |
|--------|----------|------|------|------|
| `hololive-kakao-bot-go` | Go 1.26.3 | `hololive/hololive-kakao-bot-go/` | Main bot ingress (webhook + command routing) | 30001 |
| `hololive-admin-api` | Go 1.26.3 | `hololive/hololive-admin-api/` | Admin HTTP control plane | 30006 |
| `hololive-alarm-worker` | Go 1.26.3 | `hololive/hololive-alarm-worker/` | Alarm checker, dispatch queue consumer, and proactive egress worker | 30007 |
| `hololive-llm-sched` | Go 1.26.3 | `hololive/hololive-llm-sched/` | LLM scheduler (major event + member news + delivery) | 30003 |
| `hololive-youtube-producer` | Go 1.26.3 | `hololive/hololive-youtube-producer/` | YouTube producer AP runtime: primary/backfill polling, outbox production, active-active coordination, readiness, and Holodex photo sync on Osaka | 30005 |
| `hololive-shared` | Go 1.26.3 | `hololive/hololive-shared/` | Shared Go library (hololive domain, contracts, shared services) | - |
| `shared-go` | Go 1.26.3 | `../shared-go/` (iris-stack submodule) | Shared Go utilities | - |
| `docker-compose.prod.yml` | YAML | `docker-compose.prod.yml` | Production docker compose stack | - |

## Runtime Operations Inventory

| Runtime | Module | Binary | Compose service | Port | Health / Ready | Service doc | Runbook |
|---|---|---|---|---:|---|---|---|
| `bot` | `hololive-kakao-bot-go` | `bot` | `hololive-bot` | 30001 | `https://127.0.0.1:30001/health` | `services/bot.md` | `runbooks/bot.md` |
| `admin-api` | `hololive-admin-api` | `admin-api` | `hololive-admin-api` | 30006 | `http://127.0.0.1:30006/health` | `services/admin-api.md` | `runbooks/admin-api.md` |
| `alarm-worker` | `hololive-alarm-worker` | `alarm-worker` | `hololive-alarm-worker` | 30007 | `http://127.0.0.1:30007/health` | `services/alarm-worker.md` | `runbooks/alarm-worker.md` |
| `llm-scheduler` | `hololive-llm-sched` | `llm-scheduler` | `llm-scheduler` | 30003 | `http://127.0.0.1:30003/health` | `services/llm-scheduler.md` | `runbooks/llm-scheduler.md` |
| `youtube-producer` | `hololive-youtube-producer` | `youtube-producer` | `youtube-producer` | 30005 | `http://127.0.0.1:30005/health` | `services/youtube-producer.md` | `runbooks/youtube-producer.md` |

## Infra Services

| Compose service | Role | Notes |
|---|---|---|
| `holo-postgres` | PostgreSQL data store | Host-networked PostgreSQL on port 5433 |
| `hololive-db-migrate` | Migration bootstrap/apply job | Must complete before app runtime services start |
| `valkey-cache` | Valkey cache, queue, Pub/Sub | TCP and Unix socket endpoints |
| `admin-dashboard` | Dashboard frontend | Not part of the 5 Go runtime set |
| `docker-proxy` | Docker socket proxy | Used by bot operational endpoints |
| `deunhealth` | Container autoheal | Restarts unhealthy labeled containers |

## Cross-Runtime Contracts

- Contract map: `CONTRACT_MAP.md`
- Service ownership: `SERVICE_OWNERSHIP.md`
- Runtime runbook index: `runbooks/README.md`
- Deployment baseline: `DEPLOYMENT_BASELINE.md`
- YouTube notification split: `youtube-producer` owns producer AP responsibilities up to `youtube_notification_outbox`, active-active coordination/readiness, and Osaka Holodex photo sync; `alarm-worker` owns room resolution, rendering, retry, delivery rows, and Iris/Kakao egress.

## Maintenance

- Keep Go module entries aligned with `go.work`.
- Keep runtime binary and Docker Compose service entries aligned with `docker-compose.prod.yml`.
- Keep service docs and runbook links valid for all 5 runtime rows.
- Keep contract docs aligned with `hololive/hololive-shared/pkg/contracts/*`.
- Run `./scripts/architecture/check-project-map.sh` after changing `go.work`, module inventory, or repo-root docs references.
- Run `./scripts/architecture/check-runbook-coverage.sh` after changing runtime docs or runbook links.
- Run `./scripts/architecture/check-contract-map.sh` after changing contract docs or `hololive-shared/pkg/contracts/*`.
- Run `./scripts/architecture/ci-boundary-gate.sh` for architecture-wide changes.
- Architecture: Go single-language runtime (5 binaries: bot + admin-api + alarm-worker + llm-scheduler + youtube-producer).
- Deployment baseline: Docker Compose (`docker-compose.prod.yml`) is the current production standard after the 2026-03-07 rollback from k8s/k3s.
- Retired runtime names: `hololive-alarm`, `hololive-scraper`, `rust-dispatcher`, `hololive-admin`, `hololive-rs`.
