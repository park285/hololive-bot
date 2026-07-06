# YouTube Live Catchup Alarm Rendering Review - 2026-07-05

## Summary

2026-07-05 KST에 `nekota-tsuna` YouTube 방송 알림이 실제 방송 시작 후 전송됐지만, 사용자에게는 "5분 전" 알림처럼 표시됐다.

결론은 두 문제를 분리한다.

- Live 감지 지연: 캐시/폴링/producer batch 경로를 쓰는 한 실제 시작과 `live_first_seen_at` 사이의 지연은 0이 될 수 없다.
- 알림 표시 오류: 이미 `live` 또는 `start_actual`이 확인된 알림을 `MinutesUntil=5`만 보고 "5분 전"으로 렌더링한 것은 버그다.

수정 방향은 producer 감지 경로를 즉시 바꾸는 것이 아니라, dispatch rendering 단계에서 "표시용 방송 시작 여부"를 `Stream.Status`와 `StartActual`로 판정하는 것이다.

현재 `main`에는 `40597234 fix(alarm-worker): live catchup 알림 표시 phase 분리` 커밋으로 이 렌더링 수정이 들어가 있다.

## Incident Timeline

대상 방송:

| Field | Value |
|---|---|
| Member slug | `nekota-tsuna` |
| Channel ID | `UCIjdfjcSaEgdjwbgjxC3ZWg` |
| Video ID | `GZLhfu_DeiM` |
| Title | `【歌枠】新しいマイクで歌います🎤【ぶいすぽ / 猫汰つな】` |

관측 타임라인:

| Event | KST | UTC | Notes |
|---|---:|---:|---|
| Scheduled start | `2026-07-05 23:00:00` | `2026-07-05T14:00:00Z` | DB `scheduled_start_time` |
| Actual start | `2026-07-05 23:05:38` | `2026-07-05T14:05:38Z` | DB `started_at`, dispatch payload `start_actual` |
| First persisted LIVE observation | `2026-07-05 23:09:40` | `2026-07-05T14:09:40Z` | `live_first_seen_at`, first viewer sample |
| Alarm sent | `2026-07-05 23:10:00` | `2026-07-05T14:10:00Z` | `alarm_dispatch_events.id=483` |

Latency split:

| Segment | Duration | Interpretation |
|---|---:|---|
| Scheduled start -> sent | `600s` | "5분 전" 문구와 반대 방향의 알림이다. |
| Actual start -> sent | `262s` | 실제 시작 후 약 4분 22초 뒤 발송됐다. |
| First LIVE observation -> sent | `19s` | dispatch/egress 지연보다는 LIVE 감지 지연이 지배적이다. |

`alarm_dispatch_events.payload`에는 다음 상태가 함께 들어 있었다.

- `notification.minutes_until = 5`
- `notification.stream.status = "live"`
- `notification.stream.start_actual = "2026-07-05T14:05:38Z"`
- `notification.stream.start_scheduled = "2026-07-05T14:00:00Z"`

따라서 payload 자체는 이미 "방송 시작"으로 렌더링할 근거를 갖고 있었다.

## Root Cause

### 1. Live catchup notification creation

`hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_live.go`

- `buildLiveCatchupNotifications`는 `stream.IsLive()`인 경우만 후보로 삼는다.
- `minutesUntil := c.targetPolicySnapshot().PrimaryAdvanceMinute()`로 기본 사전 알림 분값을 가져온다.
- `EnsureScheduledTime(stream, *startAt)`으로 scheduled time을 보강한다.
- `RoomNotifications([]string{roomID}, stream.Channel, stream, minutesUntil, "")`에 `minutesUntil`을 그대로 넘긴다.

즉 live catchup 알림은 의미상 "늦게 감지한 LIVE 알림"이지만, payload의 `MinutesUntil`은 `5` 같은 prelive window 값을 가질 수 있다.

이 값은 표시 문구만을 위한 값이 아니다. dispatch grouping, dedupe/claim key, Karing request identity에 엮일 수 있으므로 `MinutesUntil=0`으로 바꾸는 방식은 영향 범위가 커진다.

### 2. Previous rendering bug

기존 text renderer는 item과 group의 시작 여부를 `MinutesUntil <= 0`만으로 계산했다.

결과적으로 `MinutesUntil=5`인 live catchup 알림은 `Stream.Status=live` 또는 `StartActual != nil`이어도 `.IsStarting=false`가 됐다.

Karing extra args도 `group.minutesUntil > 0`이면 prelive 문구를 사용했기 때문에 같은 오표시 위험이 있었다.

### 3. Template was not the primary bug

`hololive/hololive-api/scripts/migrations/088_reseed_notification_templates_minimal_tone.sql`

