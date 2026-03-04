# Project Map

Module inventory for the `hololive-bot` workspace.

| Module | Language | Path | Role | Port |
|--------|----------|------|------|------|
| `hololive-kakao-bot-go` | Go 1.26 | `hololive/hololive-kakao-bot-go/` | Main bot (webhook + command routing) | 30001 |
| `hololive-admin` | Go 1.26 | `hololive/hololive-admin/` | Admin service | 30002 |
| `hololive-llm-sched` | Go 1.26 | `hololive/hololive-llm-sched/` | LLM scheduler (major event + member news + delivery) | 30003 |
| `hololive-stream-ingester` | Go 1.26 | `hololive/hololive-stream-ingester/` | YouTube/Holodex/Chzzk/Twitch polling + stats | 30004 |
| `hololive-shared` | Go 1.26 | `hololive/hololive-shared/` | Shared Go library (hololive domain) | - |
| `hololive-rs` | Rust nightly | `hololive/hololive-rs/` | RSS scraper + VTuber alarm checker + dispatcher (14 crates) | - |
| `shared-go` | Go 1.26 | `shared-go/` | Shared Go utilities (errors, stringutil, valkeyx, workerpool, etc.) | - |
| `k8s/` | YAML | `k8s/` | Kubernetes manifests (k3s) | - |

## Maintenance
- Keep Go module entries aligned with `go.work`.
- Keep Rust crate count aligned with `hololive-rs/Cargo.toml` workspace members.
- Update roles/ports when service contracts change.
- Architecture: hybrid (Rust=compute, Go=network). See `docs/GO_TO_RUST_MIGRATION_PROGRESS_20260301.md`.
