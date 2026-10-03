# Contract: majorevent

## Summary

`hololive-api`의 bot plane이 같은 프로세스의 llm plane에 major event 구독 상태를 조회/변경하는 internal HTTP JSON 계약입니다.

## Contract ID

- `majorevent.subscription`

## Provider

- Service: `hololive-api` (llm plane)
- Module: `hololive-api`
- Runtime: `hololive-api`

## Consumers

- Service: `hololive-api` (bot plane)
- Module: `hololive-api`
- Usage: major event subscription commands

## Transport

- HTTP JSON with `X-API-Key`
- Internal H3 options are passed from the bot plane's loaded config, and that plane owns its client timeout and transport cleanup.

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

## 링크 검사 HEAD→GET 예외 계약

llm plane의 major event 수집은 이벤트 링크를 HEAD로 확인합니다. HEAD를 거절하거나 처리하지 못하는 서버가 있어서, 아래 trigger에서만 같은 링크를 GET(`Range: bytes=0-0`)으로 한 번 더 확인합니다(2026-10-02 계약화).

| 항목 | 계약 |
|---|---|
| Trigger | HEAD 응답 405·403·404·501, 또는 HEAD 전송 오류 중 시간 제한(`context.DeadlineExceeded`, `net.Error.Timeout()`)과 연결 재설정(`ECONNRESET`). 오류 문자열로 판단하지 않습니다. 호출자 취소, 그 밖의 전송 오류, 차단 대상(netguard)은 해당하지 않습니다. |
| 한도 | 링크당 GET 한 번이며 요청마다 `LinkCheckerConfig.Timeout`을 씁니다. 재시도는 없고, GET도 HEAD와 같은 대상 검증을 거칩니다. |
| 종단 | GET이 2xx가 아니거나 실패하면 `failed`, 차단되면 `blocked`로 저장합니다. |
| Telemetry | `hololive_majorevent_link_get_fallback_total{result}`(`ok`, `failed`, `blocked`), 이벤트의 `link_status`·`link_checked_at`, 실패 때 Debug 로그 "Major event link check failed" |
| Owner | `hololive-api` llm plane의 majorevent scraper(`internal/planes/llm/internal/service/majorevent/scraper`) |
| 검토 조건 | `result="ok"`가 90일 동안 0이면 GET 재확인을 지우고 HEAD 결과만 씁니다. |

## Timeout and retry policy

- Timeout: consumer client uses 30 seconds.
- Retry: no automatic client retry documented.
- Idempotency: subscription operations are expected to be safe at service level; exact DB idempotency 검토 필요.

## Compatibility policy

- Additive response fields are allowed.
- Removing or renaming JSON fields requires consumer update.
- Version bump: no current HTTP body version; document before adding one.

## Tests

- Provider route tests: `hololive/hololive-api/internal/planes/llm/runtime/providers_major_event_routes_test.go`
- Consumer client tests: `hololive/hololive-api/internal/planes/bot/internal/client/majorevent/client_test.go`

## Known gaps

- No formal request/response version field.
- Error response still uses `{ "error": string }`.
