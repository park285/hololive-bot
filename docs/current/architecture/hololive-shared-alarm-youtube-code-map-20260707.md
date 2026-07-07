# Hololive Shared Alarm and YouTube Detection Code Map

작성일: 2026-07-07

이 문서는 `hololive-bot`의 알람 로직을 실제 코드 파일 기준으로 정리한다. 범위는 사용자의 알람 명령, `hololive-shared`의 구독/큐/dispatch outbox 코드, `hololive-alarm-worker`의 YouTube 감지 코드, 그리고 최종 발송 worker까지다.

핵심 결론은 다음과 같다.

- 사용자의 `!알람` 명령은 `hololive-api`에서 파싱되고, 구독 상태는 PostgreSQL의 `alarms` 테이블에 저장된다.
- YouTube 감지는 `hololive-alarm-worker/internal/service/alarm/checker/internal/checking` 아래의 `YouTubeChecker`가 담당한다.
- 감지된 후보 알림은 `notifier`가 Valkey dedup claim을 먼저 잡고, `hololive-shared/pkg/service/alarm/queue.Publisher`가 PostgreSQL dispatch outbox에 기록한다.
- 실제 외부 발송은 `alarm_dispatch_runner`가 `alarm_dispatch_deliveries`를 claim하고, `MarkSending` 조건부 UPDATE를 통과한 뒤 Iris/Kakao로 보낸다.
- Valkey는 channel registry, subscriber set, dedup claim, wakeup 신호, lease 등 빠른 조율을 맡고, PostgreSQL은 outbox와 delivery 상태의 최종 진실을 맡는다.

## 1. 큰 흐름

```text
Kakao room command
  -> hololive-api command parser
  -> AlarmCommand
  -> hololive-shared alarm.Repository
  -> PostgreSQL alarms
  -> Valkey alarm registry/subscriber cache

alarm-worker YouTube checker loop
  -> YouTubeChecker.Check
  -> load due channels from Valkey + tier scheduler
  -> Holodex live status + persisted youtube_live_sessions fallback
  -> upcoming/live catchup candidate selection
  -> AlarmNotification per room
  -> notifier claimDedup
  -> queue.Publisher PublishBatch
  -> PostgreSQL alarm_dispatch_events / alarm_dispatch_deliveries
  -> Valkey wakeup

alarm dispatch runner
  -> dispatchoutbox.Consumer DrainBatch / ClaimDue
  -> group and render
  -> MarkSending
  -> Iris SendMessage / SendKaringContentList
  -> MarkSent or retry/dlq/quarantine
```

## 2. 사용자 알람 명령 경로

### 2.1 명령어 파싱

파일:

- `hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/message_parser_alarm.go`

주요 함수:

- `tryAlarmCommand(command, args, raw)`
- `isAlarmCommand(command)`
- `parseAlarmCommand(args, raw)`
- `extractMemberAndType(parts)`
- `normalizeCompactAlarmTokens(command, args)`

이 파일은 카카오톡 메시지에서 알람 명령을 정규화한다.

예:

```text
!알람 페코라
!알림설정 페코라 방송
!알람 삭제 페코라
!알람리스트
```

`isAlarmCommand`는 `알람`, `알림`, `알림설정`, `알람설정`, `alarm` 같은 루트 명령을 알람 명령으로 본다.

`compactAlarmCommandMapping`은 붙어 있는 축약 명령을 표준 형태로 바꾼다.

```text
알람설정 -> 알람 설정
알림삭제 -> 알람 삭제
알람리스트 -> 알람 리스트
```

`extractMemberAndType`은 마지막 토큰이 타입 키워드인지 확인한다.

```text
방송 / 라이브 / live       -> LIVE
커뮤니티 / community       -> COMMUNITY
쇼츠 / shorts              -> SHORTS
전체 / all                 -> all alarm types
```

결과적으로 parser는 `AlarmCommand`가 이해할 수 있는 `action`, `member`, `type` 값을 만든다.

### 2.2 명령어 실행

파일:

- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/handler_alarm.go`
- `hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd/command_execution_policy.go`

주요 함수:

- `AlarmCommand.Execute`
- `handleAdd`
- `handleRemove`
- `handleList`
- `handleClear`
- `resolveAlarmMember`
- `parseAlarmTypes`
- `ShouldExecuteAsync`

`AlarmCommand.Execute`는 `action`에 따라 add/remove/list/clear를 분기한다.

`handleAdd` 흐름:

```text
member 파라미터 확인
  -> parseAlarmTypes
  -> resolveAlarmMember
  -> graduated 멤버 차단
  -> Deps().Alarm.AddAlarm
  -> 다음 방송 정보 조회
  -> FormatAlarmAdded
  -> SendMessage
