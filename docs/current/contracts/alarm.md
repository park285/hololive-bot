# Contract: alarm

## Summary

Alarm domain currently has HTTP JSON APIs, the PostgreSQL alarm dispatch outbox, generic notification delivery outbox egress, and the YouTube notification outbox egress path owned by `alarm-worker`.

X 스페이스 시작은 `source_kind=x_space`와 `x_space` payload로 저장한다. 기존 `LIVE` 구독을 사용하되 YouTube stream payload와 섞지 않는다. 이벤트 키는 `x-space:start:<space-id>`, delivery는 기존 방별 키이며 최초 관측 스냅샷을 사용해 제목 변경에 따른 payload 충돌을 막는다. 발송은 기존 텍스트 egress와 receipt·미상 결과 계약을 따른다. [인증·관측·보존 경계](../services/x-spaces.md).

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
- Dispatch outbox service: `alarm-worker`
- Modules: `hololive-api`, `hololive-alarm-worker`, `hololive-shared`

## Consumers

- HTTP consumers: `hololive-api` (bot + admin-plane facade paths)
- Dispatch outbox consumer: `alarm-worker` (`dispatchoutbox.Consumer`).
- `alarm_state` read consumer: `hololive-api` — `alarms` 테이블을 다음 경로로 직접 읽습니다.
  - YouTube plane: `internal/planes/youtube/runtime/queries/notification_channel_ids.sql`을 projection transaction 안에서 실행합니다. `members` JOIN으로 졸업 멤버를 제외하고 `MaxInputChannelCount+1`로 상한을 둡니다.
  - llm plane membernews: `repository_query_0080_03.sql`이 방별 구독 멤버 이름을 읽습니다.
  - bot/admin plane은 `alarm.http`를 사용합니다. 통합 API의 `apiplane.RuntimeConfig.Validate`가 빈 `ALARM_INTERNAL_URL`을 거부하므로, bootstrap에 남은 `AlarmServiceURL` 미설정 시 in-process `pkg/service/alarm.Repository` 주입 분기는 정상 기동에서 도달할 수 없고 운영 직접 읽기 계약에 포함하지 않습니다.
  - `pkg/service/alarm.Repository`는 `Add`/`Remove`/`ClearByRoom`을 함께 노출하므로 youtube-collector와 YouTube plane에는 주입하지 않습니다. `check-repository-ownership.sh`는 youtube-collector의 해당 import와 `alarm.NewRepository` 호출을 차단합니다.
- Usage: alarm CRUD/query, next stream lookup, settings updates, dispatch delivery

## Transport

- HTTP JSON for `/internal/alarm/*`
- PostgreSQL dispatch outbox (`alarm_dispatch_events`, `alarm_dispatch_deliveries`) for pending, retry, DLQ, quarantine, and terminal state. A payload-free Valkey wakeup list (`alarm:dispatch:wakeup`) only shortens polling; see [Valkey ephemeral contract](valkey_ephemeral_contract.md).
- The retired Redis dispatch queue keys (`alarm:dispatch:queue`, `alarm:dispatch:retry`, `alarm:dispatch:dlq`) have no reader or writer. Their reserved constants were removed in stack-audit 2026-09-26 T11.

### Iris Markdown admission

Markdown admission retains the exact Iris request ID and polls its reply status.
Only `handoff_completed` succeeds; confirmed failure (`sendoutcome.ErrHandoffFailed`) and
an unknown or timed-out outcome (`sendoutcome.ErrHandoffOutcomeUnknown`) retain their
distinct failure/claim semantics.

Alarm-worker does not send Karing templates
(`DEC-20260926-hololive-karing-egress-disposition`, superseding
`DEC-20260904-hololive-karing-regular-chat-egress`). The Karing chunk planner,
per-chunk request IDs, and the `karing.kakaolink` contract were removed with it.
Alarm dispatch pins each send unit's final body, text/markdown route, membership,
base ID and generation before `BeginSending`. Retries and restarts reuse that request.
Transport ambiguity may retry with the stored ID and identical request for every
source kind; structured `OUTCOME_UNKNOWN` and unknown handoff remain quarantined.
Only confirmed pre-handoff `CLIENT_REQUEST_ID_FAILED` permits atomic generation/retry
advance to SDK r1/r2 within the existing retry budget. Past Karing `SENDING`/`outcome_unknown` rows keep the existing
quarantine and stale-sweeper contract (T18 2026-09-26: 0 non-terminal rows).

