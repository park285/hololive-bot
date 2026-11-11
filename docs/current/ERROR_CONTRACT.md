# Error Contract

## Scope

이 문서는 internal HTTP API의 현재 error response와 client 해석 규칙을 고정합니다. 계약 대상은 아래 JSON wire 형태이며, 오류 응답 형태는 생산자마다 다릅니다.

## Response Shapes

### Admin API

hololive-api admin plane의 `/api/holo/*` handler는 `hololive-shared/pkg/server/httpserver.RespondError`(500은 `RespondInternalError`)로 오류를 응답합니다.

```json
{"error":"human readable message"}
```

`error`는 사람용 문구이며 계약이 아닙니다. 호출자가 분기해야 하는 거절은 `RespondError`의 `extra`로 안정 `code` field를 같은 JSON object에 더합니다. 현재 안정 code는 방 ACL 변경의 `409 acl_mode_changed` 하나입니다. 상세는 [admin.api 계약](contracts/admin-api.md)을 따릅니다.

```json
{"error":"ACL mode changed","code":"acl_mode_changed"}
```

### Shared Middleware

`hololive-shared/pkg/server/middleware`의 API key 인증, admin plane no-route, panic 복구 응답은 `error`에 안정 code를, `message`에 사람용 문구를 둡니다. admin API와 alarm API의 인증 실패도 handler보다 먼저 이 형태로 응답합니다.

```json
{"error":"unauthorized","message":"API key required"}
```

| HTTP status | `error` | 조건 |
|---:|---|---|
| 401 | `unauthorized` | `X-API-Key` header가 없습니다. |
| 403 | `forbidden` | `X-API-Key`가 설정된 key와 다릅니다. |
| 404 | `not_found` | admin plane에 등록되지 않은 경로입니다. 인증을 먼저 확인합니다. |
| 500 | `internal_error` | handler panic을 복구했고 응답을 아직 쓰지 않았습니다. |
| 503 | `auth_not_configured` | 서버에 API key가 설정되지 않았습니다. |

### Alarm API

alarm-worker의 alarm HTTP API handler는 성공과 실패 모두 `hololive-shared/pkg/contracts/alarm.Envelope` 형태로 응답합니다. 실패하면 `success`는 `false`, `error`는 안정 code, `message`는 사람용 문구입니다. code 목록은 [alarm 계약](contracts/alarm.md)의 "Error codes" 표가 소유합니다.

```json
{"success":false,"error":"stable_error_code","message":"error message"}
```

## Status Mapping

| HTTP status | Meaning | Example code | Client rule |
|---:|---|---|---|
| 400 | invalid caller request | alarm `invalid_request_body`, `invalid_host_id` | fix request; do not retry blindly |
| 401 | missing API key | middleware `unauthorized` | verify internal API key |
| 403 | invalid API key | middleware `forbidden` | verify internal API key |
| 404 | unknown route or resource not found | middleware `not_found` | map only documented stable codes |
| 409 | in-progress/conflict | admin `code` `acl_mode_changed` | map to typed conflict when documented |
| 500 | provider internal failure | middleware `internal_error`, alarm `alarm_add_failed` | retry/manual diagnosis |
| 503 | dependency unavailable or auth not configured | middleware `auth_not_configured` | manual diagnosis before retry loops |

admin handler 오류는 `acl_mode_changed`(409)를 빼면 status(400, 404, 409, 413, 415, 429, 500, 502, 503)와 관계없이 안정 code 없이 문구로만 응답합니다.

관리자 멤버 생성의 `Member already exists with different values`(409)는 같은 slug의 정본이 요청과 다름을 뜻합니다. 이미 저장된 멤버의 캐시 동기화 실패는 500이 아니라 기존 201/200 성공 envelope에 `persisted=true`, `cacheSynced=false`와 경고 문구로 표현합니다. 저장 실패와 전파 실패를 같은 재시도 대상으로 취급하지 않습니다. [멤버 쓰기 계약](services/hololive-api.md#관리자-멤버-쓰기).

## Client Interpretation Rules

- Clients must not parse arbitrary full error strings from `httputil.CheckStatus`.
- Clients should unwrap with `errors.AsType[*httputil.APIError](err)` and branch on its `StatusCode` and `Code` fields.
- Clients may map a specific stable body code only when the contract document lists it.
- Status code remains the first branch key; body parsing is a secondary contract-specific step.
- Production code must not use substring parsing for `status 404` or `status 409`.
- New error codes must be added to the relevant `docs/current/contracts/*.md` file.
- Error response shape changes must update this document.

## Related Contract Codes

- `contracts/alarm.md` documents current alarm envelope errors.

## Known Gaps

- `shared-go/pkg/httputil.APIError`는 본문의 `error`를 `Code`로, `message`를 `Message`로 읽고 admin의 `code` field는 읽지 않습니다. 그래서 admin handler 오류의 `APIError.Code`는 사람용 문구이며 분기 키로 쓸 수 없습니다.
- `APIError`는 `request_id`와 `details`도 읽지만 현재 이 두 field를 보내는 생산자는 없습니다.