현재 기본 템플릿은 이미 `.IsStarting` 분기를 갖고 있다.

```gotemplate
{{if .IsStarting}}🔴 {{.MemberName}} 방송 시작{{else if .IsScheduled}}⏰ {{.MemberName}} 방송 예정{{else}}⏰ {{.MemberName}} 방송 {{.MinutesUntil}}분 전{{end}}
```

따라서 핵심은 템플릿 문구가 아니라 `.IsStarting` view model 계산이었다.

## Implemented Fix

### Text rendering

`hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_render.go`

현재 시작 판정은 `alarmDispatchNotificationIsStarting`으로 분리되어 있다.

```go
func alarmDispatchNotificationIsStarting(notification *domain.AlarmNotification) bool {
    if notification == nil {
        return false
    }
    if notification.MinutesUntil <= 0 {
        return true
    }
    if notification.Stream == nil {
        return false
    }
    return notification.Stream.IsLive() || notification.Stream.StartActual != nil
}
```

`IsScheduled`는 `!starting` 조건을 포함하므로 live/start-actual 신호가 scheduled 표시를 덮어쓴다.

### Group header

`alarmDispatchGroupAllStarting`은 그룹 내 모든 notification이 시작 상태일 때만 group header를 `방송 시작`으로 만든다.

이 기준은 mixed group을 보수적으로 처리한다. 일부만 live catchup인 경우 group header는 기존 분 단위 표현을 유지하고, 각 entry label에서 live item만 `방송 시작`으로 표시한다.

### Karing

`hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_karing.go`

Karing extra args도 `alarmDispatchGroupAllStarting(group)`을 먼저 확인한다.

- all-starting group: `alarm_title = "라이브 시작"`, `time_left = "지금 시작"`
- prelive group: 기존 `방송 %d분 전 알림`, `%d분 후 시작`

`hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_group.go`

Karing grouping key에는 `starting` 또는 `prelive` phase가 포함된다.

```go
phase := "prelive"
if alarmDispatchNotificationIsStarting(&envelope.Notification) {
    phase = "starting"
}
```

이렇게 해서 같은 `MinutesUntil=5`라도 live catchup과 upcoming prelive가 같은 Karing request로 섞이지 않는다.

## Tests

관련 테스트는 다음 파일에 반영되어 있다.

- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_runner_test.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_render_golden_test.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_group_test.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_karing_test.go`

중요 케이스:

- `MinutesUntil=5`, `StartActual != nil`이면 text rendering은 `🔴 Member 방송 시작`을 출력한다.
- `MinutesUntil=5`, `Stream.Status=domain.StreamStatusLive`이면 `IsStarting=true`로 본다.
- normal upcoming은 기존 prelive/scheduled 표현을 유지한다.
- 모든 item이 starting인 group은 header도 `🔴 방송 시작`으로 표시한다.
- live catchup과 prelive가 섞인 Karing group은 phase가 달라져 분리된다.

검증 명령:

```bash
go test ./hololive/hololive-alarm-worker/internal/app/workerapp
```

## Operational Follow-Up: Detection Latency

이 렌더링 수정은 live detection latency 자체를 제거하지 않는다.

이번 incident의 지배적인 지연은 다음 구간이다.

- Actual start -> first persisted LIVE observation: about `262s`
- First persisted LIVE observation -> sent: about `19s`

따라서 Kakao/Iris egress보다 producer/checker observation timing이 주요 지연 구간이다.

감지 지연을 줄이려면 별도 과제로 다음을 봐야 한다.

- `live_batch` interval and scheduling near known start times
- Holodex/YouTube live status cache TTLs
- active-active `job_claim` timeout frequency
- whether `live_first_seen_at` should persist producer/region/worker owner metadata
- whether a scheduled-start-near direct check should bypass ordinary cached batch behavior

이 후속 과제는 "이미 live로 확인된 알림을 5분 전으로 표시하지 않는다"는 렌더링 수정과 분리한다.

## Rollback

렌더링 수정의 rollback 범위는 workerapp code로 한정된다.

Rollback impact:

- Queue payload shape remains unchanged.
- DB schema remains unchanged.
- Notification templates remain unchanged.
- Reverting the workerapp patch restores the old `MinutesUntil <= 0` display behavior.

## References

- `hololive/hololive-alarm-worker/internal/service/alarm/checker/internal/checking/youtube_checker_live.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_render.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_karing.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_group.go`
- `hololive/hololive-alarm-worker/internal/app/workerapp/alarm_dispatch_runner_test.go`
- `hololive/hololive-api/scripts/migrations/088_reseed_notification_templates_minimal_tone.sql`