Community/shorts authorization leases use acquisition wall-clock UTC (PostgreSQL
microsecond precision). Event detection, creation and next-attempt timestamps do
not determine the lease epoch. Stale recovery releases only the exact old token.

An existing event key with a different payload hash is a collision, not a content
substitution. The transaction records the collision and admits only delivery
entries matching the committed event payload; matching entries in the same batch
remain eligible. Rejected entries do not affect send-unit boundaries or request
IDs. `InsertBatch` exposes `HashConflictEvents` and per-input `Receipts` with ordinal,
delivery key and committed outcome. Only inserted, duplicate-active and duplicate-SENT
receipts satisfy publishing; collision and other terminal outcomes do not. A later chunk
failure preserves earlier committed receipts. `InsertPending` returns
`ErrEventPayloadConflict` and no existing record for a conflicting single input.

선정된 upcoming 알람의 방별 payload/category는 `alarm_upcoming_candidates`에 채널 평가
checkpoint와 함께 저장합니다. staging commit부터 복구를 보장하며 예정 시작 이후에는
분 전 알람을 만료합니다. 확인된 최신 일정 변경·방송 종료·구독 해제는 사유 종료하고
목록 누락만으로 취소하지 않습니다. 발행 성공은 방별 receipt로 확인하여 다른 방의
미발행을 방송 전체 dedup으로 가리지 않습니다. 기존 target minutes와 조회 lookback은 유지합니다.
Canonical 상태·일정의 최신성은 실제 `status_observed_at`·`schedule_observed_at`으로만
판정하며 예정 시각도 포함하는 `last_seen_at`은 사용하지 않습니다. 과거 관측 시각을
추정 backfill하지 않습니다. 확정된 `is_premiere=true`는 불변 분류이므로 선정·수신
시각과 무관하게 live 후보에서 제외합니다.

`PUT /room-name` stores the admin-assigned room display name in PostgreSQL
`alarm_room_display_names` (migration 232). `room_name` is required; a blank value
clears the admin name. Both fields are trimmed; a blank `room_id`, a `room_id` over 100
characters, or a `room_name` over 255 characters (rune count, matching the PG varchar
widths) is rejected with 400 `invalid_request_body` before storage. The admin API
`POST /api/holo/names/room` applies the same bounds and answers 400 without calling the worker.
`GET /keys` builds the admin list from PostgreSQL `alarms`,
one entry per distinct `(room_id, channel_id)` (UNIT B host rows collapse into one),
and resolves `roomName` as admin name, then the non-empty `alarms.room_name` most recently
changed by upsert (`room_name_updated_at`, then `id`),
then the room ID. Alarm re-registration and subscriber cache rebuild never change the
admin name. The user-name API was removed; `alarms.user_name` is still stored.

Persisted target minutes are explicit policy. `[5, 1]` remains `[5, 1]`, and a
persisted single target is not expanded. Runtime defaults are generated only when
the stored target list is absent/empty under the existing settings contract.

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| HTTP paths | `/internal/alarm/add`, `/remove`, `/room/:id`, `/room/:id/view`, `/clear`, `/settings`, `/room-name`, `/keys` |
| Dispatch storage | `alarm_dispatch_events`, `alarm_dispatch_deliveries`; wakeup list `alarm:dispatch:wakeup` |
| Method | mixed HTTP methods; PostgreSQL batch insert and leased claim; Valkey `LPUSH` wakeup token |
| Version | HTTP unversioned; envelope `QueueEnvelopeVersionV1 = 1`; the publisher rejects any other version, including a missing (`0`) version |
| Contract package | `hololive/hololive-shared/pkg/contracts/alarm`; HTTP DTOs remain under `hololive/hololive-shared/pkg/service/alarm` |
| Envelope fixtures | `hololive/hololive-shared/pkg/contracts/alarm/testdata/envelope_v1.json`, `envelope_unsupported_version.json` |

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

