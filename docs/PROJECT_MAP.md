# Project Map

Module inventory for the `hololive-bot` workspace.

| Module | Language | Path | Role | Port |
|--------|----------|------|------|------|
| `hololive-kakao-bot-go` | Go 1.26 | `hololive/hololive-kakao-bot-go/` | Main bot (webhook + command routing) | 30001 |
| `hololive-admin` | Go 1.26 | `hololive/hololive-admin/` | Admin service | 30002 |
| `hololive-alarm` | Go 1.26 | `hololive/hololive-alarm/` | Alarm queue consumer + dispatcher (Go, 운영 중) | 30010 |
| `hololive-llm-sched` | Go 1.26 | `hololive/hololive-llm-sched/` | LLM scheduler (major event + member news + delivery) | 30003 |
| `hololive-stream-ingester` | Go 1.26 | `hololive/hololive-stream-ingester/` | YouTube/Holodex/Chzzk/Twitch polling + stats | 30004 |
| `hololive-shared` | Go 1.26 | `hololive/hololive-shared/` | Shared Go library (hololive domain) | - |
| `hololive-rs` | Rust nightly | `hololive/hololive-rs/` | RSS scraper + VTuber alarm checker + dispatcher (검증 완료, 20 crates) | - |
| `shared-go` | Go 1.26 | `shared-go/` | Shared Go utilities (errors, stringutil, valkeyx, workerpool, etc. 27 pkgs) | - |
| `k8s/` | YAML | `k8s/` | Kubernetes manifests (k3s) | - |

## Maintenance
- Keep Go module entries aligned with `go.work`.
- Update roles/ports when service contracts change.
- Architecture: hybrid (Rust=compute, Go=network). See `docs/GO_TO_RUST_MIGRATION_PROGRESS_20260301.md`.
