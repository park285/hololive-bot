# Contract: alarm

## Summary

Alarm domain currently has HTTP JSON APIs, the Valkey dispatch queue, generic notification delivery outbox egress, and the YouTube notification outbox egress path owned by `alarm-worker`.

## Contract IDs

- `alarm.http`
- `alarm.dispatch`
- `youtube.outbox.egress`
- `alarm.state.read`

## Provider

- HTTP staged provider: `alarm-worker` registers `hololive-shared/pkg/service/alarm.Handler` for `/internal/alarm/*` through the shared alarm route registrar when `AlarmCRUD` is configured.
- HTTP compatibility provider: `hololive-api` admin plane still registers the same route set during the migration window so existing callers can roll forward without a hard cutover.
- Domain owner: `alarm-worker`.
- Ownership decision: `alarm-worker` is the target owner; `hololive-api` admin-plane compatibility registration must be removed after bot/admin clients are cut over. See `../../design/alarm-http-provider-ownership.md`.
- Queue service: `alarm-worker`
- Modules: `hololive-api`, `hololive-alarm-worker`, `hololive-shared`

## Consumers

- HTTP consumers: `hololive-api` (bot + admin-plane facade paths)
- Queue consumer: `alarm-worker`.
- `alarm_state` read consumer: `hololive-api` — `alarms` 테이블을 `alarmread.Reader`(`GetAllChannelIDs`, `LoadAll`)로만 읽습니다. `pkg/service/alarm.Repository`는 `Add`/`Remove`/`ClearByRoom`을 함께 노출하므로 collector/API YouTube plane에 직접 주입하지 않으며, `check-repository-ownership.sh`가 해당 import와 `alarm.NewRepository` 호출을 차단합니다.
- Usage: alarm CRUD/query, next stream lookup, settings updates, dispatch delivery, YouTube outbox handoff

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
| Contract package | `hololive/hololive-shared/pkg/contracts/alarm`; HTTP DTOs remain under `hololive/hololive-shared/pkg/service/alarm` |
| Queue fixtures | `hololive/hololive-shared/pkg/contracts/alarm/testdata/envelope_v1.json`, `envelope_unsupported_version.json` |

## Request

```go
type AlarmQueueEnvelope struct {
    Notification  domain.AlarmNotification          `json:"notification"`
    ClaimKeys     []string                          `json:"claim_keys"`
    EnqueuedAt    string                            `json:"enqueued_at"`
    Version       uint8                             `json:"version"`
    Retry         *AlarmQueueRetryMetadata          `json:"retry,omitempty"`
    SourcePayload string                            `json:"source_payload,omitempty"`
    SourceKind    domain.AlarmDispatchSourceKind    `json:"source_kind,omitempty"`
    YouTubeOutbox *domain.YouTubeOutboxDispatchPayload `json:"youtube_outbox,omitempty"`
    Celebration   *domain.CelebrationDispatchPayload   `json:"celebration,omitempty"`
}

type AlarmQueueRetryMetadata struct {
    Attempt       int    `json:"attempt,omitempty"`
    RetryAfterMS  int64  `json:"retry_after_ms,omitempty"`
    NextVisibleAt string `json:"next_visible_at,omitempty"`
    LastError     string `json:"last_error,omitempty"`
    LastErrorCode string `json:"last_error_code,omitempty"`
}
```

Live alarm notifications keep using `Notification` and `ValidateLiveDispatchRoute`.
YouTube 최초공개(`youtube_live_sessions.is_premiere=true`)는 live upcoming 및 live catchup 후보가 아니다. 구독자 알림은 `NEW_VIDEO` outbox의 `공개 예정`/`최초공개`만 보낸다. `DEC-20260830-hololive-premiere-content-owned-notifications`.
Major event/member news rows are produced in `notification_delivery_outbox`; `alarm-worker` claims those rows and sends them through Iris/Kakao. YouTube live/video/community/shorts rows are produced in `youtube_notification_outbox`; `alarm-worker` claims those rows, resolves rooms, renders with the shared YouTube outbox formatter, sends through Iris/Kakao, and writes per-room delivery state.

Birthday stream notifications use `SourceKind=celebration`, `AlarmType=BIRTHDAY`, and `Celebration.Kind=birthday_stream`. Their recipient contract is the set of rooms whose matching `celebration:birthday:{channelID}:{date}` delivery is already `sent`; an audience lookup failure must not widen delivery to other rooms. Re-publishing a known birthday stream event is permitted so a newly eligible room can add its missing delivery through the existing event/delivery dedupe keys.

HTTP request DTOs are currently defined in `hololive/hololive-shared/pkg/service/alarm/dto.go` and the client-local request structs in `client.go`.

### UNIT B member subscriptions

구독 주체는 채팅방이다. `/add`와 `/remove`는 선택적인 `host_id`를 받으며,
`channel_id=UC3OH5FKQ3qtl4uRme_vZTgA`에서 `kiyosumi-lyra`, `reimei-mira`,
`yoinagi-neon`을 지원한다. 생략한 `host_id`는 기존 전체 채널 구독이다.
저장 식별자는 `(room_id, channel_id, host_id)`이며 멤버별 `alarm_types`를 따로 보존한다.
전체 채널 해지는 별도로 등록한 멤버 구독을 삭제하지 않는다.

