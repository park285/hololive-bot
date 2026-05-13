# Contract: alarm

## Summary

Alarm domain currently has HTTP JSON APIs, the Valkey dispatch queue, generic notification delivery outbox egress, and the YouTube notification outbox egress path owned by `alarm-worker`.

## Contract IDs

- `alarm.http`
- `alarm.dispatch`
- `youtube.outbox.egress`

## Provider

- HTTP service: `admin-api` registers `hololive-shared/pkg/service/alarm.APIHandler` when `AlarmCRUD` is configured.
- Domain owner: `alarm-worker`.
- Ownership decision: short-term `admin-api` provider 유지, long-term `alarm-worker` provider migration. See `../../design/alarm-http-provider-ownership.md`.
- Queue service: `alarm-worker`
- Modules: `hololive-admin-api`, `hololive-alarm-worker`, `hololive-shared`

## Consumers

- HTTP consumers: `bot`, `admin-api` facade paths
- Queue consumer: legacy `dispatcher-go` where enabled; production proactive egress owner is `alarm-worker`.
- Usage: alarm CRUD/query, next stream lookup, settings updates, dispatch delivery, YouTube outbox handoff

## Transport

- HTTP JSON for `/internal/alarm/*`
- Valkey list/sorted set/list for dispatch queue, delayed retry, DLQ

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| HTTP paths | `/internal/alarm/add`, `/remove`, `/room/:id`, `/room/:id/view`, `/clear`, `/next-stream/:id`, `/settings`, `/room-name`, `/user-name`, `/keys` |
| Queue keys | `alarm:dispatch:queue`, `alarm:dispatch:retry`, `alarm:dispatch:dlq` |
| Method | mixed HTTP methods; Valkey `LPUSH`, `BRPOP`, `ZADD`, delayed drain script |
| Version | HTTP unversioned; queue `QueueEnvelopeVersionV1 = 1`, consumer accepts `0` and `1` |
| Contract package | `hololive/hololive-shared/pkg/contracts/alarm`; HTTP DTOs remain under `hololive/hololive-shared/pkg/service/alarm` |
| Queue fixtures | `hololive/hololive-shared/pkg/contracts/alarm/testdata/envelope_v1.json`, `envelope_unsupported_version.json` |

## Request

```go
type AlarmQueueEnvelope struct {
    Notification  domain.AlarmNotification          `json:"notification"`
    ClaimKeys     []string                          `json:"claim_keys"`
    EnqueuedAt    string                            `json:"enqueued_at"`
    Version       uint8                             `json:"version"`
    Retry         *AlarmQueueRetryMetadata          `json:"retry,omitempty"`
    SourcePayload string                            `json:"source_payload,omitempty"`
    SourceKind    domain.AlarmDispatchSourceKind    `json:"source_kind,omitempty"`
    YouTubeOutbox *domain.YouTubeOutboxDispatchPayload `json:"youtube_outbox,omitempty"`
}
```

Legacy alarm notifications keep using `Notification` and `ValidateLegacyRoute`.
Major event/member news rows are produced in `notification_delivery_outbox`; `alarm-worker` claims those rows and sends them through Iris/Kakao. YouTube live/video/community/shorts rows are produced in `youtube_notification_outbox`; `alarm-worker` claims those rows, resolves rooms, renders with the shared YouTube outbox formatter, sends through Iris/Kakao, and writes per-room delivery state.

HTTP request DTOs are currently defined in `hololive/hololive-shared/pkg/service/alarm/dto.go` and the client-local request structs in `client.go`.

## Response

```go
type APIResponse struct {
    Success bool        `json:"success"`
    Error   string      `json:"error,omitempty"`
    Message string      `json:"message,omitempty"`
    Data    interface{} `json:"data,omitempty"`
}
```

Queue success has no response body; delivery outcome is represented by queue movement, retry metadata, claim release, and dispatcher logs/metrics.

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| `invalid_request_body` | 400 | invalid HTTP payload | fix caller input |
| `alarm_add_failed` | 500 | provider add failed | retry/manual diagnosis |
| `alarm_remove_failed` | 500 | provider remove failed | retry/manual diagnosis |
| `get_room_alarms_failed` | 500 | provider query failed | retry/manual diagnosis |
| `get_room_alarms_view_failed` | 500 | provider view query failed | retry/manual diagnosis |
| `clear_room_alarms_failed` | 500 | provider clear failed | retry/manual diagnosis |
| `get_next_stream_info_failed` | 500 | provider query failed | retry/manual diagnosis |
| `set_room_name_failed` | 500 | provider room name update failed | retry/manual diagnosis |
| `set_user_name_failed` | 500 | provider user name update failed | retry/manual diagnosis |
| `get_all_alarm_keys_failed` | 500 | provider key listing failed | retry/manual diagnosis |
| unsupported queue version | n/a | queue consumer rejects payload | preserve raw payload to DLQ; not accepted for delivery |
| Invalid JSON | n/a | payload cannot parse | preserve raw payload to DLQ |

## Timeout and retry policy

- HTTP client timeout: 10 seconds for alarm client.
- Queue drain: first item blocks up to consumer block timeout, then drains batches.
- Retry queue: delayed retry uses `alarm:dispatch:retry` sorted set and retry metadata (`attempt`, `retry_after_ms`, `next_visible_at`, `last_error`).
- DLQ: invalid raw payloads and moved envelopes are preserved in `alarm:dispatch:dlq`.

## Compatibility policy

- Queue consumers must retain dual-read behavior when introducing a new envelope version.
- Raw payload preservation must remain in place before changing DLQ tooling.
- HTTP provider migration must follow `../../design/alarm-http-provider-ownership.md` before moving `/internal/alarm/*` between runtimes.

## Tests

- Contract constants: `hololive/hololive-shared/pkg/contracts/alarm/contracts_test.go`
- Queue fixtures: `hololive/hololive-shared/pkg/contracts/alarm/testdata/envelope_v1.json`, `envelope_unsupported_version.json`
- Queue behavior: `hololive/hololive-shared/pkg/service/alarm/queue/queue_test.go`
- HTTP handler/client: `hololive/hololive-shared/pkg/service/alarm/api_test.go`, `client_test.go`

## Known gaps

- Alarm HTTP API DTOs are not yet represented by a dedicated `pkg/contracts/alarm` DTO package.
- Current HTTP provider registration is `admin-api`; long-term migration to `alarm-worker` is documented but not implemented.
