# Project Map

Module inventory for the `hololive-bot` workspace.

| Module | Language | Path | Role | Port |
|--------|----------|------|------|------|
| `hololive-kakao-bot-go` | Go 1.26.3 | `hololive/hololive-kakao-bot-go/` | Main bot ingress (webhook + command routing) | 30001 |
| `hololive-admin-api` | Go 1.26.3 | `hololive/hololive-admin-api/` | Admin HTTP control plane | 30006 |
| `hololive-alarm-worker` | Go 1.26.3 | `hololive/hololive-alarm-worker/` | Alarm checker / queue publisher worker | 30007 |
| `hololive-dispatcher-go` | Go 1.26.3 | `hololive/hololive-dispatcher-go/` | Alarm dispatch queue consumer (BRPOP → Iris) | 30020 |
| `hololive-llm-sched` | Go 1.26.3 | `hololive/hololive-llm-sched/` | LLM scheduler (major event + member news + delivery) | 30003 |
| `hololive-stream-ingester` | Go 1.26.3 | `hololive/hololive-stream-ingester/` | Photo sync + ingestion-adjacent runtime | 30004 |
| `youtube-scraper` | Go 1.26.3 | `hololive/hololive-stream-ingester/` | Dedicated YouTube scraping/polling + outbox runtime | 30005 |
| `hololive-shared` | Go 1.26.3 | `hololive/hololive-shared/` | Shared Go library (hololive domain) | - |
| `shared-go` | Go 1.26.3 | `shared-go/` | Shared Go utilities (errors, stringutil, valkeyx, workerpool, etc.) | - |
| `docker-compose.prod.yml` | YAML | `docker-compose.prod.yml` | Production docker compose stack | - |

## Runtime Binaries (7)

| Binary | Module | Port |
|---|---|---|
| `bot` | `hololive-kakao-bot-go` | 30001 |
| `admin-api` | `hololive-admin-api` | 30006 |
| `alarm-worker` | `hololive-alarm-worker` | 30007 |
| `dispatcher-go` | `hololive-dispatcher-go` | 30020 |
| `llm-scheduler` | `hololive-llm-sched` | 30003 |
| `stream-ingester` | `hololive-stream-ingester` | 30004 |
| `youtube-scraper` | `hololive-stream-ingester` | 30005 |

## Maintenance
- Keep Go module entries aligned with `go.work`.
- Root build/test commands should use the in-repo shared workspace at `shared-go/` and remain runnable from the repo root.
- Committed `go.work` must not reference developer-local sibling paths such as `../iris-client-go`; use a local, uncommitted workspace edit for that case.
- Keep runtime binary and Docker Compose service entries aligned with `docker-compose.prod.yml`.
- Update roles/ports when service contracts change.
- Architecture: Go single-language runtime (7 binaries: bot + admin-api + alarm-worker + dispatcher-go + llm-scheduler + stream-ingester + youtube-scraper).
- Deployment baseline: Docker Compose (`docker-compose.prod.yml`) is the current production standard after the 2026-03-07 rollback from k8s/k3s.
- Retired runtime names: `hololive-alarm`, `hololive-scraper`, `rust-dispatcher`, `hololive-admin`, `hololive-rs`.
- Run `./scripts/architecture/check-project-map.sh` after changing `go.work`, module inventory, or repo-root docs references.
