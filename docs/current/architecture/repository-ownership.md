# Repository Ownership

## Scope

이 문서는 shared repository/helper가 runtime ownership을 우회하지 않도록 data owner와 direct import 제한을 고정합니다. Cross-runtime 호출은 HTTP JSON, Valkey queue, Valkey Pub/Sub, Docker Compose 구조를 유지합니다.

## Data Ownership Matrix

| Data area | Owner | Direct writers | Allowed readers | Required access path |
|---|---|---|---|---|
| `major_event_subscriptions` | `hololive-api` (llm plane) | `hololive-api` (llm plane) | `hololive-api` (admin/bot planes) | internal HTTP contract `majorevent.subscription` |
| `membernews` state | `hololive-api` (llm plane) | `hololive-api` (llm plane) | `hololive-api` (bot plane) | internal HTTP contracts `membernews.subscription`, `membernews.digest` |
| alarm queue state | `alarm-worker` | `alarm-worker` | `alarm-worker`, observability consumers | queue contract `alarm.dispatch` or documented API |
| `alarm_state` (`alarms` table) | `alarm-worker` | `alarm-worker` | `hololive-api` | `hololive-api`는 `alarmread.Reader` 계약(`ProvideAlarmReader`)으로만 읽는다 |
| YouTube outbox/tracking | `hololive-api` YouTube plane production, `alarm-worker` egress | `hololive-api` writes rows; `alarm-worker` writes delivery/terminal state | observability consumers | `hololive-api` writes notification intent, `alarm-worker` owns final send state |

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
- `youtube-collector` must not import `pkg/service/alarm` or call `alarm.NewRepository`; `Repository`는 `Add`/`Remove`/`ClearByRoom` write 메서드를 함께 노출하므로 read 소비자는 `pkg/service/alarmread`의 `Reader`를 `ProvideAlarmReader`로 주입받는다. `pkg/service/alarm/keys`는 제외 대상이 아니다.
- Shared data ownership changes must update `repository-ownership.allowlist`.

## YouTube Runtime Role Separation

| Runtime | Enabled role | Must stay disabled |
|---|---|---|
| `youtube-collector` | AP-fleet fetch/normalize/lease and observation Publish | Canonical persist, observation claim/finalize, Iris send, outbox dispatch |
| `hololive-api` YouTube plane | Observation consume, canonical persist, notification intent | External scraping, proactive egress |

Duplicated polling prevention is enforced by PostgreSQL collection leases. Each collector uses a slot-specific Stack Worker Profile v1 with `collection.executor.enabled=true`. Consume/canonical persist is owned by the `hololive-api` YouTube plane.
Duplicated sending prevention is enforced by code and architecture gates: `youtube-collector` must not import `pkg/service/delivery` for proactive egress, call `delivery.NewIrisMessageSender`, call `outbox.NewDispatcher`, or start `OutboxDispatcher`.
Collector adapters must not import persist helpers (`batchrepo`, `PersistCommunityPosts`).

YouTube outbox dispatcher는 `hololive-alarm-worker/internal/egress/youtubedispatch`에 있으므로 다른 모듈에서 import 자체가 불가능합니다. 즉 이 항목의 1차 보장은 Go `internal/` 컴파일러이고, 게이트의 `outbox\.NewDispatcher`/`OutboxDispatcher` 심볼 denylist는 회귀 방지용 이중화로 유지합니다. 반면 `pkg/service/delivery`는 `hololive-api`(reactive reply)와 `alarm-worker`(proactive egress)의 진성 다중 소비자라 shared에 남으므로, 해당 항목은 게이트가 유일한 보장입니다.

## Compiler and Gate Guarantees

- `outbox.NewDispatcher`, `OutboxDispatcher`, `YouTube outbox dispatcher started`는 dispatcher가 alarm-worker `internal/`에 있으므로 compiler boundary가 1차 보장하고 textual denylist는 잘못된 재도입을 감지합니다.
- `pkg/service/delivery`, `NewIrisMessageSender`, `ProvideIrisClient`, `iris.WithBaseURL`, `iris.WithBotToken`, `IrisClient:`는 합법적인 shared package 또는 SDK 표면이므로 compiler만으로 runtime capability ownership을 제한할 수 없습니다. scoped architecture gate가 직접 보장합니다.
- `repository-ownership.allowlist`는 PostgreSQL table의 owner/writer/reader 선언이며 import allowlist가 아닙니다. table 접근은 `check-sql-ownership.py`, module import와 egress capability는 `check-repository-ownership.sh` 및 `ci-notification-egress-gate.sh`가 검증합니다.

## Validation

```bash
./scripts/architecture/check-repository-ownership.sh
./scripts/architecture/ci-boundary-gate.sh
```
