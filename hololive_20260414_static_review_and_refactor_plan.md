# Hololive 전체 레포 정적 리뷰 및 리팩토링 제안

작성 기준: 업로드된 번들(`hololive-bot-full-20260414T014251Z.tar.gz`) 기준 정적 분석

## 1. 먼저 결론

이 레포에서 지금 가장 급한 것은 세 가지입니다.

첫째, **5분 전 알람 누락 버그는 설정값 문제라기보다 체크 윈도우 정책 문제**입니다. 현재 코드는 첫 관측 또는 늦은 관측에서 `Capped=true`가 되면, 실제로 5분 경계를 막 지나온 스트림도 의도적으로 backfill 하지 않습니다. 그래서 운영에서는 "기본 5분 알람"이 아니라 뒤늦게 3분/1분 페일백만 오는 현상이 발생할 수 있습니다.

둘째, **알람 영속화 비동기 경로가 실제로는 안전하지 않습니다.** `stripedExecutor.Submit()`이 채널 full 상태에서 블로킹되므로, "응답 지연 없이 비동기 저장"이라는 주석과 달리 요청 스레드를 멈출 수 있습니다. 반대로 큐가 막히거나 프로세스가 종료되면 캐시에는 반영됐는데 DB에는 반영되지 않는 durable gap도 생깁니다.

셋째, **Valkey I/O가 지나치게 잘게 쪼개져 있습니다.** 채널별 `SMembers` fan-out, 캐시 warm-up 시 per-alarm 다중 round-trip, dispatcher 재큐잉 hot loop 등으로 인해 데이터가 늘수록 병목이 선형 이상으로 커질 구조입니다.

## 2. 분석 범위와 한계

이 리뷰는 정적 분석 기준입니다. 이 환경에는 `go1.23.2`만 설치돼 있는데, 레포 루트는 `go 1.26.2`를 요구합니다. 네트워크가 막혀 있어서 toolchain 다운로드가 되지 않아 전체 테스트 실행은 하지 못했습니다. 대신 기존 테스트 코드와 런타임 경로를 역추적해 원인과 수정안을 만들었습니다.

확인한 대표 파일은 아래와 같습니다.

- `hololive/hololive-shared/pkg/service/alarm/checker/helpers.go`
- `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/youtube_checker.go`
- `hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_persistence.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/striped_executor.go`
- `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/common.go`
- `hololive/hololive-shared/pkg/service/alarm/cache_warm.go`
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
- `hololive/hololive-shared/pkg/service/member/adapter.go`
- `hololive/hololive-kakao-bot-go/docs/local/alarm-notification.md`

## 3. 레포 전반에서 보이는 구조적 냄새

### 3.1 패치 누적형 구조

정적 스캔 기준으로 다음이 보였습니다.

- `*_additional_test.go`: **117개**
- 500라인 이상 non-test Go 파일: **22개**
- `hololive-kakao-bot-go/internal/app`의 `Provide*` 함수 수: **80개**
- `hololive-llm-sched/internal/app`의 `Provide*` 함수 수: **41개**

이 숫자 자체가 문제라는 뜻은 아닙니다. 다만 지금 레포는 **기존 설계를 정리해서 흡수하기보다, 증상마다 패치를 얹는 방식**으로 진화한 흔적이 강합니다. 그 결과로 아래 문제가 생깁니다.

- 정책의 단일 소스가 없고, 같은 개념이 여러 레이어에 중복 구현됨
- 테스트가 시나리오별 보강 파일로 흩어져 회귀 의도 추적이 어려움
- 문서가 현재 코드 구조를 따라가지 못함
- 큰 파일 하나에 정책, I/O, 예외처리, fallback, 관측값 보정이 함께 들어가 변경 반경이 커짐

### 3.2 Source of Truth 분산

알람 target minute 정책은 아래 함수들에 분산되어 있습니다.

- `NormalizeTargetMinutes`
- `BuildRuntimeTargetMinutes`
- `ResolveConfiguredTargetMinutes`
- `ResolvePersistedTargetMinutes`
- settings load path
- runtime scheduler sync path

