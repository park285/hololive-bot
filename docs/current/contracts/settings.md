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

`alarmAdvanceMinutes` must be within `0..1440`; the retired `scraperProxyEnabled` field is rejected
(DEC-20260926-hololive-legacy-env-config-retirement).

## Response

`runtime` reports the worker apply result:

| Key | Meaning |
|---|---|
| `alarm_requested_advance_minutes` | requested minutes |
| `alarm_applied` | `true` only when alarm-worker returned target minutes |
| `alarm_reason` | set when not applied (for example `alarm worker did not apply alarm advance minutes`) |
| `alarm_target_minutes` | target minutes returned by alarm-worker |

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| invalid request body | 400 | malformed JSON or unknown field | fix request |
| alarmAdvanceMinutes out of range | 400 | outside `0..1440` | fix request |
| Failed to update settings | 500 | `settings.json` write failed; worker not called | retry after fixing storage |
| `alarm_applied=false` | 200 | file written, worker apply failed | alarm-worker keeps the previous value until the next successful apply or its restart (which reads `settings.json`); re-submit the setting |
| `acl_bot_resync_failed` | 500 | ACL route: bot-plane reload failed; the requested change may already be committed (body `message` explains) | bot keeps the previous ACL until a reload succeeds; retry the same request (already-applied parts are not written again; returns 200 no-op / 404 / 409) |
| Failed to add room / Failed to remove room / Failed to set ACL enabled / Failed to set ACL mode | 500 | ACL route: that step's PostgreSQL write failed. For a combined `enabled`+`mode` request, `enabled` may already be applied | retry after fixing storage |

## Timeout and retry policy

- Timeout: worker `PUT` uses the internal service client timeout (10s).
- Retry: none. A failed apply is reported in `runtime`, not retried.
- Idempotency: re-submitting the same value is safe.

## Compatibility policy

- Response `runtime` keys are additive; removing a key is a contract change for the admin caller.

## Tests

- Single apply path: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_settings_update_test.go`
- ACL in-process propagation, overlapping reloads, and reload-failure retry: `hololive/hololive-api/internal/service/acl/service_follow_test.go`
- ACL admin response on bot-plane apply failure: `hololive/hololive-api/internal/planes/admin/internal/server/api/api_room_acl_propagation_test.go`

## Known gaps

- `settings.json` is written before the worker apply; an apply failure leaves file and worker diverged until the next
  successful apply or worker restart.
- ACL: a bot-plane reload failure is not retried automatically; the bot keeps its previous ACL until the admin caller
  retries, another ACL change succeeds, or `hololive-api` restarts.