```

`resolveAlarmMember`는 member matcher를 사용한다. 후보가 여러 명이면 `matcher.AmbiguousMatchError`를 받아 `FormatAmbiguousMembers`로 사용자에게 후보 목록을 보낸다. 하나도 못 찾으면 member-not-found 응답으로 끝난다.

`parseAlarmTypes`는 타입 문자열을 `domain.AlarmType` 배열로 바꾼다. 타입이 없으면 `domain.DefaultAlarmTypes`를 쓴다.

상태를 바꾸는 알람 명령은 async로 흘리지 않는다. `command_execution_policy.go`의 `ShouldExecuteAsync`는 `AlarmAdd`, `AlarmRemove`, `AlarmList`, `AlarmClear`, `AlarmInvalid`를 동기 처리 대상으로 둔다. 이유는 같은 방에서 알람 추가/삭제가 섞일 때 처리 순서를 방 단위로 보존하기 위해서다.

## 3. hololive-shared 구독 저장 코드

### 3.1 Repository

파일:

- `hololive/hololive-shared/pkg/service/alarm/repository.go`
- `hololive/hololive-shared/pkg/service/alarm/queries/repository_0055_01.sql`
- `hololive/hololive-shared/pkg/service/alarm/queries/targets_0188_01.sql`

`Repository.Add`는 사용자의 구독을 PostgreSQL `alarms` 테이블에 upsert한다.

핵심 SQL:

```sql
INSERT INTO alarms (room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (room_id, channel_id) DO UPDATE
SET member_name = COALESCE(EXCLUDED.member_name, alarms.member_name),
    room_name = COALESCE(EXCLUDED.room_name, alarms.room_name),
    user_name = COALESCE(EXCLUDED.user_name, alarms.user_name),
    user_id = EXCLUDED.user_id,
    alarm_types = EXCLUDED.alarm_types
```

중요한 점:

- unique key는 `(room_id, channel_id)`다.
- 같은 방에서 같은 채널을 여러 번 추가해도 row는 하나만 유지된다.
- 동시에 같은 알람을 추가해도 `ON CONFLICT DO UPDATE`가 경쟁을 흡수한다.
- `member_name`, `room_name`, `user_name`은 새 값이 비었으면 기존 값을 유지한다.

알림 대상 방 조회는 `targets_0188_01.sql`에서 수행한다.

```sql
SELECT room_id, user_id, channel_id, member_name, room_name, user_name, alarm_types
FROM alarms
WHERE channel_id = $1
  AND (alarm_types @> ARRAY[$2::alarm_type] OR cardinality(alarm_types) = 0)
ORDER BY created_at ASC
```

여기서 `cardinality(alarm_types) = 0`은 빈 배열을 "모든 타입 수신"으로 해석하는 규칙이다.

### 3.2 관련 migration

파일:

- `hololive/hololive-api/scripts/migrations/010-add-alarm-types-and-templates.sql`
- `hololive/hololive-api/scripts/migrations/024-room-based-alarm-lookup.sql`

`010` migration은 `alarm_type` 배열과 관련 템플릿을 도입한다. `alarms.alarm_types`는 `alarm_type[]`이고 기본값은 `LIVE`다.

`024` migration은 구독 identity를 room 기반으로 바꾼다. 이 전환 이후 "누가 명령을 쳤는가"보다 "어느 room이 어느 channel을 구독하는가"가 알림 대상 선정의 기준이 된다.

## 4. YouTube 감지 코드

### 4.1 진입점: YouTubeChecker

파일:

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker.go`

주요 타입:

```go
type YouTubeChecker struct {
    cacheClient         cache.Client
    holodexService      *holodex.Service
    tierScheduler       *tier.TieredScheduler
    dedupService        *dedup.Service
    persistedLiveSource YouTubeLiveSessionSource
    targetPolicy        sharedchecker.TargetMinutePolicy
    evaluationWindowCap time.Duration
}
```

`Check(ctx)`가 YouTube 감지의 메인 진입점이다.

```text
now := time.Now().UTC()
loadDueYouTubeCheckInputs(ctx, now)
  -> due channel 없음: 빈 결과
collectDueYouTubeNotifications(...)
  -> channel별 worker를 errgroup으로 실행
  -> channelProcessingConcurrency = 16
```

이 구조는 channel 단위 병렬 처리를 한다. 한 channel의 감지 실패는 `errgroup` 오류로 전체 `Check` 결과에 반영된다.

### 4.2 입력 로딩

파일:

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_input.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/common.go`

`loadDueYouTubeCheckInputs`는 감지에 필요한 입력을 모두 모은다.

순서:

```text
Valkey SMEMBERS AlarmChannelRegistryKey
  -> tierScheduler.SelectDueChannels
  -> persisted live channel을 due channel에 병합
  -> Holodex GetChannelsLiveStatus
  -> PostgreSQL persisted live sessions fallback 로드
  -> member name cache 로드
  -> subscriber room set 로드
```

중요한 cache key 계층:

- `sharedalarmkeys.AlarmChannelRegistryKey`: 알람 대상 channel registry
- `sharedalarmkeys.MemberNameKey`: channel ID -> member display name
- `sharedalarmkeys.ChannelSubscribersKeyPrefix + channelID`: channel별 subscriber room set

`LoadSubscriberRoomsByChannel`은 우선 Valkey `DoMulti` batch로 `SMEMBERS`를 묶어 조회한다. batch 경로가 불가능하면 channel별 sequential/parallel fallback을 사용한다.

### 4.3 Holodex + persisted live fallback

파일:

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_persisted_live.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_live_session_source.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/queries/youtube_live_session_source_0085_01.sql`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/queries/youtube_live_session_source_0132_02.sql`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/queries/youtube_live_session_source_0175_03.sql`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/queries/youtube_live_session_source_0231_04.sql`

`loadHolodexStreamsByChannel`가 Holodex live status를 가져온다. Holodex 오류가 나더라도 `persistedLiveSource`가 있으면 경고를 남기고 persisted session으로 계속할 수 있다.

`shouldFailAfterHolodexError` 조건:

```text
Holodex error 없음 -> 계속
Holodex error 있음 + persisted source 없음 -> 실패
Holodex error 있음 + persisted source error -> 실패
Holodex error 있음 + persisted session 0건 -> 실패
그 외 -> persisted session으로 계속
```

`PgYouTubeLiveSessionSource`는 PostgreSQL의 `youtube_live_sessions`에서 최근 live/upcoming session을 다시 읽는다.

기본 윈도우:

- recent live window: 15분
- upcoming lookahead: 30분

이 fallback은 Holodex가 순간적으로 실패해도 이미 producer가 저장한 live session 기반으로 알림 후보를 만들기 위한 장치다.

`mergePersistedLiveSessionStreams`는 Holodex 결과와 persisted 결과를 병합한다.

- 같은 stream ID가 이미 있으면 빈 필드만 채운다.
- persisted stream이 live이고 현재 stream이 live가 아니면 live status를 승격한다.
- `last_seen_at`은 live catchup eligibility 계산에 사용된다.

### 4.4 target minute 정책

파일:

- `hololive/hololive-shared/pkg/service/alarm/checker/target_policy.go`
- `hololive/hololive-shared/pkg/service/alarm/checker/helpers.go`

`TargetMinutePolicy`는 "몇 분 전 알림을 보낼 것인가"를 결정한다.

주요 함수:

- `NewTargetMinutePolicy`
- `NewTargetMinutePolicyFromRuntimeAdvance`
- `NewTargetMinutePolicyFromConfigured`
- `HighestCrossed`
- `PrimaryAdvanceMinute`

`HighestCrossed(startScheduled, window)`는 이전 검사 시점과 현재 검사 시점 사이에서 crossing된 target minute을 찾는다.

예:

```text
target minutes = [5, 3, 1]
window.Start 기준 6분 전
window.End 기준 4분 전
  -> 5분 target을 crossed
  -> minutesUntil = 5
```

이 방식은 checker가 정확히 5분 전 시각에 실행되지 않아도, 검사 윈도우 사이에 target minute을 지나쳤으면 알림을 보낼 수 있게 한다.

### 4.5 upcoming 알림

파일:

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_upcoming.go`

주요 함수:

- `buildUpcomingNotifications`
- `isUpcomingNotificationCandidate`
- `resolveYouTubeUpcomingSelection`
- `detectRoomScheduleChanges`
- `buildYouTubeUpcomingRoomNotifications`

upcoming 후보 조건:

```text
stream != nil
stream.IsUpcoming()
stream.StartScheduled != nil
stream.StartScheduled.After(window.End)
```

즉 이미 시작 시각이 지났거나, scheduled time이 없는 upcoming은 일반 upcoming 사전 알림 후보가 아니다.

선택 흐름:

```text
currentMinutesUntil = StartScheduled - window.End
previousMinutesUntil = StartScheduled - window.Start
targetPolicy.HighestCrossed(...)
  -> target crossed: 해당 minutesUntil으로 알림 생성
  -> target not crossed: room별 schedule change 감지
```

이미 같은 schedule/minute 알림을 보냈는지는 dedup service가 확인한다.

```text
dedupService.IsAlreadyNotifiedForSchedule(stream.ID, StartScheduled, minutesUntil)
```

target minute crossing이 아닌 경우에도 schedule change가 있으면 알림이 생성될 수 있다. 이때는 `DetectNotificationScheduleChange`를 room별로 호출하고, 실제 변경이 감지된 room에만 notification을 만든다.

### 4.6 live catchup 알림

파일:

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_live.go`

주요 함수:

- `buildLiveCatchupNotifications`
- `isLiveCatchupCandidate`
- `resolveEligibleLiveCatchupStart`
- `unsuppressedLiveCatchupNotifications`
- `roomHasRecentUpcomingNotification`

live catchup은 "이미 live 상태인 방송을 늦게 발견했을 때" 시작 알림을 보완하는 경로다.

후보 조건:

```text
stream != nil
stream.IsLive()
resolveLiveStart(stream) 존재
startAt <= now
now - startAt <= LiveCatchupWindow
```

단, `startAt`이 window 밖이어도 persisted source가 최근 live 관측을 제공하면 허용될 수 있다.

중복 억제:

```text
dedupService.WasUpcomingEventNotifiedRecently(
    roomID,
    channelID,
    stream,
    LiveCatchupSuppressWindow,
)
```

최근 upcoming 알림이 이미 나간 room은 live catchup에서 제외한다. 같은 방송에 대해 "곧 시작" 알림을 받고 곧바로 "시작" catchup이 중복으로 오는 것을 줄이기 위한 장치다.

### 4.7 notification 생성

파일:

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/common.go`

주요 함수:

- `RoomNotifications`
- `RoomNotificationsWithScheduleChanges`
- `EnsureScheduledTime`
- `ApplyMemberNamesToStreams`

`RoomNotifications`는 subscriber room마다 `domain.NewAlarmNotification`을 만든다.

```text
subscriberRooms = [room_A, room_B, room_C]
stream = video_X
minutesUntil = 5
  -> room_A notification
  -> room_B notification
  -> room_C notification
```

`EnsureScheduledTime`은 `StartScheduled`가 없을 때 다음 순서로 보정한다.

```text
StartScheduled 있음 -> 그대로 사용
StartActual 있음 -> StartActual을 StartScheduled로 복사
둘 다 없음 -> fallback time을 UTC minute으로 truncate해서 사용
```

이 보정은 dedup key와 notification payload가 "기준 시각"을 반드시 갖도록 하는 역할을 한다.

## 5. Notifier: dedup claim 후 PG outbox 발행

### 5.1 Notifier 전체

파일:

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_prepare.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_dedup.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_publish.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_resolve.go`

`Notifier.Send`는 checker가 만든 notification 목록을 독립 처리한다. 단일 알림 실패가 전체 batch를 중단하지 않도록 실패를 집계하고 계속 진행한다.

흐름:

```text
prepareSendBatch
  -> prepareOutcomes bounded parallel
  -> prepareOne
      -> resolveSendInput
      -> ValidateLiveDispatchRoute
      -> claimDedup
  -> assemblePrepared
publishPreparedBatch
  -> queuePublisher.PublishBatch
  -> markPublishedBestEffort
```

`prepareBatchConcurrency = 8`이다. Valkey claim round-trip을 병렬화하되 부하를 제한한다.

### 5.2 dedup claim

파일:

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_dedup.go`

`claimDedup`은 두 종류의 claim key를 함께 잡는다.

```text
notifyKey  = room + stream + scheduled + category
logicalKey = room + channel + stream ID/title + scheduled + category
```

코드상 호출:

```text
dedupService.TryClaimPair(ctx, notifyKey, logicalKey, constants.CacheTTL.NotificationSent)
```

둘 중 하나만 잡히면 이미 잡은 key를 best-effort로 풀고 skip한다. 두 key를 모두 잡아야 발행 대상으로 인정한다.

schedule change 메시지가 있는 경우에는 `TryClaimNotificationScheduleChange`도 추가로 잡는다. schedule 변경 알림도 같은 변경에 대해 중복 전송되지 않도록 별도 dedup key를 가진다.

### 5.3 publish 후 mark

파일:

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_publish.go`

`publishBatchAndMark`는 `queuePublisher.PublishBatch`를 호출한다.

성공하면 각 item에 대해 best-effort로 다음을 기록한다.

```text
dedupService.MarkAsNotified(streamID, startScheduled, minutesUntil)
dedupService.MarkUpcomingEventNotified(roomID, channelID, stream)
```

이 mark는 queue publish 이후의 보조 상태다. 실제 delivery의 진실은 PostgreSQL outbox에 들어간 row다.

## 6. hololive-shared queue publisher

파일:

- `hololive/hololive-shared/pkg/service/alarm/queue/publisher.go`

`Publisher`는 notification을 `domain.AlarmQueueEnvelope`로 감싸고, PostgreSQL dispatch outbox에 먼저 기록한다.

주요 상수:

```go
const AlarmDispatchQueue = contractsalarm.DispatchQueueKey
const AlarmDispatchWakeupQueue = "alarm:dispatch:wakeup"
const alarmDispatchWakeupGuardKey = "alarm:dispatch:wakeup:guard"
```

중요한 구조:

```text
PublishBatch
  -> buildPublishBatchEnvelopes
  -> publishEnvelopes
  -> publishPGFirstBatch
      -> insertOutboxChunks
      -> publishWakeup
```

`publishPGFirstBatch`는 outbox repository가 없으면 실패한다.

```text
pg_first requires outbox repository
```

이 말은 현재 경로에서 "큐에 넣는다"는 의미가 Valkey list에 payload를 넣는 것이 아니라 PostgreSQL outbox row를 만드는 것이라는 뜻이다.

`publishWakeup`은 outbox insert 이후 worker를 깨우기 위한 보조 신호다.

```text
SetNX alarm:dispatch:wakeup:guard 1 TTL 3s
  -> 이미 guard 있으면 wakeup suppressed
LPUSH alarm:dispatch:wakeup 1
EXPIRE alarm:dispatch:wakeup 5s
```

중요한 점:

- wakeup은 유실되어도 delivery는 PostgreSQL에 남는다.
- wakeup guard는 대량 insert 시 Valkey wakeup storm을 줄인다.
- worker는 wakeup 없이도 polling/fallback으로 outbox를 볼 수 있어야 한다.

## 7. Dispatch outbox schema와 insert

### 7.1 migration

파일:

- `hololive/hololive-api/scripts/migrations/058_create_alarm_dispatch_outbox.sql`

주요 테이블:

- `alarm_dispatch_events`
- `alarm_dispatch_deliveries`
- `alarm_dispatch_admin_actions`

`alarm_dispatch_events`는 room-agnostic 이벤트다.

중요 필드:

```text
event_key
payload_hash
alarm_type
channel_id
stream_id
category
payload_schema_version
payload
```

중요 제약:

```text
event_key UNIQUE
payload_hash hex64 CHECK
payload가 room_id / roomId / room 필드를 갖지 못하도록 CHECK
```

즉 event는 "무슨 일이 발생했는가"만 담고 "어느 방에 보낼 것인가"는 담지 않는다.

`alarm_dispatch_deliveries`는 room별 delivery다.

중요 필드:

```text
event_id
room_id
dedupe_key UNIQUE
claim_keys
delivery_context
status
attempt_count
next_attempt_at
locked_by / locked_at / lock_expires_at
sending_started_at
sent_at / dlq_at / quarantined_at / cancelled_at
last_error_code / last_error
```

허용 상태:

```text
shadowed
pending
retry
leased
sending
sent
dlq
quarantined
cancelled
```

### 7.2 insert 코드

파일:

- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_insert_batch.go`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_insert.go`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_insert_0092_01.sql`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_insert_0183_02.sql`

`InsertBatch` 흐름:

```text
prepareInsertBatchRows
  -> event_key 기준 event dedupe
  -> 같은 batch 안의 hash collision 탐지
insertPreparedBatch transaction
  -> insertEvents
  -> prepareBatchDeliveriesForInsert
  -> insertDeliveries
  -> recordEventHashCollisions
  -> commit
```

event insert는 `ON CONFLICT (event_key) DO NOTHING`을 사용한다.

delivery insert는 `ON CONFLICT (dedupe_key) DO NOTHING`을 사용한다.

따라서 같은 방송/같은 room/같은 category의 delivery가 중복으로 publish되어도 PostgreSQL unique key가 최종 중복 방어를 한다.

## 8. Dispatch worker claim과 상태 전이

### 8.1 ClaimDue

파일:

- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_claim.go`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_claim_0053_02.sql`

핵심 SQL 구조:

```sql
WITH picked AS (
  SELECT id
  FROM alarm_dispatch_deliveries
  WHERE status IN ('pending', 'retry')
    AND next_attempt_at <= NOW()
  ORDER BY next_attempt_at, id
  LIMIT $1
  FOR UPDATE SKIP LOCKED
),
updated AS (
  UPDATE alarm_dispatch_deliveries d
  SET status = 'leased',
      locked_by = $2,
      locked_at = NOW(),
      lock_expires_at = NOW() + ($3::INT * INTERVAL '1 second'),
      updated_at = NOW()
  FROM picked
  WHERE d.id = picked.id
  RETURNING ...
)
SELECT ...
```

의미:

- `pending`과 `retry`만 claim 대상이다.
- `sending`, `sent`, `quarantined`, `dlq`는 claim되지 않는다.
- `FOR UPDATE SKIP LOCKED`로 여러 worker가 동시에 돌 때 같은 row를 중복 claim하지 않는다.
- claim과 상태 변경이 한 SQL 문 안에서 일어난다.

### 8.2 MarkSending

파일:

- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_transitions.go`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_transitions_0020_01.sql`

`MarkSending`은 외부 전송 직전의 최종 gate다.

조건:

```text
id in batch
status = leased
locked_by = current worker
lock_expires_at > NOW()
```

이 조건을 만족하는 row만 `sending`으로 바뀐다. 코드에서는 `expectRowsAffected`로 요청한 row 수와 실제 update row 수가 다르면 error를 반환한다.

이게 중요한 이유:

- Valkey lease나 worker scheduling이 흔들려도 delivery별 소유권을 PostgreSQL에서 다시 확인한다.
- 외부 Kakao/Iris 전송 전에만 통과해야 하는 gate다.
- 일부 row만 전이된 batch는 발송하지 않는다.

### 8.3 MarkSent, retry, quarantine

파일:

- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_transitions_0040_02.sql`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_transitions_0066_03.sql`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_transitions_0111_04.sql`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/queries/repository_maintenance_0035_02.sql`

상태 전이:

```text
pending/retry -> leased
leased -> sending
sending -> sent
leased -> retry
sending -> retry       (post-send retryable failure)
sending -> quarantined (stale/unknown external outcome)
leased/sending -> dlq  (retry exhausted or terminal failure)
```

`ScheduleSendingRetry`는 post-send retryable failure에서 사용한다. `status IN ('leased', 'sending')`을 허용하고 `lock_expires_at` 조건을 제거한다. 코드 주석은 이유를 명시한다.

```text
RecoverExpiredLeased는 leased만 만지고,
sending은 QuarantineStaleSending이 담당하므로,
locked_by + status 조건으로 소유 worker의 reschedule은 안전하다.
```

stale sending은 maintenance query가 `quarantined`로 보낸다. reason은 외부 발송 결과를 알 수 없다는 의미다.

## 9. alarm dispatch runner

파일:

- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_runner.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_group.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_render.go`

`alarmDispatchRunner.runOnce` 흐름:

```text
consumer.DrainBatch(ctx, maxBatch)
  -> groupAlarmDispatchEnvelopesForKaring
  -> dispatchGroups
  -> dispatchGroup
```

`dispatchGroup`은 text path와 Karing content-list path를 나눈다.

text path:

```text
renderAlarmDispatchGroup
MarkSending
SendMessage / SendMessageWithClientRequestID
MarkDispatched
```

Karing path:

```text
buildAlarmDispatchKaringContentListRequests
MarkSending
SendKaringContentList
MarkDispatched
```

`sendAlarmDispatchMessage`는 sender가 `SendMessageWithClientRequestID`를 지원하면 client request ID를 붙여 보낸다. 이 값은 다운스트림 멱등성 키 역할을 한다.

발송 전 실패:

```text
render/build 실패
  -> ScheduleRetry 또는 MoveToDLQ
```

발송 후 실패:

```text
HTTP 429/502/503
  -> ScheduleSendingRetry
기타 post-send failure + quarantine enabled
  -> Quarantine
그 외
  -> ScheduleRetry
```

## 10. 그룹핑과 렌더링

파일:

- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_group.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_render.go`

그룹핑은 "같은 메시지로 묶어도 되는 delivery"를 결정한다.

중요 규칙:

- celebration과 YouTube outbox milestone은 text path를 쓴다.
- live alarm의 Karing 그룹 키는 room, alarm type, phase, minutes를 포함한다.
- `phase`는 `prelive` 또는 `starting`이다.

`alarmDispatchNotificationIsStarting` 기준:

```text
MinutesUntil <= 0
또는 stream.IsLive()
또는 StartActual != nil
```

이렇게 phase를 나누는 이유는 사전 알림과 시작 알림을 한 메시지에 섞지 않기 위해서다.

렌더링은 다음 정보를 조합한다.

```text
event payload / AlarmNotification
member name
stream title
stream URL
message_strings
template renderer
schedule change message
```

URL resolve 순서도 코드에 들어 있다.

```text
직접 Twitch/Chzzk URL
integrated URL
YouTube watch URL
```

## 11. shared-go 기반 로커와 migration 기초

### 11.1 shared-go dbmigrate

파일:

- `shared-go/pkg/dbmigrate/lock.go`
- `shared-go/pkg/dbmigrate/dbmigrate.go`
- `shared-go/pkg/dbmigrate/ledger.go`

이 코드는 `hololive-bot`의 migration 실행 기초다.

핵심:

- `WithAdvisoryLock`은 PostgreSQL advisory lock을 잡고 migration을 실행한다.
- `SQLLockSession(c *sql.Conn)`은 단일 SQL connection을 받는다.
- `releaseAdvisoryLock`은 `context.WithoutCancel(ctx)` 위에 release timeout을 씌워 cleanup이 parent cancel에 같이 취소되지 않게 한다.
- `ledger.Record`는 migration 적용 기록을 남긴다.

`ledger.go` 주석의 핵심 계약:

```text
Apply와 Record는 별도 Execer 호출이라 원자적이지 않다.
ledger는 at-least-once이므로 migration SQL은 idempotent해야 한다.
ledger alone does not block concurrent execution.
Wrap with WithAdvisoryLock.
```

즉 migration 안전성은 advisory lock과 idempotent SQL이 함께 만든다.

### 11.2 hololive-shared lease

파일:

- `hololive/hololive-shared/pkg/service/lease/lease.go`
- `hololive/hololive-shared/pkg/service/cache/service_scripts.go`

`lease.Acquire`는 Valkey `SetNX`로 owner token을 가진 lease를 잡는다.

`Renew`는 Lua 기반 compare-and-expire를 사용한다.

```text
GET key == owner
  -> EXPIRE key ttl
else
  -> ownership lost
```

`Release`는 compare-and-delete를 사용한다.

```text
GET key == owner
  -> DEL key
else
  -> no-op ownership mismatch
```

이 lease는 발송 담당자 선정 같은 빠른 조율 계층이다. delivery별 최종 소유권은 PostgreSQL의 `locked_by`, `lock_expires_at`, `MarkSending` 조건이 다시 검증한다.

## 12. 한 notification의 실제 생애

```text
1. 사용자가 !알람 페코라 방송 입력
2. parser가 action=add, member=페코라, type=방송으로 파싱
3. AlarmCommand가 멤버를 channel ID로 resolve
4. alarm.Repository.Add가 alarms에 upsert
5. registry/subscriber cache가 갱신됨
6. YouTubeChecker.Check가 due channel을 선택
7. Holodex live status와 persisted youtube_live_sessions를 병합
8. upcoming 또는 live catchup 후보를 선택
9. subscriber room마다 AlarmNotification 생성
10. Notifier가 notify/logical/schedule-change dedup claim을 획득
11. queue.Publisher가 AlarmQueueEnvelope을 만든다
12. dispatchoutbox.InsertBatch가 event/delivery row를 PostgreSQL에 기록
13. Valkey wakeup이 dispatch runner를 깨운다
14. dispatch runner가 ClaimDue로 pending/retry delivery를 leased로 전이
15. 그룹핑/렌더링 후 MarkSending gate 통과
16. Iris/Kakao로 메시지 발송
17. 성공하면 MarkSent, 실패하면 retry/dlq/quarantine
```

## 13. 운영상 중요한 불변식

### 13.1 event는 room-agnostic이다

`alarm_dispatch_events.payload`는 room 정보를 가질 수 없다. migration CHECK가 `room_id`, `roomId`, `room` 키를 금지한다.

의미:

- event는 "무슨 일이 있었는가"만 표현한다.
- room별 상태는 delivery가 표현한다.
- 같은 event를 여러 room에 나눠 전달할 수 있다.

### 13.2 delivery는 room-specific이다

`alarm_dispatch_deliveries`는 `room_id`, `dedupe_key`, `status`를 가진다.

의미:

- room_A는 `sent`, room_B는 `retry`일 수 있다.
- 같은 방송이라도 방마다 상태가 독립이다.
- quarantine도 delivery 단위로 처리된다.

### 13.3 외부 발송 전에는 반드시 MarkSending을 통과한다

`MarkSending`은 발송 직전 gate다.

```text
leased
locked_by == workerID
lock_expires_at > NOW()
```

이 조건이 깨진 delivery는 발송하지 않는다.

### 13.4 wakeup은 최적화다

Valkey wakeup은 빠른 반응을 위한 신호다. 진짜 queue payload는 PostgreSQL outbox에 있다.

의미:

- wakeup이 유실돼도 delivery는 사라지지 않는다.
- worker polling이 나중에 발견할 수 있다.
- Valkey 장애는 즉시성을 떨어뜨릴 수 있지만 PostgreSQL에 기록된 delivery 자체를 없애지는 않는다.

### 13.5 dedup은 여러 겹이다

중복 방어 위치:

```text
Valkey notify/logical claim
PostgreSQL event_key UNIQUE
PostgreSQL dedupe_key UNIQUE
MarkSending ownership gate
ClientRequestID downstream idempotency
```

각 겹의 역할이 다르다.

- Valkey claim은 빠른 중복 억제다.
- PostgreSQL unique key는 durable 중복 방어다.
- MarkSending은 발송 직전 소유권 방어다.
- ClientRequestID는 다운스트림 중복 방어다.

## 14. 파일별 빠른 인덱스

### Command/API

- `hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/message_parser_alarm.go`: 알람 명령 파싱
- `hololive/hololive-api/internal/planes/bot/internal/command/handlers/handler_alarm.go`: 알람 명령 실행
- `hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd/command_execution_policy.go`: 알람 명령 동기 처리 정책

### Shared alarm subscription

- `hololive/hololive-shared/pkg/service/alarm/repository.go`: `alarms` repository
- `hololive/hololive-shared/pkg/service/alarm/queries/repository_0055_01.sql`: 구독 upsert
- `hololive/hololive-shared/pkg/service/alarm/queries/targets_0188_01.sql`: channel/type별 subscriber room 조회

### YouTube checker

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker.go`: `YouTubeChecker.Check`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_input.go`: due channel, Holodex, cache input 로딩
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_upcoming.go`: upcoming 사전 알림
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_live.go`: live catchup 알림
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_persisted_live.go`: persisted session 병합
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_live_session_source.go`: PostgreSQL persisted session source
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/common.go`: notification 생성 helper

### Notifier

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier.go`: send batch orchestration
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_prepare.go`: bounded parallel prepare
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_dedup.go`: Valkey dedup claim
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_publish.go`: queue publish and mark
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/notifier/notifier_resolve.go`: notification payload resolve

### Shared queue/outbox

- `hololive/hololive-shared/pkg/service/alarm/queue/publisher.go`: PG-first queue publisher
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_insert_batch.go`: batch insert orchestration
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_insert.go`: event/delivery insert
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_claim.go`: due delivery claim
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_transitions.go`: sending/sent/retry/dlq/quarantine transition
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/repository_maintenance.go`: stale 상태 복구/격리

### Dispatch worker

- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_runner.go`: batch drain, send, retry/quarantine
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_group.go`: group key
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_render.go`: text rendering

### Migrations

- `hololive/hololive-api/scripts/migrations/010-add-alarm-types-and-templates.sql`: alarm type/template 도입
- `hololive/hololive-api/scripts/migrations/024-room-based-alarm-lookup.sql`: room-based unique key
- `hololive/hololive-api/scripts/migrations/058_create_alarm_dispatch_outbox.sql`: dispatch event/delivery/admin schema

### Shared foundation

- `shared-go/pkg/dbmigrate/lock.go`: PostgreSQL advisory lock
- `shared-go/pkg/dbmigrate/dbmigrate.go`: migration apply
- `shared-go/pkg/dbmigrate/ledger.go`: at-least-once ledger
- `hololive/hololive-shared/pkg/service/lease/lease.go`: Valkey owner-token lease
- `hololive/hololive-shared/pkg/service/cache/service_scripts.go`: compare-and-expire/delete Lua script