즉, **config 해석**, **persisted healing**, **runtime update**, **checker 평가**가 각각 따로 판단합니다. 이전엔 `[5,1]` legacy healing이 문제 축이었을 가능성이 높지만, 현재 번들에서는 그 부분은 상당 부분 보완됐습니다. 지금 남은 핵심 버그는 "정책 배열"이 아니라 **평가 윈도우 의미론**입니다.

### 3.3 조용한 fallback이 너무 많음

예를 들어 `member/adapter.go`는 `context.TODO()` 또는 `context.WithoutCancel(ctx)`를 사용하고, 캐시/리포 오류가 나면 빈 슬라이스를 돌려주며 계속 진행합니다. 이런 스타일은 장애를 빨리 감추는 대신, 나중에 데이터 불일치와 "왜 검색이 가끔 비나" 같은 유령 버그를 만듭니다.

## 4. P0 — 5분 전 알람 버그

## 4.1 현상

사용자 기대: 기본 정책이 `[5,3,1]`이면 5분 전에 먼저 알람이 와야 함.

현재 실제 가능 동작: 스트림을 첫 관측한 시점이 이미 `4분 20초 남음` 같은 구간이면 5분 알람은 건너뛰고, 이후 3분/1분에서만 알림이 옴.

## 4.2 원인

원인은 아래 3개가 결합된 것입니다.

1. YouTube 루프는 시작 시 즉시 실행되지 않고 다음 정렬된 분까지 기다릴 수 있음.
2. 첫 관측 또는 오랫동안 체크되지 않은 채널은 `ResolveEvaluationWindow()`가 `Capped=true`인 윈도우를 만듦.
3. `HighestCrossedTarget()`는 `window.Capped`이면 **교차한 target minute를 backfill 하지 않음**.

즉, 프로세스 시작 직후 또는 특정 채널이 첫 스캔될 때, 이미 5분 경계를 막 지나온 스트림은 의도적으로 버려집니다.

## 4.3 버그를 뒷받침하는 현재 테스트

현재 번들에는 이 동작을 “정상”처럼 굳힌 테스트가 이미 있습니다.

- `hololive-shared/pkg/service/alarm/checker/helpers_test.go`
  - `does not backfill stale five minute target outside cap`
- `hololive-kakao-bot-go/internal/service/alarm/checker/checker_additional_test.go`
  - `build upcoming notifications does not backfill stale five minute target`

즉, 운영에서는 버그로 보이지만, 현재 테스트는 그 버그를 사실상 규격처럼 잠가둔 상태입니다.

## 4.4 가장 안전한 수정 방향

전체 정책을 바꾸지 말고, **초기 관측(initial observation)일 때만 capped window에서도 제한적 backfill을 허용**하는 것이 가장 안전합니다.

핵심 아이디어는 아래입니다.

- `EvaluationWindow`에 `InitialObservation bool` 추가
- `ResolveEvaluationWindow()`에서 `prevCheckedAt.IsZero()`면 `InitialObservation=true`
- `HighestCrossedTarget()`에서
  - exact minute 일치는 기존처럼 허용
  - `window.Capped && !window.InitialObservation`일 때만 backfill 금지
  - 즉, **초기 관측의 bounded window 내 교차 target**은 허용

이렇게 하면

- 재기동 직후 / 첫 관측 시의 5분 알람 복구 가능
- 장시간 outage 뒤의 과거 알림 대량 backfill은 계속 차단
- 기존 dedup 키/TTL 체계와 충돌하지 않음

## 4.5 실제 패치 파일

실제 unified diff는 별도 패치 파일로 만들었습니다.

- `hololive_20260414_p0_alarm_fix.patch`

핵심 변경은 아래 3개 파일입니다.

- `hololive-shared/pkg/service/alarm/checker/helpers.go`
- `hololive-shared/pkg/service/alarm/checker/helpers_test.go`
- `hololive-kakao-bot-go/internal/service/alarm/checker/checker_additional_test.go`

## 4.6 핵심 diff 요약

