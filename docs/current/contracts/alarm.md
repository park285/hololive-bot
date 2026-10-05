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

- HTTP provider: `alarm-worker` registers `hololive-shared/pkg/service/alarm.Handler` for `/internal/alarm/*` through the shared alarm route registrar.
- Domain owner: `alarm-worker`.
- The actual subscription service is `hololive/hololive-alarm-worker/internal/service/alarm/subscriptions`. The same instance supplies the local scheduler's target/cache-warm port and the HTTP handler's route-specific ports.
- `hololive-api` bot/admin planes require `ALARM_INTERNAL_URL` and construct worker clients. They do not register a second `/internal/alarm/*` provider or construct an in-process subscription service.
- Dispatch outbox service: `alarm-worker`
- Modules: `hololive-api`, `hololive-alarm-worker`, `hololive-shared`

## Consumers

- HTTP consumers: `hololive-api` bot/admin clients.
- Dispatch outbox consumer: `alarm-worker` (`dispatchoutbox.Consumer`).
- `alarm_state` read consumer: `hololive-api` — `alarms` 테이블을 다음 경로로 직접 읽습니다.
  - YouTube plane: `internal/planes/youtube/runtime/queries/notification_channel_ids.sql`을 projection transaction 안에서 실행합니다. `members` JOIN으로 졸업 멤버를 제외하고 `MaxInputChannelCount+1`로 상한을 둡니다.
  - llm plane membernews: `repository_query_0080_03.sql`이 방별 구독 멤버 이름을 읽습니다.
  - bot/admin plane은 `alarm.http`를 사용합니다. 통합 API의 `internal/config.RuntimeConfig.Validate`와 두 plane의 builder가 빈 `ALARM_INTERNAL_URL`을 거부합니다. URL 미설정 시 in-process service/repository를 주입하는 분기는 없습니다.
  - `pkg/service/alarm.Repository`는 `Add`/`Remove`/`ClearByRoom`을 함께 노출하므로 youtube-collector와 YouTube plane에는 주입하지 않습니다. `check-repository-ownership.sh`는 youtube-collector의 해당 import와 `alarm.NewRepository` 호출을 차단합니다.
- Usage: alarm CRUD/query, next stream lookup, settings updates, dispatch delivery

## Transport

- HTTP JSON for `/internal/alarm/*`
- PostgreSQL dispatch outbox (`alarm_dispatch_events`, `alarm_dispatch_deliveries`) for pending, retry, DLQ, quarantine, and terminal state. A payload-free Valkey wakeup list (`alarm:dispatch:wakeup`) only shortens polling; see [Valkey ephemeral contract](valkey_ephemeral_contract.md).
- The retired Redis dispatch queue keys (`alarm:dispatch:queue`, `alarm:dispatch:retry`, `alarm:dispatch:dlq`) have no reader or writer. Their reserved constants were removed in stack-audit 2026-09-26 T11.

### 관리자 실패 발송 취소·격리

Iris Console 발송 원장은 기존 재처리 외에 `POST /api/holo/dispatch/deliveries/:id/settle`로
`action=cancel|quarantine`, 필수 사유와 조회한 묶음 전체의 `{id, updatedAt}`를 받습니다.
Gateway는 운영자 ID를 인증 세션에서 결합합니다. `dlq`·`quarantined`인 최대 100건만
직렬화 트랜잭션에서 잠그고, 현재 리비전과 전체 구성 일치 후 상태·감사를 함께 커밋합니다.
발송 중·완료·취소된 묶음, 일부 대상, 오래된 리비전, 이미 전체 격리된 묶음의 재격리는 거절합니다.
이전 send unit 없는 실패 행도 단일 항목으로 처리할 수 있습니다.

취소는 `cancelled`로 종결하여 재처리를 막고, 격리는 `quarantined`로 보관하여 기존 수동
재처리 조건을 유지합니다. 두 작업 모두 외부 발송·자동 재실행 없이 식별자·실패 이력을 보존하며,
구독이나 다른 발송은 변경하지 않습니다. 감사에는 `manual_cancel|manual_quarantine`,
운영자·사유·변경 전후 상태를 남깁니다. 결과 불명은 상세·감사 재조회로 확인합니다.
기존 보존 기간은 유지하며 별도 DB migration은 필요하지 않습니다.

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
판정하며 과거에 예정 시각도 기록했던 `last_seen_at`은 사용하지 않습니다. 과거 관측 시각을
추정 backfill하지 않습니다. 확정된 `is_premiere=true`는 불변 분류이므로 선정·수신
시각과 무관하게 live 후보에서 제외합니다.

