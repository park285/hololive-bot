# Contract: settings

## Summary

Runtime settings changes go through the admin-plane settings HTTP API only. There is no settings Pub/Sub: the Valkey
`config:update` channel, its message types, publisher, and subscribers were removed (valkey dependency reduction 2nd
pass, 2026-09-28).

- `alarm_advance_minutes`: `POST /api/holo/settings` writes `settings.json` once, then applies to alarm-worker once via
  `PUT /internal/alarm/settings` (`alarm.http`). alarm-worker restores target minutes from `settings.json` at startup.
- `membernews_weekly_run_now`: `POST /api/holo/settings/llm` calls the llm scheduler trigger API directly
  (`trigger.manual`); `membernews_weekly_run_now` is only the response `runtime` / activity-log key.
- ACL changes: admin ACL routes (`POST/DELETE /api/holo/rooms`, `POST /api/holo/rooms/acl`) write PostgreSQL, then the
  bot-plane ACL instance in the same `hololive-api` process reloads from PostgreSQL before the admin response returns
  (`acl.Service.Follow`; overlapping reloads are serialized so the last applied snapshot reflects every commit). The
  in-process notification cannot be lost, but the reload (a PostgreSQL read) can fail: the admin route then answers
  `500 acl_bot_resync_failed` and the bot keeps its previous snapshot until the next successful reload. The requested
  change may already be committed (first request) or may have needed no write (duplicate/retry request). Re-sending the
  same request retries the resync without writing parts that are already applied and returns the original outcome (200
  no-op for an unchanged `enabled`/`mode`, 409 for an existing room, 404 for a missing room). `POST /api/holo/rooms/acl`
  with both `enabled` and `mode` applies them as two steps without a transaction: if the second step fails, the first
  may already be committed and propagated; a committed partial change is recorded as `acl_update` activity with
  `partial=true`. No Valkey ACL mirror exists.

## Contract ID

- `settings.update`

## Provider

- Service: `hololive-api` admin plane (`POST /api/holo/settings`, `POST /api/holo/settings/llm`)
- Runtime: `hololive-api`

## Consumers

- Services: `iris-console` (admin HTTP caller), `alarm-worker` (receives the apply `PUT`, reads `settings.json` at startup)
- Usage: alarm advance minutes, member news run-now

## Transport

- HTTP JSON (admin API → `hololive-api`; `hololive-api` → `alarm-worker` `PUT /internal/alarm/settings`)
- Shared `settings.json` file (`SETTINGS_DIR`, default `data/settings.json`) for restart restore

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Admin paths | `POST /api/holo/settings`, `POST /api/holo/settings/llm` |
| Worker apply | `PUT /internal/alarm/settings` (see `alarm.md`) |
| Persisted file | `settings.json` (`alarmAdvanceMinutes`, `targetMinutes`) |
| Contract package | `hololive/hololive-shared/pkg/contracts/settings` (`UpdateTypeMemberNewsRunNow` response key only) |

## Request

```json
{"alarmAdvanceMinutes": 7}
```

`alarmAdvanceMinutes` must be within `1..1440`. Zero is rejected before any file write or worker call; it is not
clamped or substituted. The retired `scraperProxyEnabled` field is rejected
(DEC-20260926-hololive-legacy-env-config-retirement).

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

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| invalid request body | 400 | malformed JSON or unknown field | fix request |
| alarmAdvanceMinutes out of range | 400 | outside `1..1440`; no persistence or worker apply | fix request |
| Failed to get settings | 500 | the request was canceled or its budget expired while waiting for a consistent snapshot | surface the failed read; no stale success snapshot is returned |
| Failed to update settings | 500 | admission was canceled/timed out before persistence, or the `settings.json` write failed; worker not called | inspect the cause before re-submitting |
| `alarm_applied=false` | 200 | file written; worker application was rejected or could not be confirmed | inspect `alarm_reason`; an unknown outcome may already have changed the worker, so the response must not be interpreted as proof of its previous state |
| `acl_bot_resync_failed` | 500 | ACL route: bot-plane reload failed; the requested change may already be committed (body `message` explains) | bot keeps the previous ACL until a reload succeeds; retry the same request (already-applied parts are not written again; returns 200 no-op / 404 / 409) |
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
- Total client apply budget and cancellation before dispatch: `hololive/hololive-shared/pkg/service/alarm/client_advance_budget_test.go`
- Saved settings and unknown outcome after the worker applies but loses its response: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_settings_response_loss_test.go`
- Worker/client result classification and concurrent request ordering: `hololive/hololive-shared/pkg/service/alarm/client_advance_test.go`, `hololive/hololive-alarm-worker/internal/service/alarm/subscriptions/client_advance_integration_test.go` (both run without build tags)
- Zero rejection before persistence/apply: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_settings_update_test.go`.
- Persist-before-publish and stored-file validation: `hololive/hololive-shared/pkg/service/settings/service_test.go`.
- ACL in-process propagation, overlapping reloads, and reload-failure retry: `hololive/hololive-api/internal/service/acl/service_follow_test.go`
- ACL admin response on bot-plane apply failure: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_room_acl_propagation_test.go`

## Known gaps

- `settings.json` is written before the worker apply; a rejected apply leaves file and worker diverged until the next
  successful apply or worker restart. With an unknown outcome, that divergence cannot be determined from the response.
- ACL: a bot-plane reload failure is not retried automatically; the bot keeps its previous ACL until the admin caller
  retries, another ACL change succeeds, or `hololive-api` restarts.
