# Error Contract

## Scope

이 문서는 internal HTTP API의 현재 error response와 client 해석 규칙을 고정합니다.

## Current Compatibility Format

`hololive-shared/pkg/server.RespondError`는 현재 다음 형태를 반환합니다.

```json
{"error":"error_code_or_message"}
```

호출부가 `extra`를 넘기면 같은 JSON object에 추가 field가 병합됩니다.

Alarm shared API는 일부 endpoint에서 다음 envelope도 사용합니다.

```json
{"success":false,"message":"error message"}
```

## Additive Typed Format

`shared-go/pkg/httputil.APIError`는 기존 `{"error":"error_code_or_message"}` 응답을 깨지 않고 다음 추가 field를 해석합니다.

```json
{
  "error": "stable_error_code",
  "message": "human readable message",
  "request_id": "optional request id",
  "details": {}
}
```

## Status Mapping

| HTTP status | Meaning | Example code | Client rule |
|---:|---|---|---|
| 400 | invalid caller request | `invalid_request`, `room_id_required` | fix request; do not retry blindly |
| 401/403 | auth failure | 검토 필요 | verify internal API key/secret |
| 404 | resource/domain state not found | `no_subscribed_members` | map only documented stable codes |
| 409 | in-progress/conflict | `notification_in_progress` | map to typed conflict when documented |
| 500 | provider internal failure | `internal_server_error`, `digest_generation_failed` | retry/manual diagnosis |
| 503 | dependency/scheduler unavailable | `*_scheduler_unavailable` | manual diagnosis before retry loops |

## Client Interpretation Rules

- Clients must not parse arbitrary full error strings from `httputil.CheckStatus`.
- Clients should use `httputil.IsStatus`, `httputil.IsCode`, and `httputil.AsAPIError`.
- Clients may map a specific stable body code only when the contract document lists it.
- Status code remains the first branch key; body parsing is a secondary contract-specific step.
- Production code must not use substring parsing for `status 404` or `status 409`.
- New error codes must be added to the relevant `docs/current/contracts/*.md` file.
- Error response shape changes must update this document and `scripts/architecture/check-error-contracts.sh`.

## Related Contract Codes

- `contracts/membernews.md` documents `no_subscribed_members`.
- `contracts/trigger.md` documents `notification_in_progress`.
- `contracts/alarm.md` documents current alarm envelope errors.

## Validation

```bash
./scripts/architecture/check-error-contracts.sh
```

## Known Gaps

- `RespondError` does not yet emit `message` or `request_id` by default.
- Alarm API envelope uses `success/message` rather than the shared `{error}` format.