방송 checker와 생일 방송 후보 조회도 LIVE에는 `status_observed_at`, UPCOMING에는
`schedule_observed_at`을 사용합니다. 각 조회의 최신성 하한부터 현재 시각까지의 관측만
허용하며 NULL·미래 관측은 제외합니다. 예약 시각이나 오래된 `last_seen_at` 값으로
관측 시각을 대신하지 않습니다.
관측 조회의 현재 시각은 Holodex 응답이나 생일 멤버 조회를 기다린 뒤 DB 조회 직전에
다시 측정합니다. 주기 시작 시각·생일 날짜·분 전 알림 판정 창은 별도로 유지하여,
대기 중 수신된 정상 관측을 미래 시각으로 제외하지 않습니다.

제목과 예정 시각의 갱신 순서는 각각 `title_observed_at`·`schedule_observed_at`으로
독립 판정합니다. reducer와 DB UPSERT 모두 더 새로운 유효 관측만 반영하며 같은 시각의
충돌은 기존 값을 유지합니다. migration 257은 nullable 제목 관측 시각을 추가하고 기존
제목의 시각을 추정하지 않습니다. 새 API 실행 전 migration 적용이 필요합니다.

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
Major event/member news rows are produced in `notification_delivery_outbox`; `alarm-worker` claims those rows and sends them through Iris/Kakao. YouTube live/video/community/shorts rows are produced in `youtube_notification_outbox`; `alarm-worker` claims those rows, resolves rooms, renders with `internal/egress/youtubedispatch/format`, sends through Iris/Kakao, and writes per-room delivery state. Alarm dispatch uses the same formatter implementation directly from `internal/egress/alarmdispatch`.

Birthday stream notifications use `SourceKind=celebration`, `AlarmType=BIRTHDAY`, and `Celebration.Kind=birthday_stream`. Their recipient contract is the set of rooms whose matching `celebration:birthday:{channelID}:{date}` delivery is already `sent`; an audience lookup failure must not widen delivery to other rooms. Re-publishing a known birthday stream event is permitted so a newly eligible room can add its missing delivery through the existing event/delivery dedupe keys.

HTTP request DTOs are currently defined in `hololive/hololive-shared/pkg/service/alarm/dto.go` and the client-local request structs in `client.go`.

### UNIT B member subscriptions

구독 주체는 채팅방이다. `/add`와 `/remove`는 선택적인 `host_id`를 받으며,
`channel_id=UC3OH5FKQ3qtl4uRme_vZTgA`에서 `kiyosumi-lyra`, `reimei-mira`,
`yoinagi-neon`을 지원한다. 생략한 `host_id`는 기존 전체 채널 구독이다.
저장 식별자는 `(room_id, channel_id, host_id)`이며 멤버별 `alarm_types`를 따로 보존한다.
전체 채널 해지는 별도로 등록한 멤버 구독을 삭제하지 않는다.

채널별 구독 캐시는 해당 방의 전체·멤버별 구독 합집합이다. UNIT B의 최종 수신 방은
`alarm.SubscriberResolver.ResolveEventSubscribers`가 DB 구독 행과 제목으로 결정한다. 진행자가 확인되면
해당 멤버와 전체 채널 구독 방을, 미상이면 해당 알림 종류를 구독한 모든 UNIT B 방을 포함한다.
공동 진행자는 합집합이며 방 ID는 중복을 제거한다. 외부 유닛의 게스트를 UNIT B 진행자로
취급하지 않는다. 구독 DB 오류나 손상된 outbox payload는 이 fail-open의 대상이 아니다.

`alarm-worker` YouTube checker의 채널별 LIVE 구독 방은 `alarm:channel_subscribers:{channel}` set을 먼저 읽는다.
set이 비어 있으면 `alarm:channel_subscribers_empty:LIVE:{channel}` marker(30초)가 있는 채널만 구독 0으로 본다.
marker가 없는 채널은 set 유실(eviction 등)로 보고 해당 주기의 미확정 채널을 한 번의 DB 조회로 확정한다.
`SubscriberResolver`는 cache·DB pool별로 생성하여 재사용한다. 같은 resolver의 동일 채널·종류 조회만 singleflight로 합치며 다른 DB의 결과·오류는 공유하지 않는다. 호출자 취소는 해당 대기만 끝내고, 공유 DB 조회는 기존 5초 예산으로 끝난다.

`ResolveChannelSubscribersByType`와 `ResolveUncachedChannelSubscribersByType` 메서드의 DB 결과는 이번 조회에만 사용하며 set이나 빈 구독 marker를 쓰지 않는다. 늦은 SADD가 해지를 되돌리거나 늦은 marker가 새 구독을 숨기는 경합을 방지한다. 유실된 set은 다음 전체 rebuild까지 DB에서 확인하며, checker는 주기마다 미확정 채널의 empty marker를 한 pipeline으로 확인한 뒤 batch DB 조회 1회로 확정한다.
set 조회 오류와 이 DB 조회 오류는 해당 check 주기 오류로 반환하며, 확정하지 못한 채널을 구독 0으로 기록하지 않는다.