```diff
--- a/hololive/hololive-shared/pkg/service/alarm/checker/helpers.go
+++ b/hololive/hololive-shared/pkg/service/alarm/checker/helpers.go
@@
 type EvaluationWindow struct {
-    Start  time.Time
-    End    time.Time
-    Capped bool
+    Start              time.Time
+    End                time.Time
+    Capped             bool
+    InitialObservation bool
 }
@@
     window := EvaluationWindow{
-        Start:  windowStart,
-        End:    now,
-        Capped: true,
+        Start:              windowStart,
+        End:                now,
+        Capped:             true,
+        InitialObservation: prevCheckedAt.IsZero(),
     }
@@
-    if window.Capped {
-        return 0, false
-    }
-
     previous := minutesUntilFloorZeroClamped(startScheduled, window.Start)
     if previous <= current {
         return 0, false
     }
+
+    if window.Capped && !window.InitialObservation {
+        return 0, false
+    }
```

## 4.7 추가 권고

이 P0 패치만으로도 운영 증상은 상당히 줄어들 가능성이 높습니다. 다만 장기적으로는 `runImmediately=false`인 YouTube 시작 정책도 다시 볼 필요가 있습니다. 지금은 초기 관측 backfill로 가리는 방식이고, 근본적으로는 첫 루프를 지연시키는 설계 자체가 startup blind spot을 만듭니다.

다만 이 부분은 부수효과가 더 크므로, 1차 패치에서는 건드리지 않는 편이 안전합니다.

## 5. P0 — 비동기 영속화가 실제로는 블로킹되고, 심지어 유실될 수 있음

문제 파일:

- `hololive-kakao-bot-go/internal/service/notification/striped_executor.go`
- `hololive-kakao-bot-go/internal/service/notification/alarm_persistence.go`

## 5.1 현재 문제

`stripedExecutor.Submit()`은 이렇게 동작합니다.

```go
e.taskWG.Add(1)
e.stripes[index] <- task
```

즉, stripe queue가 꽉 차면 여기서 **무한정 블로킹**됩니다. 주석은 "사용자 응답을 지연시키지 않기 위해 goroutine으로 실행"이라고 돼 있는데, 실제 구현은 그렇지 않습니다.

또 다른 문제는 더 큽니다.

- 캐시에 반영 후 DB 저장은 비동기
- 큐 포화 / 프로세스 종료 / writer 장애 시 DB 반영 실패 가능
- 그런데 사용자에게는 이미 성공처럼 보임

이건 단순 성능 이슈가 아니라 **일관성 문제**입니다.

## 5.2 최소 안전 패치

단기적으로는 두 단계가 필요합니다.

1. `Submit()`을 non-blocking 또는 짧은 timeout 제출로 바꿔 request latency collapse를 막기
2. 큐 포화 시 **inline fallback**으로라도 저장해서 durable gap을 줄이기

```diff
--- a/hololive/hololive-kakao-bot-go/internal/service/notification/striped_executor.go
+++ b/hololive/hololive-kakao-bot-go/internal/service/notification/striped_executor.go
@@
 var errStripedExecutorClosed = errors.New("striped executor closed")
+var errStripedExecutorSaturated = errors.New("striped executor saturated")
@@
 func (e *stripedExecutor) Submit(key string, task func()) error {
@@
     index := e.stripeIndex(key)
     e.taskWG.Add(1)
-
-    e.stripes[index] <- task
-
-    return nil
+
+    select {
+    case e.stripes[index] <- task:
+        return nil
+    case <-e.stopCh:
+        e.taskWG.Done()
+        return errStripedExecutorClosed
+    default:
+        e.taskWG.Done()
+        return errStripedExecutorSaturated
+    }
 }
```

그리고 submit caller는 포화 시 inline fallback을 타야 합니다.

```diff
--- a/hololive/hololive-kakao-bot-go/internal/service/notification/alarm_persistence.go
+++ b/hololive/hololive-kakao-bot-go/internal/service/notification/alarm_persistence.go
@@
 import (
     "context"
+    "errors"
     "fmt"
     "log/slog"
@@
     if err := as.persistExecutor.Submit(roomID, task); err != nil {
+        if errors.Is(err, errStripedExecutorSaturated) {
+            if as.logger != nil {
+                as.logger.Error("Persist queue saturated, falling back to inline execution",
+                    slog.String("action", action),
+                    slog.String("room_id", roomID),
+                )
+            }
+            task()
+            return
+        }
+
         if as.logger != nil {
             as.logger.Warn("Failed to submit persist task to executor",
                 slog.String("action", action),
                 slog.String("room_id", roomID),
                 slog.Any("error", err),
             )
         }
     }
 }
```

## 5.3 장기적으로는 이렇게 가야 함

