# Contract: settings

## Summary

Runtime settings changes go through the admin-plane settings HTTP API only. There is no settings Pub/Sub: the Valkey
`config:update` channel, its message types, publisher, and subscribers were removed (valkey dependency reduction 2nd
pass, 2026-09-28).

- `alarm_advance_minutes`: `POST /api/holo/settings` writes `settings.json` once, then applies to alarm-worker once via
  `PUT /internal/alarm/settings` (`alarm.http`). alarm-worker restores target minutes from `settings.json` at startup.
- ACL changes: admin ACL routes (`POST/DELETE /api/holo/rooms`, `POST /api/holo/rooms/acl`) write PostgreSQL and then
  the in-memory snapshot of the single process-owned `acl.Service`. The bot plane reads the same instance, so the
  change is visible to bot ingress decisions before the admin response returns; there is no reload or propagation
  step. `POST /api/holo/rooms/acl` with both `enabled` and `mode` applies them as two steps without a transaction: if
  the second step fails, the first may already be committed; a committed partial change is recorded as `acl_update`
  activity with `partial=true`. No Valkey ACL mirror exists. The ACL instance uses the bot pool and the bot-plane
  `KAKAO_ROOMS`/`KAKAO_ACL_*` seed (`app.buildAPIInfrastructure`).
- Member news run-now moved to the operator CLI (`trigger.manual`, `hololive-api --rerun-notification=membernews-weekly`).

## Contract ID

- `settings.update`

## Provider

- Service: `hololive-api` admin plane (`POST /api/holo/settings`)
- Runtime: `hololive-api`

## Consumers

- Services: `iris-console` (admin HTTP caller), `alarm-worker` (receives the apply `PUT`, reads `settings.json` at startup)
- Usage: alarm advance minutes

## Transport

- HTTP JSON (admin API → `hololive-api`; `hololive-api` → `alarm-worker` `PUT /internal/alarm/settings`)
- Shared `settings.json` file (`SETTINGS_DIR`, default `data/settings.json`) for restart restore

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Admin paths | `POST /api/holo/settings` |
| Worker apply | `PUT /internal/alarm/settings` (see `alarm.md`) |
| Persisted file | `settings.json` (`alarmAdvanceMinutes`, `targetMinutes`) |
| Contract package | `hololive/hololive-api/internal/server/settings` |

## Request

```json
{"alarmAdvanceMinutes": 7}
```

`alarmAdvanceMinutes` must be within `1..1440`. Zero is rejected before any file write or worker call; it is not
clamped or substituted. The settings update endpoint rejects unknown JSON fields before any persistence or
runtime apply, including mixed requests with a valid field plus the retired `scraperProxyEnabled` field.
Other admin request decoders retain their existing unknown-field policy.

## Response

`runtime` reports the worker apply result:

| Key | Meaning |
|---|---|
| `alarm_requested_advance_minutes` | requested minutes |
| `alarm_applied` | `true` only when the current worker response confirms application and target minutes |
| `alarm_reason` | distinguishes confirmed rejection from an unknown apply outcome |
| `alarm_target_minutes` | target minutes confirmed by this apply response; omitted on rejection or unknown outcome |

Persisting a candidate must succeed before the admin settings snapshot changes. A file-write failure preserves the
previous snapshot and file and does not call the worker. After a successful write, a lost response, transport error,
server error, or invalid success envelope cannot prove whether the worker applied the setting. Such outcomes use
`alarm_applied=false` and an explicit unknown-outcome reason within the existing response fields. The previous
successful target list is not presented as the worker's current state after an unknown outcome.
Iris Console displays `alarm_applied=false` with an `alarm worker outcome_unknown:` reason as an unconfirmed
application, preserving the distinction from a confirmed worker rejection.

같은 admin Handler의 설정 조회·변경은 공유 작업 gate를 사용합니다. 변경은 현재 값 조회·patch·파일 저장·worker 적용 순서를 함께 지키며, GET도 저장 뒤 적용 전의 두 상태를 섞어 반환하지 않습니다. 빈 `{}` 요청은 현재 값을 반환하고 파일 저장·worker 적용·변경 활동 기록을 수행하지 않습니다.

방 이름 설정의 활동 summary는 고정 문구이며, 방 ID와 이름은 구조화 필드에 기존 privacylog 토큰·가명으로 기록합니다. stdout의 운영 sanitizer와 파일 활동 로그 모두 원문 방 이름을 남기지 않습니다.

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| invalid request body | 400 | malformed JSON or unknown field | fix request |
| alarmAdvanceMinutes out of range | 400 | outside `1..1440`; no persistence or worker apply | fix request |
| Failed to get settings | 500 | the request was canceled or its budget expired while waiting for a consistent snapshot | surface the failed read; no stale success snapshot is returned |
| Failed to update settings | 500 | admission was canceled/timed out before persistence, or the `settings.json` write failed; worker not called | inspect the cause before re-submitting |
| `alarm_applied=false` | 200 | file written; worker application was rejected or could not be confirmed | inspect `alarm_reason`; an unknown outcome may already have changed the worker, so the response must not be interpreted as proof of its previous state |
| Failed to add room / Failed to remove room / Failed to set ACL enabled / Failed to set ACL mode | 500 | ACL route: that step's PostgreSQL write failed. For a combined `enabled`+`mode` request, `enabled` may already be applied | retry after fixing storage |

## Timeout and retry policy

- Timeout: admin settings GET/POST use the existing 10s admin request budget, including operation-gate waiting and worker apply. The worker client also includes its own apply-queue wait and HTTP exchange in its configured positive timeout (production: 10s); an earlier caller deadline wins. An explicit client timeout of zero retains the existing disabled-timeout contract.
- Retry: none. A rejected or unknown apply is reported in `runtime`, not retried; the saved file is not rolled back.
- Idempotency: re-submitting the same value is safe.

## Compatibility policy

- Response `runtime` keys are additive; removing a key is a contract change for the admin caller.

## Tests

- Single apply path: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_settings_update_test.go`
- Persistence failure before worker apply: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_settings_persistence_test.go`
- Concurrent save/apply, no-op updates and consistent GET snapshots: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_settings_order_test.go`
- Actual partial-file-write failure on Linux: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_settings_partial_write_linux_test.go`
- Total client apply budget and cancellation before dispatch: `hololive/hololive-shared/pkg/service/alarmhttp/client_advance_budget_test.go`
- Saved settings and unknown outcome after the worker applies but loses its response: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_settings_response_loss_test.go`
- Worker/client result classification and concurrent request ordering: `hololive/hololive-shared/pkg/service/alarmhttp/client_advance_test.go`, `hololive/hololive-alarm-worker/internal/service/alarm/subscriptions/client_advance_integration_test.go` (both run without build tags)
- Zero rejection before persistence/apply: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_settings_update_test.go`.
- Persist-before-publish and stored-file validation: `hololive/hololive-shared/pkg/service/settings/service_test.go`.
- ACL concurrent mutations and committed snapshot: `hololive/hololive-api/internal/service/acl/service_concurrency_test.go`
- Single process ACL instance: `hololive/hololive-api/internal/app/runtime_infrastructure_test.go` (needs `TEST_VALKEY_ADDR`)

## Known gaps

- `settings.json` is written before the worker apply; a rejected apply leaves file and worker diverged until the next
  successful apply or worker restart. With an unknown outcome, that divergence cannot be determined from the response.