구독 추가·종류 변경·삭제·전체 해지는 DB 변경 전에 영향받는 종류의 빈 구독 marker를
먼저 지우고 positive set을 무효화합니다. 무효화를 확인하지 못하면 DB를 변경하지 않습니다.
추가는 DB 변경 전에 채널을 registry에 등록하고 전역 빈 구독 marker도 지웁니다.
종류별 set은 부분 갱신하지 않으며, 다음 전체 rebuild까지 기존 DB read-through를 사용합니다.
변경과 전체 rebuild는 같은 mutation mutex로 직렬화한다. rebuild는 `ScanKeyPages`로 받은 페이지를 즉시 삭제하고, 모든 삭제가 끝난 뒤 DB snapshot을 적재한다. 키 전체를 메모리에 모으지 않으며 SCAN COUNT는 서버의 hint이므로 실제 페이지 크기는 달라질 수 있다. 이 직렬화는 단일 worker를 전제로 하며, replica를 늘리기 전에는 프로세스 간 변경·rebuild fence가 필요하다.

DB commit 뒤 캐시 후처리와 기존 실패 복구는 요청 취소와 분리된 최대 5초 context를
사용합니다. 실패는 호출자에게 반환하되 DB 변경을 되돌리지 않습니다. 무효화된 set은
이후 조회에서 DB로 확인하므로 기존 positive set이 추가·해지를 가리지 않습니다.
새 영속 상태·재시도 큐·별도 캐시 복구 루프는 없습니다.

구독 삭제 뒤 채널 registry 유지 여부는 캐시 원소 수가 아니라 DB의 잔여 구독으로 판단합니다.
유실된 registry는 전체 DB 구독을 먼저 복구한 다음 신규 채널을 등록합니다.
DB에 구독 채널이 없을 때만 신규 채널 하나로 registry를 초기화하며, 복구 직후 다시
유실되면 DB 변경 전에 실패합니다. DB 미반영 채널이 일시적으로 registry에 남아도
실제 수신자는 DB 구독으로 제한됩니다.

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
| Telemetry | `hololive_youtube_outbox_member_name_missing_total`(YouTube outbox 알림), `hololive_alarm_dispatch_member_name_missing_total`(방송·X 스페이스 알림)이 종단 문구로 알림을 만든 횟수 |
| Owner | hololive-bot alarm(`hololive-shared/pkg/service/alarm`의 `GetMemberName`, `hololive-alarm-worker/internal/egress/youtubedispatch/format`의 `DisplayMemberName`, `hololive-alarm-worker/internal/egress/alarmdispatch`의 `alarmContractMemberName`) |
| 검토 조건 | 지표가 0이 아니면 해당 채널의 members 한국어 표시명을 등록한다. 90일 동안 0이면 종단 문구 대신 포맷 실패로 바꿀지 다시 결정한다 |

코드 근거는 `alarm.Repository.GetMemberName` 주석과 `queries/repository_0155_07.sql`, `queries/repository_0231_10.sql`이다.

YouTube outbox dispatch의 `MemberNameSource`는 표시명을 Valkey `alarm:member_names`에서 읽지 않고 메시지마다 `alarm.Repository.GetMemberName`으로
PostgreSQL 정본을 조회한다(2026-10-02). 조회 오류는 이 예외 계약의 trigger가 아니다. 대체 문구로 보내지 않고
재시도 가능한 `format_message` 실패로 전이하며, grouped 발송이면 group 전체를 같은 실패로 전이한다. 조회 결과가 빈
문자열일 때만 `misc/vtuber_fallback` 문구를 쓴다.

방송(live·upcoming) 알림과 X 스페이스 시작 알림도 같은 계약을 따른다(2026-10-05). alarm dispatch 렌더가 `channel_id`로
members를 조회하며, Holodex 채널 제목·Valkey 이름 캐시·X 스페이스 설정의 이름은 쓰지 않는다. 조회 오류는 렌더 실패(발송 전 재시도)다.
X 스페이스 payload(`x_space_starts`, dispatch 원장)는 이름을 담지 않는다.

`alarms.member_name`은 2026-10-05부터 읽거나 쓰지 않는다. 알람 목록은 이름 캐시에 없는 채널을 members
(`short_korean_name`→`korean_name`→`english_name`)로 채우고, members에도 없으면 채널 ID를 보여 준다. 멤버 뉴스 구독 이름도
members에서만 읽는다. 1단계 배포를 확인한 뒤 migration 269가 컬럼과 읽지 않는 `misc/alarm_unknown_member` 문구를 지웠다.

### 명령 응답 멤버 표시명