`alarm-worker` YouTube checker의 채널별 LIVE 구독 방은 `alarm:channel_subscribers:{channel}` set을 먼저 읽는다.
set이 비어 있으면 `alarm:channel_subscribers_empty:LIVE:{channel}` marker(30초)가 있는 채널만 구독 0으로 본다.
marker가 없는 채널은 set 유실(eviction 등)로 보고 해당 주기의 미확정 채널을 한 번의 DB 조회로 확정한다.
`ResolveChannelSubscribersByType`와 `ResolveUncachedChannelSubscribersByType`의 DB 결과는 이번 조회에만 사용하며 set이나 빈 구독 marker를 쓰지 않는다. 늦은 SADD가 해지를 되돌리거나 늦은 marker가 새 구독을 숨기는 경합을 방지한다. 유실된 set은 다음 rebuild나 구독 변경이 채울 때까지 DB에서 확인하며, checker는 주기마다 미확정 채널을 batch 조회 1회로 확정한다.
set 조회 오류와 이 DB 조회 오류는 해당 check 주기 오류로 반환하며, 확정하지 못한 채널을 구독 0으로 기록하지 않는다.

신규 구독을 허용하기 전에 migration 194–196, 양쪽 HTTP provider, worker의 대상 선정 코드를
함께 전환해야 한다. 196 이후에는 이전 `(room_id, channel_id)` upsert를 실행할 수 없다.
운영 전환 조건과 검증은 [변경 보고서](../../review/unit-b-member-subscriptions-20260906.md)에 있다.

### 멤버 표시명 예외 계약

`DEC-20260926-hololive-source-fallbacks-retirement`는 계약 없는 원천·표시 폴백을 오류 반환 단일 경로로 바꾸고,
알림 멤버 표시명 폴백 하나만 예외로 남겼다. 2026-10-02에 제거 조건을 확인했다. 두 지표(`hololive_alarm_member_name_fallback_channels`,
`hololive_alarm_member_name_caller_fallback_total`)가 30일 동안 0이었고, 운영 DB 읽기 전용 조회에서 구독 채널 21개 모두
members 한국어 표시명을 가졌다. 그래서 중간 단계(최신 `alarms.member_name`, alarm cache 기록 때 호출자 값)와 두 지표를 지웠다.
남은 것은 표시 단계의 종단 문구다.

| 항목 | 계약 |
|---|---|
| Trigger | `members`의 `short_korean_name`·`korean_name`이 모두 비었거나 채널 행이 없음 |
| 순서 | members(`short_korean_name`→`korean_name`) → 표시 단계 `misc/vtuber_fallback` 문구(종단) |
| 한도 | 표시 전용. 식별·dedup·라우팅에 쓰지 않고 외부 호출·재시도가 없음. 조회 오류는 trigger가 아님 |
| Telemetry | `hololive_youtube_outbox_member_name_missing_total`(alarm-worker가 종단 문구로 YouTube 알림을 만든 횟수) |
| Owner | hololive-bot alarm(`hololive-shared/pkg/service/alarm`의 `GetMemberName`, `hololive-alarm-worker/internal/service/youtube/outbox/format`의 `DisplayMemberName`) |
| 검토 조건 | 지표가 0이 아니면 해당 채널의 members 한국어 표시명을 등록한다. 90일 동안 0이면 종단 문구 대신 포맷 실패로 바꿀지 다시 결정한다 |

코드 근거는 `alarm.Repository.GetMemberName` 주석과 `queries/repository_0155_07.sql`, `queries/repository_0231_10.sql`이다.

YouTube outbox dispatch는 표시명을 Valkey `alarm:member_names`에서 읽지 않고 메시지마다 `alarm.Repository.GetMemberName`으로
PostgreSQL 정본을 조회한다(2026-10-02). 조회 오류는 이 예외 계약의 trigger가 아니다. 대체 문구로 보내지 않고
재시도 가능한 `format_message` 실패로 전이하며, grouped 발송이면 group 전체를 같은 실패로 전이한다. 조회 결과가 빈
문자열일 때만 `misc/vtuber_fallback` 문구를 쓴다.

