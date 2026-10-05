# Repository Ownership

## Scope

이 문서는 shared repository/helper가 runtime ownership을 우회하지 않도록 data owner와 direct import 제한을 고정합니다. Cross-runtime 호출은 HTTP JSON, Valkey queue, Valkey Pub/Sub, Docker Compose 구조를 유지합니다.

## Data Ownership Matrix

| Data area | Owner | Direct writers | Allowed readers | Required access path |
|---|---|---|---|---|
| `major_event_subscriptions` | `hololive-api` (llm plane) | `hololive-api` (llm plane) | `hololive-api` (admin/bot planes) | internal HTTP contract `majorevent.subscription` |
| `membernews` state | `hololive-api` (llm plane) | `hololive-api` (llm plane) | `hololive-api` (bot plane) | internal HTTP contracts `membernews.subscription`, `membernews.digest` |
| alarm queue state | `alarm-worker` | `alarm-worker` | `alarm-worker`, observability consumers | queue contract `alarm.dispatch` or documented API |
| `alarm_state` (`alarms` table) | `alarm-worker` | `alarm-worker` | `hololive-api` | `alarm.state.read`: YouTube plane `notification_channel_ids.sql`, llm plane membernews read SQL. bot/admin plane은 필수 `ALARM_INTERNAL_URL`의 `alarm.http`를 사용하며 in-process 주입 분기는 정상 기동에서 도달 불가 |
| YouTube outbox/tracking | `hololive-api` YouTube plane production, `alarm-worker` egress | `hololive-api` writes rows; `alarm-worker` writes delivery/terminal state | observability consumers | `hololive-api` writes notification intent, `alarm-worker` owns final send state |

Structured table (doc-only, no gate reads it): `repository-ownership.allowlist`.

## Shared Infrastructure Ownership

- Runtime bootstrap owns env loading and passes typed config into shared infra helpers.
- `pkg/providers/modules.BuildInfraModule(ctx, InfraOptions{Valkey, Postgres}, logger)` accepts typed cache/DB config and cleanup ownership remains with the returned module.
- Iris SDK env fallback in `pkg/providers/iris.ProvideIrisClient` is a documented compatibility exception for runtime Iris configuration; it must not be used as a pattern for database/cache ownership.
- Shared helpers must not silently override typed database, cache, or repository config from process env.

## Import Boundary Rules

- The `hololive-api` bot plane must not import `hololive-alarm-worker/internal`; cross-runtime access uses documented internal HTTP/queue contracts.
- `shared-go` must not import any `hololive/*` module.
- The `hololive-api` bot and admin planes must not import major event repository/storage internals directly; they use documented internal HTTP contracts.
- `youtube-collector` must not import `pkg/service/alarm` or call `alarm.NewRepository`; `Repository`는 `Add`/`Remove`/`ClearByRoom` write 메서드를 함께 노출하므로 alarms를 읽어야 하는 read 전용 경로는 소유 plane의 SQL asset으로 직접 읽는다(예: YouTube plane `notification_channel_ids.sql`). `pkg/service/alarm/keys`는 제외 대상이 아니다.
- Shared data ownership changes must update `repository-ownership.allowlist`.

## YouTube Runtime Role Separation

| Runtime | Enabled role | Must stay disabled |
|---|---|---|
| `youtube-collector` | AP-fleet fetch/normalize/lease and observation Publish | Canonical persist, observation claim/finalize, Iris send, outbox dispatch |
| `hololive-api` YouTube plane | Observation consume, canonical persist, notification intent | External scraping, proactive egress |

Duplicated polling prevention is enforced by PostgreSQL collection leases. Each collector uses a slot-specific Stack Worker Profile v1 with `collection.executor.enabled=true`. Consume/canonical persist is owned by the `hololive-api` YouTube plane.
Duplicated sending prevention is enforced by code and architecture gates: `youtube-collector` must not import `pkg/service/delivery`, and the Iris proactive sender `NewIrisMessageSender` exists only in `alarm-worker/internal/egress`.
Canonical 저장 함수는 `hololive-api/internal/youtube/canonicalwrite`에 있으므로 collector에서 import할 수 없습니다.

YouTube outbox dispatcher와 범용 notification delivery dispatcher도 각각 `hololive-alarm-worker/internal/egress/{youtubedispatch,notificationdelivery}`에 있어 Go `internal/` 경계가 교차 module import를 거절합니다. 삭제된 dispatcher·batchrepo 이름의 재등장을 찾는 grep은 두지 않습니다. 반면 `pkg/service/delivery`의 producer enqueue 저장소·sender interface·Iris transport는 API llm plane producer와 worker consumer가 함께 쓰므로 collector의 직접 사용을 scoped gate로 제한합니다.

## Compiler and Gate Guarantees

- API canonical writer와 worker dispatcher의 module 소유권은 Go `internal/` 컴파일러 경계가 보장합니다.
- `pkg/service/delivery`, `ProvideIrisClient`, `iris.WithBaseURL`, `iris.WithBotToken`, `IrisClient:`는 합법적인 shared package 또는 SDK 표면이므로 compiler만으로 runtime capability ownership을 제한할 수 없습니다. scoped architecture gate가 직접 보장하며, worker 내부의 `NewIrisMessageSender` 사용 위치도 같은 gate가 `internal/{egress,app}`으로 제한합니다.
- `repository-ownership.allowlist`는 PostgreSQL table의 owner/writer/reader 선언이며 import allowlist가 아닙니다. table 접근은 `check-sql-ownership.py`, module import와 egress capability는 `check-repository-ownership.sh` 및 `ci-notification-egress-gate.sh`가 검증합니다.

## Validation

```bash
./scripts/architecture/check-repository-ownership.sh
./scripts/architecture/ci-boundary-gate.sh
```
