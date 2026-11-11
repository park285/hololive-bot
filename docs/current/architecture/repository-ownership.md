# Repository Ownership

## Scope

이 문서는 shared repository/helper가 runtime ownership을 우회하지 않도록 data owner와 direct import 제한을 고정합니다. Cross-runtime 호출은 HTTP JSON, PostgreSQL outbox, payload 없는 Valkey wakeup·Pub/Sub을 쓰고, 같은 process 안의 plane 사이는 in-process 주입 계약을 씁니다.

## Data Ownership Matrix

| Data area | Owner | Direct writers | Allowed readers | Required access path |
|---|---|---|---|---|
| alarm dispatch outbox (`alarm_dispatch_events`, `alarm_dispatch_deliveries`) | `alarm-worker` | `alarm-worker`; `hololive-api` admin plane은 운영자 조치(requeue·settle)로 격리·DLQ 행의 상태만 바꾸고 `alarm_dispatch_admin_actions`에 기록 | `alarm-worker`, `hololive-api` admin plane(`dispatchops` 조회), `hololive-api` bot plane(방송 이력이 세션에 없는 topic·thumbnail을 최신 LIVE event payload에서 보충), observability consumers | `alarm.dispatch` 계약(PostgreSQL dispatch outbox + payload 없는 Valkey wakeup `alarm:dispatch:wakeup`). admin 직접 접근은 `runbooks/admin-dispatch-operations.md` 절차로 한정 |
| `alarm_state` (`alarms` table) | `alarm-worker` | `alarm-worker` | `hololive-api` | `alarm.state.read`: YouTube plane `notification_channel_ids.sql`. bot/admin plane은 필수 `ALARM_INTERNAL_URL`의 `alarm.http`를 사용하며 in-process 주입 분기는 정상 기동에서 도달 불가 |
| YouTube outbox/tracking | `hololive-api` YouTube plane production, `alarm-worker` egress | `hololive-api` writes rows; `alarm-worker` writes delivery/terminal state | observability consumers | `hololive-api` writes notification intent, `alarm-worker` owns final send state |

Structured table (doc-only, no gate reads it): `repository-ownership.allowlist`.

## Shared Infrastructure Ownership

- Runtime bootstrap owns env loading and passes typed config into shared infra helpers.
- `pkg/providers/modules.BuildInfraModule(ctx, InfraOptions{Valkey, Postgres}, logger)` accepts typed cache/DB config and cleanup ownership remains with the returned module.
- Shared helpers must not silently override typed database, cache, or repository config from process env.

## Import Boundary Rules

- The `hololive-api` bot plane must not import `hololive-alarm-worker/internal`; cross-runtime access uses documented internal HTTP/queue contracts.
- `shared-go` must not import any `hololive/*` module.
- alarms 저장소 `alarmstore.Repository`(`Add`/`Remove`/`ClearByRoom` write 메서드 포함)는 `hololive-alarm-worker/internal/service/alarm/alarmstore`에 있어 Go `internal` 가시성 규칙상 다른 module이 import할 수 없다. alarms를 읽어야 하는 read 전용 경로는 소유 plane의 SQL asset으로 직접 읽는다(예: YouTube plane `notification_channel_ids.sql`).
- Shared data ownership changes must update `repository-ownership.allowlist`.

## YouTube Runtime Role Separation

| Runtime | Enabled role | Must stay disabled |
|---|---|---|
| `youtube-collector` | AP-fleet fetch/normalize/lease and observation Publish | Canonical persist, observation claim/finalize, Iris send, outbox dispatch |
| `hololive-api` YouTube plane | Observation consume, canonical persist, notification intent | External scraping, proactive egress |

Duplicated polling prevention is enforced by PostgreSQL collection leases. Each collector uses a slot-specific Stack Worker Profile v1 with `collection.executor.enabled=true`. Consume/canonical persist is owned by the `hololive-api` YouTube plane.
중복 발송은 dispatch 원장의 claim·fence·정산 동작으로 제어하며, Iris proactive sender는 `hololive-alarm-worker/internal/egress`가 소유합니다.
Canonical 저장 함수는 `hololive-api/internal/youtube/canonicalwrite`에 있으므로 collector에서 import할 수 없습니다.

YouTube outbox dispatcher도 `hololive-alarm-worker/internal/egress/youtubedispatch`에 있어 Go `internal/` 경계가 교차 module import를 거절합니다. `pkg/service/delivery`의 요청·결과 타입(`MessageVariants`, `SendResult`)과 Iris transport(`NewRuntimeIrisClient`)는 API와 worker가 함께 쓰고, sender interface(`PreparedMessageSender`)는 worker가 씁니다.

## Compiler and Gate Guarantees

- API canonical writer와 worker dispatcher의 module 소유권은 Go `internal/` 컴파일러 경계가 보장합니다.
- 공유 SDK 표면의 import 위치를 고정하는 문자열 gate는 두지 않습니다. 발송·저장 동작의 회귀는 해당 패키지 시험으로 확인합니다.
- `repository-ownership.allowlist`는 PostgreSQL table의 owner/writer/reader 선언이며 gate 입력이 아닙니다. `check-sql-ownership.py`는 SQL 안전 정책을, `check-repository-ownership.sh`는 API·template의 native 실행 권한 제한을 검사합니다.

## Validation

```bash
./scripts/architecture/check-repository-ownership.sh
./scripts/architecture/ci-boundary-gate.sh
```
