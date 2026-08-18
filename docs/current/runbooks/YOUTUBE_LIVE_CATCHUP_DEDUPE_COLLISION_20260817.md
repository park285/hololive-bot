# YouTube Live Catchup Dedupe Collision — 2026-08-17

## Summary

2026-08-17 00:24–01:04 KST 전후 Kakao 알림이 다음처럼 관측됐다.

- 예정 시각 기준 `방송 5분 전`은 나갔다.
- 실제 시작 시각의 `방송 시작`은 나가지 않았다.
- 방송이 끝난 직후 `방송 시작`이 다시 나갔다.

이것은 `NEW_VIDEO` 영상등록과 live catchup 키가 겹친 사건이 아니다. 겹친 쪽은 **같은 YouTube 스트림의 `방송 5분 전`과 live catchup `방송 시작`**이다. 둘 다 `MinutesUntil = 5`라서 같은 예정 시각이면 같은 중복 키로 취급된다.

실제 시작 때의 live catchup은 5분 전 알림에 먹혔고, 종료 직전에 Holodex/세션의 `start_scheduled`가 1–2분 보정되면서 키가 달라져 재전송이 허용됐다. 그때 payload는 이미 `status=live`와 `start_actual`을 갖고 있어서 문구가 `방송 시작`으로 렌더됐다.

수정은 로컬 작업 트리에 구현했다. 운영 runtime에는 아직 배포하지 않았다.

관련 선행 문서: `YOUTUBE_LIVE_CATCHUP_ALARM_RENDERING_20260705.md`. 2026-07-05 수정은 live catchup을 `MinutesUntil=5`여도 `방송 시작`으로 표시하게 했다. 이번 사건은 그 표시 버그가 아니라, 같은 `MinutesUntil=5`를 중복 키에 쓰는 쪽이다.

## Not This Issue

| 오해 | 실제 |
|---|---|
| live catchup 키와 영상등록(`NEW_VIDEO`) 키가 충돌했다 | `youtube_notification_outbox`에 해당 `video_id`의 `NEW_VIDEO`/`LIVE_STREAM` 행이 없다 |
| 방송 종료 후 아카이브가 `youtube_videos`에 새로 들어가 LIVE로 재등록됐다 | 두 ID 모두 `youtube_videos` 행이 없다. 같은 채널 구간 알람도 원래 `stream_id` 두 개뿐이다 |
| 종료된 방송이 VOD로 등록되면서 `새 영상`이 나갔다 | 늦은 메시지는 alarm-worker LIVE 이벤트이고 문구는 `방송 시작`이다. 종료 직후라는 타이밍만 그 증상과 닮았다 |
| collector가 LIVE를 늦게 봤다 | `youtube_live_sessions.live_first_seen_at`는 실제 시작과 수십 초 이내다 |
| 멤버십 방송이라 LIVE 감지가 실패했다 | 이로하도 `00:31` KST에 LIVE로 persist됐다. 멤버십 결제 링크 부재는 별 제품 공백이다 |

## Observed Streams

조사 시각 2026-08-17 11:31 KST 기준, 중앙 PostgreSQL `hololive` 읽기 전용 조회.

### 미코 `iPvbyvgv18g`

| Field | Value |
|---|---|
| Channel ID | `UC-hM6YJuNYVAmUWxeIr9FeA` |
| Title | `【7Days to Die】#ホロ7末日劇場 ...` |
| `topic_id` | `7_days_to_die` |
| Scheduled (DB) | `2026-08-16 22:02:04` KST |
| `started_at` | `2026-08-16 22:02:15` KST |
| `live_first_seen_at` | `2026-08-16 22:02:28` KST |
| `ended_at` | `2026-08-17 00:42:15` KST |
| `last_seen_at` | `2026-08-17 00:43:30` KST |
| End reason | `EXPLICIT_END` |

`ended_at`는 `started_at + 2h40m`과 초 단위까지 같다. VOD duration으로 종료 시각을 채운 흔적이다.

### 이로하 `dnAcI-ITkzI`

| Field | Value |
|---|---|
| Channel ID | `UC_vMYWcDjmfdpH6r4TTn1MQ` |
| Title | `風真いろはの #ねるまえらじお 📻inメン限` |
| `topic_id` | `membersonly` |
| Scheduled (DB) | `2026-08-17 00:31:21` KST |
| `started_at` | `2026-08-17 00:31:45` KST |
| `live_first_seen_at` | `2026-08-17 00:31:58` KST |
| `ended_at` | `2026-08-17 01:03:45` KST |
| `last_seen_at` | `2026-08-17 01:05:30` KST |
| End reason | `EXPLICIT_END` |