이 최소 패치는 "요청이 멈추는 문제"를 줄여줄 뿐, 아키텍처의 근본 치료는 아닙니다. 장기적으로는 아래 둘 중 하나로 가야 합니다.

- 사용자 명령의 DB write를 동기 처리하고, 캐시/레지스트리 갱신은 그 결과를 반영
- 또는 durable command/outbox를 두고, 캐시 반영도 그 durable 기록을 기준으로 재구성

현재 구조처럼 “캐시는 성공, DB는 best effort”는 운영 사고 때 반드시 흔적을 남깁니다.

## 6. P1 — 채널별 SMembers fan-out이 Valkey round-trip을 과도하게 만듦

문제 파일:

- `hololive-kakao-bot-go/internal/service/alarm/checker/common.go`

현재 `loadSubscriberRoomsByChannel()`은 채널마다 goroutine을 만들고, 채널마다 `SMembers`를 따로 보냅니다. 채널 수가 커지면 네트워크 round-trip 수와 서버 락 경쟁이 커집니다.

이건 **고루틴 수를 늘려 I/O를 숨기는 구조**이지, I/O 자체를 줄이는 구조가 아닙니다.

## 6.1 권장 패치

한 번의 `DoMulti`로 `SMEMBERS`를 파이프라인하고, 결과를 channelID별로 역매핑하는 방식이 맞습니다.

```diff
--- a/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/common.go
+++ b/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/common.go
@@
 import (
     "context"
     "fmt"
     "log/slog"
-    "sync"
     "time"
+
+    "github.com/valkey-io/valkey-go"
@@
 func loadSubscriberRoomsByChannel(
     ctx context.Context,
     cacheSvc cache.Client,
     channelIDs []string,
 ) (map[string][]string, error) {
@@
-    result := make(map[string][]string, len(uniqueChannelIDs))
-
-    var mu sync.Mutex
-
-    eg, egCtx := errgroup.WithContext(ctx)
-    eg.SetLimit(defaultLookupConcurrency)
-
-    for _, channelID := range uniqueChannelIDs {
-        eg.Go(func() error {
-            rooms, err := cacheSvc.SMembers(egCtx, notification.ChannelSubscribersKeyPrefix+channelID)
-            if err != nil {
-                return fmt.Errorf("load subscriber rooms by channel: smembers channel %s: %w", channelID, err)
-            }
-
-            if len(rooms) == 0 {
-                return nil
-            }
-
-            mu.Lock()
-            result[channelID] = rooms
-            mu.Unlock()
-
-            return nil
-        })
-    }
-
-    if err := eg.Wait(); err != nil {
-        return nil, fmt.Errorf("load subscriber rooms by channel: wait workers: %w", err)
-    }
+    result := make(map[string][]string, len(uniqueChannelIDs))
+    cmds := make([]valkey.Completed, 0, len(uniqueChannelIDs))
+
+    for _, channelID := range uniqueChannelIDs {
+        cmds = append(cmds, cacheSvc.B().Smembers().Key(notification.ChannelSubscribersKeyPrefix+channelID).Build())
+    }
+
+    results := cacheSvc.DoMulti(ctx, cmds...)
+    if len(results) != len(uniqueChannelIDs) {
+        return nil, fmt.Errorf("load subscriber rooms by channel: unexpected result count: got=%d want=%d", len(results), len(uniqueChannelIDs))
+    }
+
+    for i, channelID := range uniqueChannelIDs {
+        rooms, err := results[i].AsStrSlice()
+        if err != nil {
+            return nil, fmt.Errorf("load subscriber rooms by channel: smembers channel %s: %w", channelID, err)
+        }
+        if len(rooms) == 0 {
+            continue
+        }
+        result[channelID] = rooms
+    }
 
     return result, nil
 }
```

효과는 간단합니다.

- N개 채널 → N개 network round-trip에서 **1회 pipeline**로 축소
- goroutine scheduling overhead 제거
- Valkey latency spike 시 tail latency 완화

## 7. P1 — 캐시 warm-up이 너무 잘게 쪼개져 있음

문제 파일:

- `hololive-shared/pkg/service/alarm/cache_warm.go`

현재 `WarmSubscriberCacheFromAlarms()`는 alarm마다 아래를 각각 호출합니다.

