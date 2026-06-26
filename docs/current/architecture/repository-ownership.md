# Repository Ownership

## Scope

이 문서는 shared repository/helper가 runtime ownership을 우회하지 않도록 data owner와 direct import 제한을 고정합니다. Cross-runtime 호출은 HTTP JSON, Valkey queue, Valkey Pub/Sub, Docker Compose 구조를 유지합니다.

## Data Ownership Matrix

| Data area | Owner | Direct writers | Allowed readers | Required access path |
|---|---|---|---|---|
| `major_event_subscriptions` | `hololive-api` (llm plane) | `hololive-api` (llm plane) | `hololive-api` (admin/bot planes) | internal HTTP contract `majorevent.subscription` |
| `membernews` state | `hololive-api` (llm plane) | `hololive-api` (llm plane) | `hololive-api` (bot plane) | internal HTTP contracts `membernews.subscription`, `membernews.digest` |
| alarm queue state | `alarm-worker` | `alarm-worker` | `alarm-worker`, observability consumers | queue contract `alarm.dispatch` or documented API |
| YouTube outbox/tracking | `youtube-producer` production, `alarm-worker` egress | `youtube-producer` writes rows; `alarm-worker` writes delivery/terminal state | observability consumers | `youtube-producer` writes rows, `alarm-worker` owns final send state |

Structured allowlist: `repository-ownership.allowlist`.

## Shared Infrastructure Ownership

- Runtime bootstrap owns env loading and passes typed config into shared infra helpers.
- `BuildInfraModule(ctx, cfg, logger)` accepts typed config and cleanup ownership remains with the returned module.
- Iris SDK env fallback in `ProvideIrisClient` is a documented compatibility exception for runtime Iris configuration; it must not be used as a pattern for database/cache ownership.
- Shared helpers must not silently override typed database, cache, or repository config from process env.

## Import Boundary Rules

- The `hololive-api` bot plane must not import `hololive-alarm-worker/internal`; cross-runtime access uses documented internal HTTP/queue contracts.
- `shared-go` must not import any `hololive/*` module.
- The `hololive-api` bot and admin planes must not import major event repository/storage internals directly; they use documented internal HTTP contracts.
- Shared data ownership changes must update `repository-ownership.allowlist`.

## YouTube Runtime Role Separation

| Runtime | Enabled role | Must stay disabled |
|---|---|---|
| `youtube-producer` | YouTube scraping/polling, `youtube_notification_outbox` production, and Holodex photo sync (a/c singleton lease) | Iris send, direct outbox dispatch |

Duplicated polling prevention is enforced operationally by Compose env ownership: `youtube-producer` owns `YOUTUBE_INGESTION_ENABLED=true`.
Duplicated sending prevention is enforced by code and architecture gates: `youtube-producer` and producer runtimes must not import `pkg/service/delivery` for proactive egress, call `delivery.NewIrisMessageSender`, call `outbox.NewDispatcher`, or start `OutboxDispatcher`.

## Validation

```bash
./scripts/architecture/check-repository-ownership.sh
./scripts/architecture/ci-boundary-gate.sh
```