`ended_at`는 `started_at + 32m`과 초 단위까지 같다.

## Alarm Dispatch Timeline

`alarm_dispatch_events` 4행. 모두 `alarm_type=LIVE`, `category=5`. checker는 분 정각에 돌았다.

| KST | Video | Payload status | `start_scheduled` | `start_actual` | `minutes_until` | User-visible |
|---|---|---|---|---|---:|---|
| 21:54 | 미코 | `upcoming` | `2026-08-16T13:00:00Z` (22:00 KST) | empty | 5 | 방송 5분 전 |
| 00:25 | 이로하 | `upcoming` | `2026-08-16T15:30:00Z` (00:30 KST) | empty | 5 | 방송 5분 전 |
| 00:43 | 미코 | `live` | `2026-08-16T13:02:04Z` | `13:02:15.246183Z` | 5 | 방송 시작 |
| 01:04 | 이로하 | `live` | `2026-08-16T15:31:21Z` | `15:31:45.306129Z` | 5 | 방송 시작 |

event key:

```text
live:{channel_id}:{video_id}:{scheduled_unix_minute}:5:LIVE
```

| Event | scheduled unix | Truncated UTC | KST |
|---|---:|---|---|
| 미코 5분 전 | `1786885200` | `13:00:00` | 22:00 |
| 미코 늦은 시작 | `1786885320` | `13:02:00` | 22:02 |
| 이로하 5분 전 | `1786894200` | `15:30:00` | 00:30 |
| 이로하 늦은 시작 | `1786894260` | `15:31:00` | 00:31 |

`alarm_dispatch_event_collisions`에는 해당 `stream_id` 행이 없다. 시작 시각 live catchup은 collision audit가 아니라 **같은 event key / Valkey target-minute 중복으로 삼켜진 것**이다.

## Root Cause

### 1. Live catchup이 5분 전 알림과 같은 분 값을 쓴다

`hololive/hololive-alarm-worker/internal/service/alarm/checker/checking/youtube_checker_live.go`

- `buildLiveCatchupNotifications`는 `stream.IsLive()`만 본다.
- `minutesUntil := c.targetPolicySnapshot().PrimaryAdvanceMinute()` → production 기본값 `5`.
- 그 값을 `RoomNotifications`에 그대로 넣는다.

의미는 "늦게 잡은 LIVE"인데, 중복 키의 category/minutes 자리는 prelive `5`와 같다.

### 2. event key와 Valkey가 그 분 값을 식별자로 쓴다

`hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/dedupe_key.go`

```text
live:{channel_id}:{stream_id}:{StartScheduled truncated to minute}:5:LIVE
```

5분 전 upcoming도 `AlarmTypeLive` + `MinutesUntil=5`라서 같은 형식이다. 예정 시각 분이 같으면 unique `event_key`가 시작 알림 insert를 막는다.

Valkey `IsAlreadyNotifiedForSchedule`는 더 넓다. 같은 `start_scheduled` 문자열에 target minute(5/3/1) 중 하나라도 보냈으면, 이후 `minutesUntil=5` live catchup도 이미 보낸 것으로 본다.

반대로 `start_scheduled` 문자열이 바뀌면 주석 그대로 **스케줄 변경 → 발송 허용**이다.

### 3. EnsureScheduledTime이 StartActual로 키를 바꾸지 않는다

`hololive/hololive-alarm-worker/internal/service/alarm/checker/checking/common.go`

`EnsureScheduledTime`은 `StartScheduled`가 이미 있으면 그대로 둔다. Holodex `/users/live`가 준 22:00 / 00:30이 persist된 `22:02:04` / `00:31:21`보다 이긴다 (`firstTimePtr`가 Holodex 쪽을 유지).

그래서 실제 시작 직후 checker가 persist LIVE와 `start_actual`을 보더라도, event key 분의 자리는 여전히 5분 전과 같다.

### 4. 종료 직전 예정 시각이 1–2분 보정되면 재전송이 `방송 시작`으로 보인다

2026-07-05 이후 renderer는 `MinutesUntil=5`여도 `status=live` 또는 `StartActual != nil`이면 `.IsStarting=true`다.