- `SAdd(room alarm key)`
- `SAdd(registry)`
- `SAdd(channel registry)`
- alarmType마다 `SAdd(channel subscriber)`
- 필요 시 `HSet(memberName)`
- 필요 시 `HSet(roomName)`
- 필요 시 `HSet(userName)`

즉, startup 때 alarm 수가 많아지면 **per-record × per-field round-trip 폭발**이 납니다.

또 `compactUniqueStrings()`는 `slices.Contains`를 사용해서 O(n²) dedupe입니다.

## 7.1 최소 수정안

### dedupe부터 바로 수정

```diff
--- a/hololive/hololive-shared/pkg/service/alarm/cache_warm.go
+++ b/hololive/hololive-shared/pkg/service/alarm/cache_warm.go
@@
-import "slices"
@@
 func compactUniqueStrings(values []string) []string {
-    result := make([]string, 0, len(values))
+    result := make([]string, 0, len(values))
+    seen := make(map[string]struct{}, len(values))
     for _, value := range values {
         value = strings.TrimSpace(value)
-        if value == "" || slices.Contains(result, value) {
+        if value == "" {
             continue
         }
+        if _, ok := seen[value]; ok {
+            continue
+        }
+        seen[value] = struct{}{}
         result = append(result, value)
     }
     return result
 }
```

### warm-up write batching

아래처럼 chunk 단위 pipeline helper를 두는 것이 좋습니다.

```diff
--- a/hololive/hololive-shared/pkg/service/alarm/cache_warm.go
+++ b/hololive/hololive-shared/pkg/service/alarm/cache_warm.go
@@
+const cacheWarmWriteBatchSize = 128
+
 func WarmSubscriberCacheFromAlarms(ctx context.Context, cacheSvc cache.Client, alarms []*domain.Alarm) (CacheWarmSummary, error) {
@@
-    for _, alarmRecord := range alarms {
+    pending := make([]*domain.Alarm, 0, cacheWarmWriteBatchSize)
+    for _, alarmRecord := range alarms {
         if alarmRecord == nil {
             continue
         }
@@
-        if _, err := cacheSvc.SAdd(...); err != nil { ... }
-        if _, err := cacheSvc.SAdd(...); err != nil { ... }
-        ...
+        pending = append(pending, alarmRecord)
+        if len(pending) == cacheWarmWriteBatchSize {
+            if err := flushAlarmWarmBatch(ctx, cacheSvc, pending); err != nil {
+                return CacheWarmSummary{}, err
+            }
+            pending = pending[:0]
+        }
@@
     }
+
+    if len(pending) > 0 {
+        if err := flushAlarmWarmBatch(ctx, cacheSvc, pending); err != nil {
+            return CacheWarmSummary{}, err
+        }
+    }
@@
 }
+
+func flushAlarmWarmBatch(ctx context.Context, cacheSvc cache.Client, alarms []*domain.Alarm) error {
+    cmds := make([]valkey.Completed, 0, len(alarms)*8)
+    for _, alarmRecord := range alarms {
+        roomID := strings.TrimSpace(alarmRecord.RoomID)
+        channelID := strings.TrimSpace(alarmRecord.ChannelID)
+        if roomID == "" || channelID == "" {
+            continue
+        }
+
+        alarmTypes := alarmRecord.AlarmTypes
+        if len(alarmTypes) == 0 {
+            alarmTypes = domain.DefaultAlarmTypes
+        }
+
+        cmds = append(cmds,
+            cacheSvc.B().Sadd().Key(sharedalarmkeys.BuildRoomAlarmKey(roomID)).Member(channelID).Build(),
+            cacheSvc.B().Sadd().Key(sharedalarmkeys.AlarmRegistryKey).Member(alarmRecord.RegistryKey()).Build(),
+            cacheSvc.B().Sadd().Key(sharedalarmkeys.AlarmChannelRegistryKey).Member(channelID).Build(),
+        )
+
+        for _, alarmType := range alarmTypes {
+            cmds = append(cmds,
+                cacheSvc.B().Sadd().Key(sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)).Member(alarmRecord.RegistryKey()).Build(),
+            )
+        }
+
+        if alarmRecord.MemberName != "" {
+            cmds = append(cmds,
+                cacheSvc.B().Hset().Key(sharedalarmkeys.MemberNameKey).FieldValue().FieldValue(channelID, alarmRecord.MemberName).Build(),
+            )
+        }
+        if alarmRecord.RoomName != "" {
+            cmds = append(cmds,
+                cacheSvc.B().Hset().Key(sharedalarmkeys.RoomNamesCacheKey).FieldValue().FieldValue(roomID, alarmRecord.RoomName).Build(),
+            )
+        }
+        if alarmRecord.UserName != "" && alarmRecord.UserID != "" {
+            cmds = append(cmds,
+                cacheSvc.B().Hset().Key(sharedalarmkeys.UserNamesCacheKey).FieldValue().FieldValue(alarmRecord.UserID, alarmRecord.UserName).Build(),
+            )
+        }
+    }
+
+    for _, result := range cacheSvc.DoMulti(ctx, cmds...) {
+        if err := result.Error(); err != nil {
+            return fmt.Errorf("warm subscriber cache from alarms: execute batched cache writes: %w", err)
+        }
+    }
+
+    return nil
+}
```

