# Contract: membernews

## Summary

`bot`이 `llm-scheduler`에 member news 구독 상태와 digest 생성을 요청하는 internal HTTP JSON 계약입니다.

## Contract IDs

- `membernews.digest`
- `membernews.subscription`

## Provider

- Service: `llm-scheduler`
- Module: `hololive-llm-sched`
- Runtime: `llm-scheduler`

## Consumers

- Service: `bot`
- Module: `hololive-kakao-bot-go`
- Usage: member news subscription and digest commands

## Transport

- HTTP JSON with `X-API-Key`

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Path/Event/Queue | `/internal/membernews/subscriptions`, `/internal/membernews/digest` |
| Method | `GET /subscriptions/:roomID`, `POST /subscriptions`, `DELETE /subscriptions/:roomID`, `POST /digest` |
| Version | unversioned HTTP body; route constants in package |
| Contract package | `hololive/hololive-shared/pkg/contracts/membernews` |

## Request

```go
type SubscribeRequest struct {
    RoomID   string `json:"room_id"`
    RoomName string `json:"room_name"`
}

type digestRequest struct {
    RoomID string `json:"room_id"`
    Period string `json:"period"`
}
```

## Response

```go
type SubscriptionStatusResponse struct {
    Subscribed bool `json:"subscribed"`
}

type Digest struct {
    Period       Period        `json:"period"`
    Headline     string        `json:"headline"`
    TopItems     []SummaryItem `json:"top_items"`
    MoreSummary  string        `json:"more_summary"`
    OmittedCount int           `json:"omitted_count"`
    TotalCount   int           `json:"total_count"`
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
| `no_subscribed_members` | 404 | room has no subscribed members | maps to `ErrNoSubscribedMembers` |
| `digest_generation_failed` | 500 | digest generation failed | retry/manual diagnosis |

## Timeout and retry policy

- Timeout: consumer client uses 60 seconds.
- Retry: no automatic client retry documented.
- Idempotency: subscription operations are expected to be safe at service level; exact DB idempotency 검토 필요.

## Compatibility policy

- Additive response fields are allowed.
- Removing or renaming JSON fields requires consumer update.
- Error string values are contract-significant until typed errors are introduced.
- Version bump: no current HTTP body version; document before adding one.

## Tests

- Provider route tests: `hololive/hololive-llm-sched/internal/app/providers_membernews_routes_test.go`
- Consumer client tests: `hololive/hololive-kakao-bot-go/internal/service/membernewsclient/client_test.go`

## Known gaps

- No formal request/response version field.
- Error response still uses `{ "error": string }`.
