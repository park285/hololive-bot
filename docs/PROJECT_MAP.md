# Project Map

Module inventory for the `hololive-bot` workspace.

| Module | Language | Path | Role | Port |
|--------|----------|------|------|------|
| `hololive-kakao-bot-go` | Go 1.26 | `hololive/hololive-kakao-bot-go/` | Main bot (webhook + command routing + admin API) | 30001 |
| `hololive-dispatcher-go` | Go 1.26 | `hololive/hololive-dispatcher-go/` | Alarm dispatch queue consumer (BRPOP → Iris) | 30020 |
| `hololive-llm-sched` | Go 1.26 | `hololive/hololive-llm-sched/` | LLM scheduler (major event + member news + delivery) | 30003 |
| `hololive-stream-ingester` | Go 1.26 | `hololive/hololive-stream-ingester/` | Sole ingestion owner for YouTube/Holodex/Chzzk/Twitch polling + stats | 30004 |
| `hololive-shared` | Go 1.26 | `hololive/hololive-shared/` | Shared Go library (hololive domain) | - |
| `shared-go` | Go 1.26 | `shared-go/` | Shared Go utilities (errors, stringutil, valkeyx, workerpool, etc.) | - |
| `docker-compose.prod.yml` | YAML | `docker-compose.prod.yml` | Production docker compose stack | - |

## Runtime Binaries (4)

| Binary | Module | Port |
|---|---|---|
| `bot` | `hololive-kakao-bot-go` | 30001 |
| `dispatcher-go` | `hololive-dispatcher-go` | 30020 |
| `llm-scheduler` | `hololive-llm-sched` | 30003 |
| `stream-ingester` | `hololive-stream-ingester` | 30004 |

## Maintenance
- Keep Go module entries aligned with `go.work`.
- Update roles/ports when service contracts change.
- Architecture: Go single-language runtime (4 binaries: bot + dispatcher-go + llm-scheduler + stream-ingester).
- Deployment baseline: Docker Compose (`docker-compose.prod.yml`) is the current production standard after the 2026-03-07 rollback from k8s/k3s.
- Retired runtime names: `admin-api`, `hololive-alarm`, `hololive-scraper`, `rust-dispatcher`, `hololive-admin`, `hololive-rs`.