### Live catchup 억제 marker의 실패 처리

upcoming 알림의 최근 전송 marker는 추가 catchup을 줄이는 보조 증거입니다. marker를 읽지 못한 사실을 이미 알림을 받았다는 증거로 쓰지 않습니다. 다음은 기존 동작과 `TestFilterLiveCatchupSuppressedRoomsFailsOpenOnCacheError`·`TestFilterLiveCatchupSuppressedRoomsFailsOpenOnInvalidMarker`가 재현하는 예외입니다.

| 항목 | 계약 |
|---|---|
| Trigger | upcoming 억제 marker의 캐시 조회 오류 또는 `notified_at` 형식 오류 |
| 한도 | 해당 LIVE_STREAM outbox의 기존 구독 방에만 적용합니다. 정상 marker의 억제 창은 `LiveCatchupSuppressWindow` 15분이며, 이 예외가 새 수집·재시도·수신 방을 만들지 않습니다. |
| 종단 동작 | 억제를 적용하지 않고 기존 delivery 원장·멱등성·발송 상태 전이를 따릅니다. 별도 upcoming 알림 뒤 catchup 알림이 추가될 수 있습니다. |
| Telemetry | `hololive_youtube_outbox_live_catchup_suppression_total{result="cache_error"}` 또는 `result="invalid_marker"`와 기존 Warn 로그 |
| Owner | alarm-worker의 YouTube OutboxGrouper |
| 재검토 조건 | 억제 증거 저장소 변경, 중복 upcoming/catchup 사례 확인, 또는 delivery 원장만으로 억제를 판정할 수 있게 될 때 이 예외를 재검토합니다. |

2026-10-02 운영 조회에서 이 metric의 시계열이 30일 동안 없었습니다(억제·오류 모두 0회). 같은 기간 관련 Warn 로그도 0건이었습니다.

### Holodex 실패 시 persisted live session 계속 예외 계약

live checker는 Holodex live 상태와 collector가 저장한 `youtube_live_sessions`를 매 주기 함께 읽어 합친다. Holodex가
실패하면 저장된 세션만으로 그 주기를 계속한다. collector가 이미 관측한 방송의 시작 알림을 Holodex 장애 때문에 놓치지 않기
위한 것이다(2026-10-02 계약화). 2026-09-30 Holodex 장애(요청 timeout과 DNS 조회 timeout) 동안 이 경로가 108회
실행됐고 그중 101회가 주기 실패로 끝났다. 54회는 Holodex 재시도가 check 주기 예산(45초)을 모두 써서 저장 세션
조회가 기한을 넘긴 경우였다. 그 뒤로 Holodex 조회는 주기 안에서 자체 한도를 가진다.

| 항목 | 계약 |
|---|---|
| Trigger | 이번 주기의 Holodex `GetChannelsLiveStatus` 오류 |
| 한도 | Holodex 조회는 주기 안에서 최대 25초(`youtubeHolodexLiveStatusBudget`)이며, 나머지 예산을 저장 세션 조회와 후속 단계에 남긴다. 이번 주기의 due 채널에서 최근 15분 안에 LIVE로 관측된 세션과, 최근 15분 안에 관측됐고 30분 안에 시작할 UPCOMING 세션만 쓴다. Holodex 응답이 없으므로 provider 응답으로 후보를 취소하지 않는다. 추가 외부 호출·재시도는 없다. |
| 종단 | persisted source가 없거나, 세션 조회가 실패했거나, 세션이 0건이면 Holodex 오류를 그 check 주기의 오류로 반환한다. |
| Telemetry | `hololive_alarm_youtube_persisted_live_sessions_total{result="holodex_error_continued",status="all"}`와 Warn 로그 "YouTube Holodex live status source failed; continuing with persisted live sessions" |
| Owner | hololive-bot alarm-worker live checker(`internal/service/alarm/checker/checking`) |
| 검토 조건 | Holodex 없이 live 상태를 판정하는 단일 원천이 생기면 이 예외를 지운다. `holodex_error_continued`가 늘어나는데 알림 누락 보고가 있으면 한도(15분·30분)를 다시 검토한다. |

