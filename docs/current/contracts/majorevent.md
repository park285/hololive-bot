# Contract: majorevent

## Summary

`bot`이 `llm-scheduler`에 major event 구독 상태를 조회/변경하는 internal HTTP JSON 계약입니다.

## Provider

- Service: `llm-scheduler`
- Module: `hololive-llm-sched`
- Runtime: `llm-scheduler`

## Consumers

- Service: `bot`
- Module: `hololive-kakao-bot-go`
- Usage: major event subscription commands

## Transport

- HTTP JSON with `X-API-Key`

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Path/Event/Queue | `/internal/majorevent/subscriptions` |
| Method | `GET /subscriptions/:roomID`, `POST /subscriptions`, `DELETE /subscriptions/:roomID` |
| Version | unversioned HTTP body; route constants in package |
| Contract package | `hololive/hololive-shared/pkg/contracts/majorevent` |

## Request

```go
type SubscribeRequest struct {
    RoomID   string `json:"room_id"`
    RoomName string `json:"room_name"`
}
```

## Response

```go
type SubscriptionStatusResponse struct {
    Subscribed bool `json:"subscribed"`
}
```

Subscribe/unsubscribe success currently returns `{"status":"subscribed"}` or `{"status":"unsubscribed"}`.

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| `invalid_request` | 400 | request JSON binding failed | surface command error |
| `room_id_required` | 400 | missing room id | fix caller input |
| `subscription_check_failed` | 500 | provider failed checking state | retry/manual diagnosis |
| `subscribe_failed` | 500 | provider failed subscribing | retry/manual diagnosis |
| `unsubscribe_failed` | 500 | provider failed unsubscribing | retry/manual diagnosis |

## Timeout and retry policy

- Timeout: consumer client uses 30 seconds.
- Retry: no automatic client retry documented.
- Idempotency: subscription operations are expected to be safe at service level; exact DB idempotency 검토 필요.

## Compatibility policy

- Additive response fields are allowed.
- Removing or renaming JSON fields requires consumer update.
- Version bump: no current HTTP body version; document before adding one.

## Tests

- Provider route tests: `hololive/hololive-llm-sched/internal/app/providers_major_event_routes_test.go`
- Consumer client tests: `hololive/hololive-kakao-bot-go/internal/service/majoreventclient/client_test.go`

## Known gaps

- No formal request/response version field.
- Error response still uses `{ "error": string }`.
