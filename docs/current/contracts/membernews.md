# Contract: membernews

## Summary

`hololive-api`의 bot plane이 같은 프로세스의 llm plane에 member news 구독 상태와 digest 생성을 요청하는 internal HTTP JSON 계약입니다.

## Contract IDs

- `membernews.digest`
- `membernews.subscription`

## Provider

- Service: `hololive-api` (llm plane)
- Module: `hololive-api`
- Runtime: `hololive-api`

## Consumers

- Service: `hololive-api` (bot plane)
- Module: `hololive-api`
- Usage: member news subscription and digest commands

## Transport

- HTTP JSON with `X-API-Key`
- Internal H3 options are passed from the bot plane's loaded config; the bot plane owns its client transport and the existing timeout. The provider remains the LLM plane in the same API process.

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

LLM 출력의 `source_url`은 입력 후보의 URL을 정확히 복사해야 하며, `member`도 같은 후보에 포함된 멤버여야 합니다. 신뢰되는 공식 도메인이라는 이유만으로 입력에 없던 경로를 허용하지 않습니다. 멤버 검증은 `MatchedMembers`의 정본 이름을 사용하며 이름 자체에 포함된 쉼표를 쪼개지 않습니다. 콜라보 후보의 멤버 부분집합·순서 변경은 허용하지만 다른 후보의 멤버를 붙일 수는 없습니다. primary와 consensus adjudicator에 같은 검증을 적용합니다. 모든 항목이 탈락하면 기존 `ErrNoValidatedItems` 처리로 전달하며 새 원천이나 자동 재시도를 추가하지 않습니다.

정기·trigger 실행은 시작 시각과 기간 후보를 실행별로 한 번 고정하고 읽기 전용 후보 metadata를 공유합니다. SQL·Go 필터·요약은 같은 시각을 사용합니다. 방별 구독 멤버·alias는 해당 방 처리 시점에 조회하며 guard·요약·render·enqueue 오류도 방별로 처리합니다. 공통 후보 조회 실패는 실행 전체 오류로 반환하고 이전 후보나 빈 성공으로 대체하지 않습니다. 단일 방 요청은 구독 멤버가 없으면 후보 조회 전에 기존 `ErrNoSubscribedMembers`를 반환합니다.

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

## 결정적 digest fallback 예외 계약

요약기가 digest를 만들지 못하면 llm plane은 검증된 후보를 그대로 나열한 결정적 digest를 돌려줍니다. 2026-09-26 stack 감사 B5(T09)가 이 경로를 "fallback 단일 소유, ResultType 표기·metric"으로 유지하기로 정했고, 2026-10-02에 workspace 예외 형식으로 정리했습니다.

| 항목 | 계약 |
|---|---|
| Trigger | `llm_disabled`(요약기 미설정 또는 `ErrLLMUnavailable`), `validation_empty`(`ErrNoValidatedItems`), `summarizer_error`(그 밖의 요약 오류), `empty_result`(요약 결과가 없거나 `TopItems`가 0건) |
| 한도 | 출처 검증과 prompt guard를 통과한 후보 중 앞 5건을 날짜·분류·제목·원문 링크로 나열합니다. 나머지는 `omitted_count`로 셉니다. fallback 자체는 외부 호출과 재시도를 하지 않습니다. 호출자 context가 끝났으면 fallback을 만들지 않고 취소 오류를 반환합니다. |
| 종단 | 검증된 후보가 0건이면 fallback이 아니라 `result_type=empty` digest를 반환합니다. |
| Telemetry | `hololive_member_news_digest_result_total{result_type,reason}`(reason은 위 4개와 빈 값의 bounded enum)와 Warn 로그 "Member news digest uses deterministic fallback" |
| Owner | `hololive-api` llm plane의 membernews(`internal/planes/llm/internal/service/membernews`) |
| 검토 조건 | `reason="summarizer_error"`가 요약 요청의 다수를 차지하면 요약기 결함으로 보고 원인을 고친 뒤 이 경로의 유지 여부를 다시 결정합니다. LLM 요약을 제품에서 빼기로 하면 결정적 digest를 정상 경로로 바꾸고 이 예외를 지웁니다. |

provider는 `result_type`(`primary`, `fallback`, `empty`)을 응답에 싣지만, consumer 계약 struct(`hololive-shared/pkg/contracts/membernews.Digest`)에는 이 필드가 없어 bot 응답에서 fallback 여부가 보이지 않습니다. 사용자에게 표시하려면 bot 문구를 바꿔야 하므로 이 계약의 범위가 아닙니다.

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

- Provider route tests: `hololive/hololive-api/internal/planes/llm/runtime/providers_membernews_routes_test.go`
- Consumer client tests: `hololive/hololive-api/internal/planes/bot/internal/client/membernews/client_test.go`
- Candidate values/order and period SQL: `hololive/hololive-api/internal/planes/llm/internal/service/membernews/repository_period_test.go`
- Run snapshot, per-room observation and failure behavior: `hololive/hololive-api/internal/planes/llm/internal/service/membernews/service_run_test.go`

## Known gaps

- No formal request/response version field.
- The consumer `Digest` struct drops the provider's `result_type`, so the bot cannot tell a deterministic fallback digest from an LLM digest.
- Error response still uses `{ "error": string }`.