## 8. P1 — dispatcher가 하위 장애 시 재큐잉 폭주를 만들 수 있음

문제 파일:

- `hololive-dispatcher-go/internal/dispatch/dispatcher.go`

현재는 send 실패 시 즉시 `Requeue()` 하고, `dispatchGroups()`는 개별 group 오류를 로그만 남기고 nil을 반환합니다.

즉, 하위 전송 계층(Iris/Kakao)이 깨졌을 때 다음이 가능합니다.

- dequeue
- send 실패
- 즉시 requeue
- dispatcher iteration overall success 취급
- 다음 루프에서 다시 dequeue
- 무한 반복

이 구조는 **retry budget도 없고, backoff도 없고, DLQ도 없습니다.** 장애 시 가장 나쁜 패턴입니다.

## 8.1 권장 방향

이 부분은 diff 한두 줄로 끝나는 수준이 아닙니다. 아래 중 하나가 필요합니다.

- envelope에 `attempt`, `next_visible_at` 추가 후 delayed retry
- 별도 retry ZSET/queue 도입
- max attempt 초과 시 parking/DLQ 이동
- per-iteration 오류 집계를 통해 outer loop backoff 유도

개념 diff는 아래 정도가 맞습니다.

```diff
 type AlarmQueueEnvelope struct {
     Notification domain.AlarmNotification `json:"notification"`
     ClaimKeys    []string                 `json:"claim_keys"`
     EnqueuedAt   string                   `json:"enqueued_at"`
     Version      uint8                    `json:"version"`
+    Attempt      int                      `json:"attempt,omitempty"`
+    RetryAfter   string                   `json:"retry_after,omitempty"`
 }
```

```diff
 if err := d.sender.SendMessage(ctx, group.RoomID, message); err != nil {
-    d.requeueEnvelopes(ctx, group.RoomID, group.Envelopes)
+    bumped := bumpRetry(group.Envelopes, time.Now())
+    d.requeueEnvelopesWithDelay(ctx, group.RoomID, bumped)
     return fmt.Errorf("dispatch group: send message: %w", err)
 }
```

이건 운영 안정성 측면에서 우선순위가 높지만, 단기 핫픽스로 넣기엔 변경 반경이 큽니다. P0/P1 캐시·알람 패치 뒤에 바로 착수하는 것이 맞습니다.

## 9. P2 — member adapter의 취소 전파 상실과 조용한 오류 은폐

문제 파일:

- `hololive-shared/pkg/service/member/adapter.go`

문제는 두 가지입니다.

첫째, `NewMemberServiceAdapter()`가 nil context면 `context.TODO()`, 아니면 `context.WithoutCancel(ctx)`를 사용합니다. 이건 호출자 lifecycle과 분리된 컨텍스트를 만들기 때문에 shutdown/cancel 전파가 끊깁니다.

둘째, `GetAllMembers()`나 `searchableMembers()`가 에러를 빈 슬라이스로 바꿔버립니다. 결과적으로 장애가 "검색 결과 없음"처럼 보입니다.

최소 패치는 아래입니다.

