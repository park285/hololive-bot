# Contract: majorevent

## Summary

`hololive-api`의 bot plane이 같은 프로세스 llm plane의 major event 구독 저장소를 직접 호출해 구독 상태를 조회·변경하는 in-process 계약입니다. HTTP 경로와 직렬화는 없습니다.

## Contract ID

- `majorevent.subscription`

## Provider

- Service: `hololive-api` (llm plane `majorevent.Repository`, `internal/planes/llm/internal/service/majorevent`)

## Consumers

- Service: `hololive-api` (bot plane `handlercore.MajorEventRepository`)
- Usage: major event subscription commands

## Wiring

`app.BuildRuntime`이 `LLMSchedulerRuntime.MajorEventSubscriptions`를 `botruntime.Dependencies.MajorEvent`로 넘깁니다.

```go
type MajorEventRepository interface {
    IsSubscribed(ctx context.Context, roomID string) (bool, error)
    Subscribe(ctx context.Context, roomID, roomName string) error
    Unsubscribe(ctx context.Context, roomID string) error
}
```

오류는 bot 명령 오류 응답으로 드러나며 문자열 오류 코드 매핑은 없습니다.

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

- Timeout: bot 명령 실행 시한(worker profile `bot_webhook_inbox` executor `attempt_timeout`)을 그대로 따릅니다.
- Retry: 자동 재시도는 없습니다.
- Idempotency: subscription operations are expected to be safe at service level; exact DB idempotency 검토 필요.

## Compatibility policy

- 이전의 llm plane `/internal/majorevent/subscriptions` HTTP 경로와 `internal/contracts/majorevent` route 상수는 삭제했습니다.

## Tests

- Repository: `hololive/hololive-api/internal/planes/llm/internal/service/majorevent/repository_test.go`, `repository_postgres_test.go`
