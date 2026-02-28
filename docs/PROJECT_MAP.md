# Project Map

Module inventory for the `hololive-bot` workspace.

| Module | Language | Path | Role | Port |
|--------|----------|------|------|------|
| `hololive-kakao-bot-go` | Go 1.26 | `hololive/hololive-kakao-bot-go/` | Main bot (webhook + scheduler + ingestion) | 30001/30002/30010 |
| `hololive-admin` | Go 1.26 | `hololive/hololive-admin/` | Admin service | - |
| `hololive-alarm` | Go 1.26 | `hololive/hololive-alarm/` | Alarm dispatcher service | - |
| `hololive-llm-sched` | Go 1.26 | `hololive/hololive-llm-sched/` | LLM-based scheduler | - |
| `hololive-stream-ingester` | Go 1.26 | `hololive/hololive-stream-ingester/` | Stream data ingestion | - |
| `hololive-shared` | Go 1.26 | `hololive/hololive-shared/` | Shared Go library (hololive domain) | - |
| `hololive-scraper-rs` | Rust nightly | `hololive/hololive-scraper-rs/` | RSS scraper + VTuber alarm system | - |
| `shared-go` | Go 1.26 | `shared-go/` | Shared Go utilities (errors, stringutil, valkeyx, workerpool, etc. 27 pkgs) | - |
| `k8s/` | YAML | `k8s/` | Kubernetes manifests (k3s) | - |

## Maintenance
- Keep Go module entries aligned with `go.work`.
- Update roles/ports when service contracts change.