```diff
--- a/hololive/hololive-shared/pkg/service/member/adapter.go
+++ b/hololive/hololive-shared/pkg/service/member/adapter.go
@@
 func NewMemberServiceAdapter(ctx context.Context, cache *Cache, logger *slog.Logger) *ServiceAdapter {
     if ctx == nil {
-        ctx = context.TODO()
-    } else {
-        ctx = context.WithoutCancel(ctx)
+        ctx = context.Background()
     }
     return &ServiceAdapter{
         cache:  cache,
         ctx:    ctx,
         logger: logger,
@@
 func (a *ServiceAdapter) GetAllMembers() []*domain.Member {
     members, err := a.LoadAllMembers()
     if err != nil {
         a.logger.Warn("repository lookup failed in GetAllMembers", "error", err)
-        return []*domain.Member{}
+        return nil
     }
     return members
 }
```

여기서 더 나아가면 `FindMembersByName/Alias`는 `(result, error)` 반환으로 바꾸는 편이 좋습니다.

## 10. P2 — 문서가 현재 아키텍처를 설명하지 못함

문제 파일:

- `hololive-kakao-bot-go/docs/local/alarm-notification.md`

이 문서는 현재 코드와 불일치가 큽니다.

- 예전 bot ticker 기반 경로를 설명함
- 현재 runtime scheduler/checker/dedup/dispatcher 경로와 맞지 않음
- 옛 key format, 옛 파일 위치, `MinutesUntilCeil` 등 과거 개념을 서술함

즉, 이 문서는 onboarding 문서가 아니라 **오해를 생산하는 문서**입니다. 유지할 거면 현재 구조 기준으로 전면 재작성, 아니면 삭제가 낫습니다.

## 11. 타깃 minute 정책은 하나의 객체로 합쳐야 함

지금은 함수 여러 개가 조합되어 정책을 만들고 있습니다. 이건 테스트도 분산시키고, 버그 때 원인 추적도 어렵게 만듭니다.

권장 방향은 아래처럼 하나의 객체로 모으는 것입니다.

```diff
+type TargetMinutePolicy struct {
+    TargetMinutes        []int
+    PrimaryAdvanceMinute int
+}
+
+func NewTargetMinutePolicyFromConfig(targetMinutes []int) TargetMinutePolicy
+func NewTargetMinutePolicyFromPersisted(alarmAdvanceMinutes int, targetMinutes []int) TargetMinutePolicy
+func NewTargetMinutePolicyFromRuntimeAdvance(alarmAdvanceMinutes int) TargetMinutePolicy
+
+func (p TargetMinutePolicy) Clone() []int
+func (p TargetMinutePolicy) Contains(minute int) bool
+func (p TargetMinutePolicy) HighestCrossed(start time.Time, window EvaluationWindow) (int, bool)
```

이렇게 바꾸면 settings, runtime scheduler, checker, dedup가 같은 policy를 공유하게 되어 지금 같은 split-brain 버그를 줄일 수 있습니다.

## 12. 권장 적용 순서

### 1단계 — 바로 배포 가능한 핫픽스

- P0 5분 알람 버그 수정
- helper 테스트/YouTube checker 테스트 갱신

### 2단계 — 사용자 체감 장애 예방

- `stripedExecutor` non-blocking + saturated fallback
- persistence 실패 메트릭 추가

### 3단계 — 운영 비용 절감

- `loadSubscriberRoomsByChannel()` pipelining
- `WarmSubscriberCacheFromAlarms()` batched writes
- `compactUniqueStrings()` O(n²) 제거

### 4단계 — 구조 리팩토링

- target minute policy 단일 객체화
- dispatcher retry/DLQ 설계
- member adapter 오류 모델 정리
- stale docs 정리

## 13. 이번 리뷰에서 가장 중요한 판단

이번 번들 기준으로는 **5분 알람 누락의 1차 원인은 persisted target minute healing 누락이 아닙니다.** 그 부분은 이미 꽤 보정돼 있습니다. 지금 실제 운영 증상을 만드는 핵심은 아래입니다.

- startup/first observation
- capped evaluation window
- capped 상태에서 crossed target backfill 금지

즉, 사용자가 말씀하신 "기본적으로 5분전 알람이 가야 하는데 3분/1분 페일백만 온다"는 현상은 코드상 충분히 설명됩니다.

그리고 이 문제를 가장 안전하게 고치는 방법은, **초기 관측일 때만 bounded backfill을 허용하는 것**입니다.

