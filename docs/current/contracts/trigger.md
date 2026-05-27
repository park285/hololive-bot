# Contract: trigger

## Summary

`admin-api`가 `llm-scheduler`에 major event/member news manual notification trigger를 요청하는 internal HTTP JSON 계약입니다.

## Contract ID

- `trigger.manual`

## Provider

- Service: `llm-scheduler`
- Module: `hololive-llm-sched`
- Runtime: `llm-scheduler`

## Consumers

- Service: `admin-api`
- Module: `hololive-admin-api`
- Usage: dashboard/admin manual notification trigger

## Transport

- HTTP JSON with `X-API-Key`

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Path/Event/Queue | `/internal/trigger/majorevent-weekly`, `/internal/trigger/majorevent-monthly`, `/internal/trigger/membernews-weekly` |
| Method | `POST` |
| Version | route constants, no request body |
| Contract package | `hololive/hololive-shared/pkg/contracts/trigger` |

## Request

```go
// No request body.
```

## Response

```go
// Success body examples:
gin.H{"status": "weekly notification sent"}
gin.H{"status": "monthly notification sent"}
gin.H{"status": "member news weekly digest sent"}
```

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| `major_event_scheduler_unavailable` | 503 | weekly scheduler missing | manual diagnosis |
| `major_event_monthly_scheduler_unavailable` | 503 | monthly scheduler missing | manual diagnosis |
| `member_news_weekly_scheduler_unavailable` | 503 | member news scheduler missing | manual diagnosis |
| `notification_in_progress` | 409 | trigger already running | maps to `ErrNotificationInProgress` |
| `internal_server_error` | 500 | provider trigger failed | retry/manual diagnosis |

## Timeout and retry policy

- Timeout: consumer client uses 30 seconds.
- Retry: no automatic client retry documented.
- Idempotency: protected by provider in-progress handling where implemented.

## Compatibility policy

- Route constants in `contracts/trigger` are SSOT.
- The 409 mapping is contract-significant.
- New trigger routes must update `CONTRACT_MAP.md`, this file, provider registration, and consumer client.

## Tests

- Route constants: `hololive/hololive-shared/pkg/contracts/trigger/routes_test.go`
- Provider/router tests: `hololive/hololive-llm-sched/internal/app/*trigger*_test.go`
- Consumer tests: `hololive/hololive-admin-api/internal/client/trigger/client_test.go`

## Known gaps

- No typed error body beyond current `{ "error": string }` compatibility format.