## Response

```go
type APIResponse struct {
    Success bool        `json:"success"`
    Error   string      `json:"error,omitempty"`
    Message string      `json:"message,omitempty"`
    Data    interface{} `json:"data,omitempty"`
}
```

Dispatch publish has no response body; delivery outcome is represented by delivery row state, retry metadata, claim release, and dispatcher logs/metrics.

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
| `set_room_name_failed` | 500 | provider room name update failed | retry/manual diagnosis |
| `get_all_alarm_keys_failed` | 500 | provider key listing failed | retry/manual diagnosis |
| unsupported envelope version | n/a | publisher rejects the batch before insert | fix the producer; nothing is stored |
| Invalid stored payload | n/a | consumer cannot decode the event payload or delivery context | delivery moves to `dlq` with the decode error |

## Timeout and retry policy

- HTTP client timeout: 10 seconds for alarm client.
- Dispatch claim: the consumer claims due `pending`/`retry` deliveries under a row lease, woken by `alarm:dispatch:wakeup` or its poll interval.
- Retry: a failed delivery returns to `retry` with `next_attempt_at`; the claimed envelope carries retry metadata (`attempt`, `last_error`, optional `last_error_code`) from the delivery row.
- `last_error_code` is one of `timeout`, `canceled`, `http_4xx`, `http_5xx`, `network`, `pg`, `payload`, `unknown`, or the recovery codes `lease_expired`, `stale_sending`, and `lease_released`. Existing consumers may ignore this optional field.
- DLQ and quarantine are delivery states (`dlq`, `quarantined`) in `alarm_dispatch_deliveries`; the event payload stays in `alarm_dispatch_events`. Replay uses the audited requeue in [admin dispatch operations](../runbooks/admin-dispatch-operations.md).

## Compatibility policy

- A new envelope version requires the dispatch consumer to decode both versions before any producer emits it; the publisher currently accepts only `QueueEnvelopeVersionV1`.
- Stored event payloads of `dlq`/`quarantined` deliveries must stay intact before changing replay tooling.
- HTTP provider migration must keep `hololive-api` admin-plane compatibility registration until the `hololive-api` bot/admin and dashboard paths are explicitly cut over to the `alarm-worker` provider.
- The two staged providers must register the same `/internal/alarm/*` route set and reuse the same shared handler implementation.
- compatibility facade 제거는 다음 조건을 모두 만족하는 별도 변경에서만 수행합니다: bot/admin/dashboard caller inventory가 alarm-worker endpoint로 수렴하고, `hololive-api`의 alarm route registration과 facade-only imports가 0건이며, alarm HTTP contract test와 architecture gate가 최종 tree에서 통과해야 합니다. 이 조건 전에는 route나 shared DTO를 선제 삭제하지 않습니다.

## Tests

- Contract constants: `hololive/hololive-shared/pkg/contracts/alarm/contracts_test.go`
- Envelope fixtures: `hololive/hololive-shared/pkg/contracts/alarm/testdata/envelope_v1.json`, `envelope_unsupported_version.json`
- Publish validation: `hololive/hololive-alarm-worker/internal/service/alarm/queue/queue_test.go`
- Dispatch outbox: `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/*_test.go`
- HTTP handler/client: `hololive/hololive-shared/pkg/service/alarm/api_test.go`, `client_test.go`
- Shared alarm route registrar: `hololive/hololive-shared/pkg/service/alarm/routes_test.go`
- Member subscription HTTP roundtrip: `hololive/hololive-shared/pkg/service/alarm/member_subscription_api_test.go`
- Member selection and fail-open: `hololive/hololive-shared/pkg/service/alarm/member_subscriptions_test.go`

## Known gaps

- Alarm HTTP API DTOs are not yet represented by a dedicated `pkg/contracts/alarm` DTO package.
- `hololive-api` admin-plane compatibility registration remains until the consumer cutover PR removes it.
