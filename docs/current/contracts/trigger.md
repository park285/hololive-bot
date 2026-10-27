# Contract: trigger

## Summary

`hololive-api`의 admin plane이 같은 프로세스의 llm plane에 major event/member news manual notification trigger를 요청하는 internal HTTP JSON 계약입니다.

## Contract ID

- `trigger.manual`

## Provider

- Service: `hololive-api` (llm plane)
- Module: `hololive-api`
- Runtime: `hololive-api`

## Consumers

- Service: `hololive-api` (admin plane)
- Module: `hololive-api`
- Usage: dashboard/admin manual notification trigger

## Transport

- HTTP JSON with `X-API-Key`
- The common trigger handler/router is API-owned at `hololive/hololive-api/internal/httpapi`; shared retains generic health/ready, JSON response and HTTP server primitives.

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Path/Event/Queue | `/internal/trigger/majorevent-weekly`, `/internal/trigger/majorevent-monthly`, `/internal/trigger/membernews-weekly` |
| Method | `POST` |
| Version | route constants, no request body |
| Contract package | `hololive/hololive-api/internal/contracts/trigger` |

## Request

```go
// No request body.
```

## Response

성공 응답은 HTTP 200의 `trigger.Result`이며 실제 delivery/handoff 완료를 뜻하지 않습니다.

```json
{"status":"queued","queued":2,"skipped":0,"failed":0}
{"status":"skipped","queued":0,"skipped":3,"failed":0}
{"status":"partial","queued":2,"skipped":1,"failed":1}
```

- `queued`: 방별 durable intent 적재가 수락된 수입니다. 기존 intent에 대한 멱등 no-op도 포함하며 현재 행이 PENDING인지, 이미 발송됐는지는 증명하지 않습니다.
- `skipped`: 구독 멤버가 없어 처리하지 않은 방의 수입니다. 방·이벤트가 없거나 membernews 실행 락을 얻지 못한 경우도 `status=skipped`이며 방 집계는 0일 수 있습니다.
- `failed`: 적재 처리 실패 또는 실행 취소로 시작하지 못한 방의 수입니다. 실패한 HTTP/DB 호출만으로 부수효과가 없었다고 추정하지 않습니다.
- 일부 방 적재가 성공하고 일부 실패하면 `partial`을 그대로 반환합니다. 전체 트리거를 자동 재시도하지 않습니다. Major event의 완료 표시는 모든 방 적재가 성공한 경우에만 진행합니다.
- 전부 실패하면 기존 500 오류 경로를 유지합니다. Major event 락 충돌은 기존 409이고 membernews 락 충돌은 skipped입니다.
- Admin client는 본문을 버리지 않고 typed result를 decode합니다. 공개 major-event trigger도 같은 결과를 반환합니다. Membernews settings run-now는 기존 `applied/reason/error/source` envelope의 `trigger` 필드에 결과를 싣습니다. `applied=true`는 실행 결과를 받았다는 뜻이며 queued/skipped/partial을 구분해서 표시해야 합니다.

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| `major_event_scheduler_unavailable` | 503 | weekly scheduler missing | manual diagnosis |
| `major_event_monthly_scheduler_unavailable` | 503 | monthly scheduler missing | manual diagnosis |
| `member_news_weekly_scheduler_unavailable` | 503 | member news scheduler missing | manual diagnosis |
| `notification_in_progress` | 409 | trigger already running | maps to `ErrNotificationInProgress` |
| `internal_server_error` | 500 | provider trigger failed; side effects may already exist | manual diagnosis; no automatic whole-trigger retry |

## Timeout and retry policy

- Timeout: consumer client uses 30 seconds.
- Provider execution uses a separate 5-minute context; a client disconnect does not shorten it. Internal H3 options are passed from admin config, and admin owns its client transport cleanup.
- Retry: no automatic whole-trigger retry, including partial enqueue and transport timeout. A client timeout does not prove that the detached provider stopped or that no intent was persisted.
- Idempotency: provider execution locks and the existing outbox `(kind, content_id)` uniqueness/rearm contract remain unchanged.
- Digest execution is cooperatively canceled after at most 14 minutes, before the existing 15-minute lock TTL; an earlier caller deadline (including HTTP's 5 minutes) wins. Scheduled runtime Stop cancels the active tick and then joins it before resources can be released. Lock release remains bounded to 5 seconds independently of caller cancellation.
- Cancellation is not joining or fencing: an external call that ignores cancellation can outlive the TTL. No renewal or lease-fencing redesign is introduced, so the budget alone does not prove exclusive execution after expiry.

## Compatibility policy

- Route constants in `hololive-api/internal/contracts/trigger` are SSOT.
- The 409 mapping is contract-significant.
- This is a coordinated source cutover to the typed result, with no legacy `"... sent"` response shim. Provider, admin client, and run-now callers must ship together; route paths/status codes and persisted outbox bytes are unchanged. This source contract update is not a deployment-completion claim.
- New trigger routes must update `CONTRACT_MAP.md`, this file, provider registration, and consumer client.

## Tests

- Route constants: `hololive/hololive-api/internal/contracts/trigger/routes_test.go`
- Provider/router tests: `hololive/hololive-api/internal/planes/llm/runtime/router_integration_test.go`, `hololive/hololive-api/internal/httpapi/trigger_test.go`, `router_test.go`
- Consumer tests: `hololive/hololive-api/internal/planes/admin/internal/client/trigger/client_test.go`
- Enqueue result decoding/no-retry: `hololive/hololive-api/internal/planes/admin/internal/client/trigger/client_result_test.go`
- Budget/cancel-then-join: `hololive/hololive-api/internal/planes/llm/internal/schedulerkit/digest_lifetime_test.go`

## Known gaps

- No typed error body beyond current `{ "error": string }` compatibility format.