종료 직후 Holodex가 스트림을 내려주고 persist 세션만 남으면, checker는 보정된 `scheduled_start_time`과 `start_actual`을 쓴다. 키가 달라지고 Valkey도 스케줄 변경으로 재허용한다. 그 틱의 payload는 `live`라서 사용자에게는 끝난 방송의 `방송 시작`으로 보인다.

미코 늦은 이벤트 `00:43:00`는 `ended_at 00:42:15` 이후, `last_seen_at 00:43:30` 이전이다. 이로하 늦은 이벤트 `01:04:00`는 `ended_at 01:03:45` 직후다.

## What Was Healthy

조사 당시 적체는 원인이 아니었다.

| Check | Result |
|---|---|
| `source_observation_queue` | `PENDING 2`, `PROCESSED 224953` |
| `holodex_live` lease | `IDLE`, `last_error_code` empty |
| `youtube_notification_outbox` for both IDs | 0 rows |

LIVE persist 자체는 정각에 성공했다. 빠진 것은 시작 시각의 Kakao `방송 시작` egress다.

## Adjacent Operational Finding

2026-08-17 11:31 KST 중앙 호스트 컨테이너:

| Runtime | Running revision | Image tag revision | Started (UTC) |
|---|---|---|---|
| `hololive-youtube-collector-c` | `0b7af758` (PR #367) | `0b7af758` | `2026-08-16T11:22:24Z` |
| `hololive-api` | `8b068318` (PR #362) | tagged `0b7af758`, container still `8b068318` | `2026-08-16T05:39:29Z` |
| `hololive-alarm-worker` | `8b068318` | `8b068318` | `2026-08-16T05:44:28Z` |

`hololive-api:prod` 태그는 새 이미지로 옮겨졌지만 실행 중 API 컨테이너는 recreate되지 않았다. 이번 두 알림의 직접 원인은 아니지만 collector/API 버전이 어긋난 채로 돌아가고 있다.

## Reproduce The Pattern

같은 키가 다시 겹치는지 확인하는 읽기 전용 조회:

```sql
SELECT stream_id,
       created_at,
       event_key,
       payload #>> '{notification,minutes_until}' AS minutes_until,
       payload #>> '{notification,stream,status}' AS status,
       payload #>> '{notification,stream,start_scheduled}' AS start_scheduled,
       payload #>> '{notification,stream,start_actual}' AS start_actual
FROM alarm_dispatch_events
WHERE stream_id = '<video_id>'
ORDER BY created_at;
```

패턴:

1. 첫 행: `status=upcoming`, `minutes_until=5`, `start_actual` 없음, `start_scheduled`가 정시/30분.
2. `youtube_live_sessions.live_first_seen_at`는 그 직후인데 그 분에는 LIVE 이벤트가 없다.
3. 마지막 행: `status=live`, `start_actual` 있음, `start_scheduled`가 1–2분 이동, 시각은 `ended_at` 근처.

## Implemented Fix

- `AlarmNotification.IsStarting`과 `IsLiveCatchup`이 표시, grouping, dedupe에서 공유하는 phase 판정의 단일 소유자다.
- live catchup은 payload의 표시용 `MinutesUntil=5`를 유지하면서 Valkey claim과 PostgreSQL event에 `live_catchup` category를 사용한다. 이 identity는 `start_scheduled`와 무관하므로 1–2분 보정으로 다시 열리지 않는다.
- checker가 기존 PostgreSQL sent-room evidence를 후보 억제에도 재사용한다. 이 조회는 guardrail 경고용 2분 grace와 분리되어 현재 LIVE stream에 즉시 적용된다. 같은 `stream_id` 알림을 이미 받은 방은 예정 시각이 바뀌어도 catchup 대상에서 제외하고, 아직 받지 않은 방은 prelive event와 분리된 catchup event로 보낼 수 있다.
- queue wire version, schema, migration, 새 dependency는 추가하지 않았다.

## Rollback

운영 runtime에는 아직 변경이 없다. 로컬 소스 변경을 되돌리면 되며 schema나 데이터 rollback은 필요하지 않다.

## References

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/checking/youtube_checker_live.go`
- `hololive/hololive-alarm-worker/internal/service/alarm/checker/checking/common.go`
- `hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_render.go`
- `hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/dedupe_key.go`
- `hololive/hololive-shared/pkg/service/alarm/dedup/dedup_notified.go`
- `docs/current/runbooks/YOUTUBE_LIVE_CATCHUP_ALARM_RENDERING_20260705.md`
- `docs/current/contracts/alarm.md`
