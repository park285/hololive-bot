# Contract: alarm

## Summary

Alarm domain currently has two contract surfaces: alarm HTTP JSON APIs and the Valkey dispatch queue consumed by `dispatcher-go`.

## Contract IDs

- `alarm.http`
- `alarm.dispatch`

## Provider

- HTTP service: `admin-api` registers `hololive-shared/pkg/service/alarm.APIHandler` when `AlarmCRUD` is configured; provider ownership remains 검토 필요 because alarm domain work is split from `alarm-worker`.
- Queue service: `alarm-worker`
- Modules: `hololive-admin-api`, `hololive-alarm-worker`, `hololive-shared`

## Consumers

- HTTP consumers: `bot`, `admin-api` facade paths
- Queue consumer: `dispatcher-go`
- Usage: alarm CRUD/query, next stream lookup, settings updates, dispatch delivery

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
| Contract package | `hololive/hololive-shared/pkg/contracts/alarm`; HTTP handler/client under `hololive/hololive-shared/pkg/service/alarm` |

## Request

```go
type AlarmQueueEnvelope struct {
    Notification  domain.AlarmNotification `json:"notification"`
    ClaimKeys     []string                 `json:"claim_keys"`
    EnqueuedAt    string                   `json:"enqueued_at"`
    Version       uint8                    `json:"version"`
    Retry         *AlarmQueueRetryMetadata `json:"retry,omitempty"`
    SourcePayload string                   `json:"source_payload,omitempty"`
}
```

HTTP request DTOs are currently defined in `hololive/hololive-shared/pkg/service/alarm/dto.go` and the client-local request structs in `client.go`.

## Response

```go
type APIResponse struct {
    Success bool        `json:"success"`
    Message string      `json:"message,omitempty"`
    Data    interface{} `json:"data,omitempty"`
}
```

Queue success has no response body; delivery outcome is represented by queue movement, retry metadata, claim release, and dispatcher logs/metrics.

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| `invalid request body` / bind error | 400 | invalid HTTP payload | fix caller input |
| `alarm add failed` | 500 | provider add failed | retry/manual diagnosis |
| `alarm remove failed` | 500 | provider remove failed | retry/manual diagnosis |
| `get room alarms failed` | 500 | provider query failed | retry/manual diagnosis |
| `get next stream info failed` | 500 | provider query failed | retry/manual diagnosis |
| unsupported queue version | n/a | queue consumer rejects payload | log and skip; not accepted for delivery |
| invalid queue JSON | n/a | payload cannot parse | preserve raw payload to DLQ |

## Timeout and retry policy

- HTTP client timeout: 10 seconds for alarm client.
- Queue drain: first item blocks up to consumer block timeout, then drains batches.
- Retry queue: delayed retry uses `alarm:dispatch:retry` sorted set and retry metadata.
- DLQ: invalid raw payloads and moved envelopes are preserved in `alarm:dispatch:dlq`.

## Compatibility policy

- Queue consumers must retain dual-read behavior when introducing a new envelope version.
- Raw payload preservation must remain in place before changing DLQ tooling.
- HTTP provider ownership should be clarified before moving `/internal/alarm/*` between runtimes.

## Tests

- Contract constants: `hololive/hololive-shared/pkg/contracts/alarm/contracts_test.go`
- Queue behavior: `hololive/hololive-shared/pkg/service/alarm/queue/queue_test.go`
- HTTP handler/client: `hololive/hololive-shared/pkg/service/alarm/api_test.go`, `client_test.go`

## Known gaps

- Alarm HTTP API is not yet represented by a dedicated `pkg/contracts/alarm` route/DTO package.
- Current HTTP provider registration is `admin-api`; long-term ownership remains 검토 필요.