봇 명령 응답(`!라이브`, `!예정`, `!일정`, 알람 추가·해제, 방송기록, 동명이인 후보, 캘린더)은 members 정본
`short_korean_name`→`korean_name`→`english_name` 순서로 멤버 이름을 표시한다(2026-10-05). 알림의 위 예외 계약과 달리
`english_name`까지 쓰며 종단 문구는 없다. 담당은 `domain.Member.DisplayName`이고, SQL로 이름을 만드는 `!라이브` 목록
(`livequery/queries/snapshot.sql`)과 방송기록(`broadcast_history_repository_0179_01.sql`)이 같은 순서를 쓴다.

- 식별·검색·dedup은 `channel_id`와 matcher 검색 키로 한다. 동명이인 안내의 복사 예시는 검색 키 형식
  `english_name (그룹)`(`domain.Member.QualifiedName`)을 유지한다.
- `!예정`은 Holodex·공식 일정 응답의 채널 이름 대신, 표시 직전에 `channel_id`로 결합한 members 표시명을 쓴다.
  members에 없는 채널만 응답 이름을 그대로 쓴다. Holodex 캐시 데이터는 바꾸지 않으며, members 조회가 실패하면
  원천 이름으로 보내지 않고 예정 조회 실패로 응답한다.
- mekPark 호스트처럼 members 밖 대상은 이 규칙의 대상이 아니다.

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

이 예외는 이름 조회가 성공했으나 값이 없는 경우에만 적용합니다. YouTube 단건·묶음 formatter는 이름 저장소 조회 실패를 포맷 오류로 반환하며 대체 표시명으로 성공을 만들지 않습니다.

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
- HTTP/H3 options are passed explicitly from the consuming plane's loaded `BotPlaneConfig.InternalH3` or `AdminPlaneConfig.InternalH3`. The client constructor does not reread the environment, and failed HTTPS/H3 configuration is a startup error. Each plane owns transport cleanup.
- Dispatch claim: the consumer claims due `pending`/`retry` deliveries under a row lease, woken by `alarm:dispatch:wakeup` or its poll interval.
- Retry: a failed delivery returns to `retry` with `next_attempt_at`; the claimed envelope carries retry metadata (`attempt`, `last_error`, optional `last_error_code`) from the delivery row.
- `last_error_code` is one of `timeout`, `canceled`, `http_4xx`, `http_5xx`, `network`, `pg`, `payload`, `unknown`, or the recovery codes `lease_expired`, `stale_sending`, and `lease_released`. Existing consumers may ignore this optional field.
- DLQ and quarantine are delivery states (`dlq`, `quarantined`) in `alarm_dispatch_deliveries`; the event payload stays in `alarm_dispatch_events`. Replay uses the audited requeue in [admin dispatch operations](../runbooks/admin-dispatch-operations.md).

## Compatibility policy

- A new envelope version requires the dispatch consumer to decode both versions before any producer emits it; the publisher currently accepts only `QueueEnvelopeVersionV1`.
- Stored event payloads of `dlq`/`quarantined` deliveries must stay intact before changing replay tooling.
- Existing `/internal/alarm/*` routes and shared DTOs are preserved with `alarm-worker` as their provider. API bot/admin clients use that provider; admin-facing `/api/holo/*` routes remain API-owned.

## Tests

- Contract constants: `hololive/hololive-shared/pkg/contracts/alarm/contracts_test.go`
- Envelope fixtures: `hololive/hololive-shared/pkg/contracts/alarm/testdata/envelope_v1.json`, `envelope_unsupported_version.json`
- Publish validation: `hololive/hololive-alarm-worker/internal/service/alarm/queue/queue_test.go`
- Dispatch outbox and canonical clock: `hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/*_test.go`
- Actual subscription service and worker/client roundtrip: `hololive/hololive-alarm-worker/internal/service/alarm/subscriptions/*_test.go`, including `client_advance_integration_test.go` without a build tag.
- Alarm runner and formatter: `hololive/hololive-alarm-worker/internal/egress/alarmdispatch/*_test.go`, `hololive/hololive-alarm-worker/internal/egress/youtubedispatch/format/*_test.go`
- HTTP handler/client: `hololive/hololive-shared/pkg/service/alarm/api_test.go`, `client_test.go`
- Shared alarm route registrar: `hololive/hololive-shared/pkg/service/alarm/routes_test.go`
- Member subscription HTTP roundtrip: `hololive/hololive-shared/pkg/service/alarm/member_subscription_api_test.go`
- Member selection and fail-open: `hololive/hololive-shared/pkg/service/alarm/member_subscriptions_test.go`

## Known gaps

- Alarm HTTP API DTOs are not yet represented by a dedicated `pkg/contracts/alarm` DTO package.
