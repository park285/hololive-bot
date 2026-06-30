# Project Map

Module and runtime inventory for the `hololive-bot` workspace.

## Module Inventory

| Module | Language | Path | Role | Port |
|--------|----------|------|------|------|
| `hololive-alarm-worker` | Go 1.26 | `hololive/hololive-alarm-worker/` | Alarm checker, dispatch queue consumer, and proactive egress worker | 30007 |
| `hololive-api` | Go 1.26 | `hololive/hololive-api/` | Unified runtime hosting bot/admin/llm planes in one process | 30001/30003/30006 |
| `hololive-youtube-producer` | Go 1.26 | `hololive/hololive-youtube-producer/` | YouTube producer AP runtime: primary/backfill polling, outbox production, active-active coordination (Seoul b + main-host c + Osaka host-native a/d), readiness, and Holodex photo sync | 30005/30015/30025/30035 |
| `hololive-shared` | Go 1.26 | `hololive/hololive-shared/` | Shared Go library (hololive domain, contracts, shared services) | - |
| `shared-go` | Go 1.26 | `../shared-go/` (iris-stack submodule) | Shared Go utilities | - |
| `admin-dashboard-backend` | Go 1.26 | `admin-dashboard/backend/` | Admin dashboard Go backend (auth/session, holo API relay, Docker control, embedded frontend serving) | 30190 |
| `deploy/compose/docker-compose.prod.yml` | YAML | `deploy/compose/docker-compose.prod.yml` | Production docker compose stack | - |
| `deploy/compose/docker-compose.osaka.yml` | YAML | `deploy/compose/docker-compose.osaka.yml` | Osaka split-host AP overlay (`youtube-producer-a`, host `<tailnet-osaka-a>`) for compose-path contract validation; live runtime is host-native `systemd` | - |
| `deploy/compose/docker-compose.osaka2.yml` | YAML | `deploy/compose/docker-compose.osaka2.yml` | Osaka second split-host AP overlay (`youtube-producer-d`, host `<tailnet-osaka2-d>`) for compose-path contract validation; live runtime is host-native `systemd` | - |
| `deploy/compose/docker-compose.seoul.yml` | YAML | `deploy/compose/docker-compose.seoul.yml` | Seoul split-host active-active AP (`youtube-producer-b`) | - |
| `deploy/compose/docker-compose.main-ap.yml` | YAML | `deploy/compose/docker-compose.main-ap.yml` | Main-host active-active AP (`youtube-producer-c`, profile `main-ap`) | - |

## Runtime Operations Inventory

| Runtime | Module | Binary | Compose service | Port | Health / Ready | Service doc | Runbook |
|---|---|---|---|---:|---|---|---|
| `hololive-api` | `hololive-api` | `hololive-api` | `hololive-api` | 30001/30003/30006 | `https://127.0.0.1:30001/health` | `services/hololive-api.md` | `runbooks/hololive-api.md` |
| `alarm-worker` | `hololive-alarm-worker` | `alarm-worker` | `hololive-alarm-worker` | 30007 | `https://127.0.0.1:30007/health` | `services/alarm-worker.md` | `runbooks/alarm-worker.md` |
| `youtube-producer` | `hololive-youtube-producer` | `youtube-producer` | `youtube-producer` | 30005/30015/30025/30035 | `https://127.0.0.1:30025/health` (main `c`; 원격 AP는 각 호스트 로컬 H3 포트) | `services/youtube-producer.md` | `runbooks/youtube-producer.md` |

## Infra Services

| Compose service | Role | Notes |
|---|---|---|
| `holo-postgres` | PostgreSQL data store | Bridge-networked PostgreSQL; live-compat explicitly publishes `<tailnet-central>:5433` to container `5432`; `ssl=on`; OpenBao PKI server cert under `/run/hololive-bot/postgres-tls/` |
| `hololive-db-migrate` | Migration bootstrap/apply job | Must complete before app runtime services start; `PGSSLMODE=verify-full` with `postgres-ca.pem` |
| `valkey-cache` | Valkey cache, queue, Pub/Sub | TCP and Unix socket endpoints |
| `admin-dashboard` | Dashboard (Go backend + embedded frontend) | Not part of the 3 app runtime set |
| `docker-proxy` | Docker socket proxy | Used by `admin-dashboard` (Docker control) and `deunhealth` (autoheal). `hololive-api` is not granted access — it performs no Docker control |
| `deunhealth` | Container autoheal | Restarts unhealthy labeled containers |

## Cross-Runtime Contracts

- Contract map: `CONTRACT_MAP.md`
- Service ownership: `SERVICE_OWNERSHIP.md`
- Runtime runbook index: `runbooks/README.md`
- Deployment baseline: `DEPLOYMENT_BASELINE.md`
- YouTube notification split: `youtube-producer` owns producer AP responsibilities up to `youtube_notification_outbox`, active-active coordination/readiness, and Holodex photo sync (`c` singleton lease; `b` excluded); `alarm-worker` owns room resolution, rendering, retry, delivery rows, and Iris/Kakao egress.

## Maintenance

- Keep Go module entries aligned with `go.work`.
- Keep runtime binary and Docker Compose service entries aligned with `deploy/compose/docker-compose.prod.yml`.
- Keep service docs and runbook links valid for all 3 runtime rows.
- Keep contract docs aligned with `hololive/hololive-shared/pkg/contracts/*`.
- Run `./scripts/architecture/check-project-map.sh` after changing `go.work`, module inventory, or repo-root docs references.
- Run `./scripts/architecture/check-runbook-coverage.sh` after changing runtime docs or runbook links.
- Run `./scripts/architecture/check-contract-map.sh` after changing contract docs or `hololive-shared/pkg/contracts/*`.
- Run `./scripts/architecture/ci-boundary-gate.sh` for architecture-wide changes.
- Architecture: Go single-language runtime (3 app runtimes: hololive-api + alarm-worker + youtube-producer). `hololive-api` hosts the bot/admin/llm planes in one process on ports 30001/30003/30006.
- Central host default `docker compose up -d` starts `hololive-api` + `hololive-alarm-worker` only. `youtube-producer` is AP-owned and gated behind `COMPOSE_PROFILES=oracle` (central `c`) or the per-host AP overlays (`docker-compose.{osaka,osaka2,seoul,main-ap}.yml`); it is intentionally absent from a profile-less central `up`.
- Deployment baseline: Docker Compose (`deploy/compose/docker-compose.prod.yml`) is the current production standard after the 2026-03-07 rollback from k8s/k3s.
- Retired runtime names: `hololive-alarm`, `hololive-scraper`, `rust-dispatcher`, `hololive-admin`, `hololive-rs`.