채널별 구독 캐시는 해당 방의 전체·멤버별 구독 합집합이다. UNIT B의 최종 수신 방은
`alarm.ResolveEventSubscribers`가 DB 구독 행과 제목으로 결정한다. 진행자가 확인되면
해당 멤버와 전체 채널 구독 방을, 미상이면 해당 알림 종류를 구독한 모든 UNIT B 방을 포함한다.
공동 진행자는 합집합이며 방 ID는 중복을 제거한다. 외부 유닛의 게스트를 UNIT B 진행자로
취급하지 않는다. 구독 DB 오류나 손상된 outbox payload는 이 fail-open의 대상이 아니다.

신규 구독을 허용하기 전에 migration 194–196, 양쪽 HTTP provider, worker의 대상 선정 코드를
함께 전환해야 한다. 196 이후에는 이전 `(room_id, channel_id)` upsert를 실행할 수 없다.
운영 전환 조건과 검증은 [변경 보고서](../../review/unit-b-member-subscriptions-20260906.md)에 있다.

## Response

```go
type APIResponse struct {
    Success bool        `json:"success"`
    Error   string      `json:"error,omitempty"`
    Message string      `json:"message,omitempty"`
    Data    interface{} `json:"data,omitempty"`
}
```

Queue success has no response body; delivery outcome is represented by queue movement, retry metadata, claim release, and dispatcher logs/metrics.

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| `invalid_request_body` | 400 | invalid HTTP payload | fix caller input |
| `invalid_host_id` | 400 | unsupported UNIT B member target | fix channel/host input |
| `alarm_add_failed` | 500 | provider add failed | retry/manual diagnosis |
| `alarm_remove_failed` | 500 | provider remove failed | retry/manual diagnosis |
| `get_room_alarms_failed` | 500 | provider query failed | retry/manual diagnosis |
| `get_room_alarms_view_failed` | 500 | provider view query failed | retry/manual diagnosis |
| `clear_room_alarms_failed` | 500 | provider clear failed | retry/manual diagnosis |
| `get_next_stream_info_failed` | 500 | provider query failed | retry/manual diagnosis |
| `set_room_name_failed` | 500 | provider room name update failed | retry/manual diagnosis |
| `set_user_name_failed` | 500 | provider user name update failed | retry/manual diagnosis |
| `get_all_alarm_keys_failed` | 500 | provider key listing failed | retry/manual diagnosis |
| unsupported queue version | n/a | queue consumer rejects payload | preserve raw payload to DLQ; not accepted for delivery |
| Invalid JSON | n/a | payload cannot parse | preserve raw payload to DLQ |

## Timeout and retry policy

- HTTP client timeout: 10 seconds for alarm client.
- Queue drain: first item blocks up to consumer block timeout, then drains batches.
- Retry queue: delayed retry uses `alarm:dispatch:retry` sorted set and retry metadata (`attempt`, `retry_after_ms`, `next_visible_at`, `last_error`, optional `last_error_code`).
- `last_error_code` is one of `timeout`, `canceled`, `http_4xx`, `http_5xx`, `network`, `pg`, `payload`, `unknown`, or the recovery codes `lease_expired`, `stale_sending`, and `lease_released`. Existing consumers may ignore this optional field.
- DLQ: invalid raw payloads and moved envelopes are preserved in `alarm:dispatch:dlq`.

## Compatibility policy

- Queue consumers must retain dual-read behavior when introducing a new envelope version.
- Raw payload preservation must remain in place before changing DLQ tooling.
- HTTP provider migration must keep `hololive-api` admin-plane compatibility registration until the `hololive-api` bot/admin and dashboard paths are explicitly cut over to the `alarm-worker` provider.
- The two staged providers must register the same `/internal/alarm/*` route set and reuse the same shared handler implementation.
- compatibility facade 제거는 다음 조건을 모두 만족하는 별도 변경에서만 수행합니다: bot/admin/dashboard caller inventory가 alarm-worker endpoint로 수렴하고, `hololive-api`의 alarm route registration과 facade-only imports가 0건이며, alarm HTTP contract test와 architecture gate가 최종 tree에서 통과해야 합니다. 이 조건 전에는 route나 shared DTO를 선제 삭제하지 않습니다.

## Tests

- Contract constants: `hololive/hololive-shared/pkg/contracts/alarm/contracts_test.go`
- Queue fixtures: `hololive/hololive-shared/pkg/contracts/alarm/testdata/envelope_v1.json`, `envelope_unsupported_version.json`
- Queue behavior: `hololive/hololive-shared/pkg/service/alarm/queue/queue_test.go`
- HTTP handler/client: `hololive/hololive-shared/pkg/service/alarm/api_test.go`, `client_test.go`
- Shared alarm route registrar: `hololive/hololive-shared/pkg/service/alarm/routes_test.go`
- Member subscription HTTP roundtrip: `hololive/hololive-shared/pkg/service/alarm/member_subscription_api_test.go`
- Member selection and fail-open: `hololive/hololive-shared/pkg/service/alarm/member_subscriptions_test.go`

## Known gaps

- Alarm HTTP API DTOs are not yet represented by a dedicated `pkg/contracts/alarm` DTO package.
- `hololive-api` admin-plane compatibility registration remains until the consumer cutover PR removes it.
