# Hololive 전체 레포 범위 재리뷰 보고서 (2026-04-14)

## 1. 범위와 전제

이번 리뷰는 `hololive-stream-ingester`만이 아니라, 방금 업로드된 전체 번들을 기준으로 다음 범위를 함께 봤다.

- `hololive-kakao-bot-go`
- `hololive-dispatcher-go`
- `hololive-llm-sched`
- `hololive-stream-ingester`
- `hololive-shared`
- `shared-go`
- 루트 `docs`, `scripts`, `admin-dashboard`

검토 방식은 세 층으로 나눴다.

1. 정적 구조 스캔: 모듈별 파일 수, 큰 파일, 경계 혼합, `*_additional_test.go` 누적 여부
2. 대표 코드 경로 점검: 알람/디스패치/유튜브 outbox/ACL/stream-ingester ops/runtime/LLM prompt
3. 저장소 거버넌스 점검: `docs/current`, 구조 점검 스크립트, threshold 파일, 로컬 산출물 누수

실행 검증은 제한이 있었다. 이 번들은 `go1.26.2` toolchain을 요구하지만, 현재 환경은 `go1.23.2`만 있고 외부 네트워크가 차단되어 있어 전체 `go test ./...`를 실행할 수 없었다. 실제 시도 결과는 다음과 같았다.

```text
go version go1.23.2 linux/amd64
go: downloading go1.26.2 (linux/amd64)
go: download go1.26.2: ... dial tcp ... proxy.golang.org ... connection refused
```

따라서 이번 결론은 **정적 분석 + 저장소 내 구조 게이트 실행 + 대표 코드 경로 검토** 기준이다.

## 2. 전체 결론

이번 번들은 이전 상태보다 분명히 좋아졌다. 특히 알람 큐, durable retry / DLQ, 문서 계층화(`docs/current`, `docs/history`, `docs/design`)는 이전보다 훨씬 정리되어 있다. 5분 전 알람 의미론도 현재 번들 기준으로는 이전 핵심 버그가 상당 부분 정리된 방향으로 보인다.

하지만 레포 전체 관점에서 보면, 아직 완성도가 가장 낮은 지점은 기능 그 자체보다 **경계와 실패 처리의 마감**이다.

가장 중요한 판단은 다음 네 가지다.

1. **실제 버그가 아직 남아 있다.** 특히 `hololive-shared/pkg/service/youtube/scheduler.go`의 알림 sent 마킹 로직은 성공 여부와 무관하게 “보냈다”로 기록할 수 있다.
2. **I/O 실패 시 hot-loop나 조용한 불일치가 생기는 경로가 남아 있다.** 대표적으로 outbox subscriber lookup failure와 ACL cache sync가 그렇다.
3. **AI 냄새의 실체는 코드 스타일이 아니라 반쯤 끝난 구조 분리다.** `shared/youtube`, `stream-ingester ops/runtime`, `bot internal/app`이 대표적이다.
4. **문서와 구조 게이트가 현재 코드 구조를 완전히 따라가지 못한다.** 즉 “문서가 좋아졌다”와 “SSOT가 완전히 맞다”는 아직 다르다.

## 3. 전체 레포에서 보이는 주요 구조 지표

### 3.1 모듈별 대략적인 크기

비테스트 Go 파일 기준으로 보면, 무게 중심은 명확하다.

- `hololive-shared`: 252개 비테스트 Go 파일
- `hololive-kakao-bot-go`: 154개
- `hololive-stream-ingester`: 76개
- `hololive-llm-sched`: 53개
- `shared-go`: 17개
- `hololive-dispatcher-go`: 7개

즉, 문제를 가장 크게 만드는 축은 `hololive-shared`와 `hololive-kakao-bot-go`, 그리고 그 다음으로 `hololive-stream-ingester`다.

### 3.2 가장 큰 파일들

비테스트 Go 파일 기준 상위 파일은 다음과 같다.

- `hololive/hololive-shared/pkg/service/youtube/poller/repository_batch.go` — 1214 LOC
- `hololive/hololive-shared/pkg/service/youtube/scheduler.go` — 1096 LOC
- `hololive/hololive-shared/pkg/service/youtube/outbox/delivery_timelines.go` — 940 LOC
- `hololive/hololive-shared/pkg/service/youtube/service.go` — 915 LOC
- `hololive/hololive-shared/pkg/service/youtube/poller/pollers.go` — 866 LOC
- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go` — 840 LOC
- `hololive/hololive-shared/pkg/service/youtube/tracking/repository.go` — 824 LOC

파일 크기만으로 악성 여부를 단정할 수는 없지만, 실제로도 이 파일들 상당수가 여러 책임을 동시에 가지고 있다.

### 3.3 큰 패키지 묶음

응집도 과다 패키지는 다음이 대표적이다.

- `hololive-stream-ingester/internal/ops` — 30개 파일, 6718 LOC
- `hololive-shared/pkg/service/youtube/outbox` — 22개 파일, 6444 LOC
- `hololive-shared/pkg/service/youtube/poller` — 11개 파일, 4321 LOC
- `hololive-shared/pkg/service/youtube/tracking` — 8개 파일, 3271 LOC
- `hololive-kakao-bot-go/internal/app` — 37개 파일, 3278 LOC
- `hololive-stream-ingester/internal/runtime` — 26개 파일, 3063 LOC

이 수치는 “어디부터 쪼개야 하는가”를 보여 준다. 레포 전체에서 우선순위가 가장 높은 구조 분리 대상은 `shared/pkg/service/youtube/*`, `stream-ingester/internal/ops`, `bot/internal/app`이다.

### 3.4 `*_additional_test.go` 누적

`*_additional_test.go`는 전체 번들에서 23개였다. 대부분이 `hololive-kakao-bot-go/internal/app` 쪽에 집중되어 있다.

이건 단순 취향 문제가 아니다. 보통 이런 파일은 “원래 책임 경계가 애매해서 임시 테스트를 얹기 쉬운 구조”에서 늘어난다. 즉 테스트 파일명 자체가 구조 냄새의 후행 지표다.

## 4. 실제 버그로 보는 항목

여기서는 “구조상 거슬린다”가 아니라, 운영상 잘못된 결과를 만들 가능성이 높은 항목만 따로 뽑았다.

### 4.1 `youtube/scheduler.go`: send 실패와 무관하게 sent로 마킹되는 버그

문제 파일:

- `hololive/hololive-shared/pkg/service/youtube/scheduler.go`

문제 함수:

- `dispatchMilestoneAlertWorks`
- `dispatchApproachingAlertWorks`
- 이후의 `markMilestoneNotificationsSent`, `markApproachingNotificationsSent`

현재 구현의 핵심 문제는 `sentNotifications`를 **실제 send 성공 전에** 채운다는 점이다.

```go
func (ys *schedulerImpl) dispatchMilestoneAlertWorks(...) []ytstats.MilestoneNotification {
    ...
    sentNotifications := make([]ytstats.MilestoneNotification, 0, len(works))
    ...
    for _, work := range works {
        sentNotifications = append(sentNotifications, work.notification)
        for _, room := range rooms {
            eg.Go(func() error {
                if err := sendMessage(room, work.message); err != nil {
                    ys.logger.Error(...)
                }
                return nil
            })
        }
    }
    _ = eg.Wait()
    return sentNotifications
}
```

이 로직이면 다음이 가능하다.

- 모든 방 전송이 실패해도 `sentNotifications`는 채워진다.
- 그 결과 `MarkMilestonesNotifiedBatch` / `MarkApproachingChatNotifiedBatch`가 호출된다.
- 즉, **실제로는 아무 데도 안 갔는데 DB에는 알림이 간 것으로 찍힐 수 있다.**

이건 가장 명백한 correctness bug다.

#### 왜 더 위험한가

이 버그는 단순히 로그만 남기고 끝나는 문제가 아니다.

- 재시도 기회를 잃는다.
- 운영자는 “왜 다음번에 안 나가지?”라는 증상만 보게 된다.
- sent flag가 오염되므로 사후 복구도 어렵다.

#### 권장 수정 방향

전송 단위 성공을 별도로 추적해서, **최소 한 방 이상 성공한 notification만** sent 목록에 넣어야 한다.

대표 diff는 다음 형태가 맞다.

```diff
 func (ys *schedulerImpl) dispatchMilestoneAlertWorks(
     ctx context.Context,
     sendMessage func(room, message string) error,
     rooms []string,
     works []milestoneAlertWork,
 ) []ytstats.MilestoneNotification {
     if len(works) == 0 || len(rooms) == 0 {
         return nil
     }

-    sentNotifications := make([]ytstats.MilestoneNotification, 0, len(works))
-    eg, _ := errgroup.WithContext(ctx)
+    sentByNotification := make(map[string]ytstats.MilestoneNotification, len(works))
+    var sentMu sync.Mutex
+    eg, _ := errgroup.WithContext(ctx)
     eg.SetLimit(4)

     for _, work := range works {
-        sentNotifications = append(sentNotifications, work.notification)
         for _, room := range rooms {
+            work := work
+            room := room
             eg.Go(func() error {
                 if err := sendMessage(room, work.message); err != nil {
                     ys.logger.Error("Failed to send milestone notification",
                         slog.String("room", room),
                         slog.String("member", work.notification.MemberName),
                         slog.Any("error", err))
+                    return nil
                 }
+
+                key := fmt.Sprintf("%s|%d", work.notification.ChannelID, work.notification.Value)
+                sentMu.Lock()
+                sentByNotification[key] = work.notification
+                sentMu.Unlock()
                 return nil
             })
         }
     }

     _ = eg.Wait()
-    return sentNotifications
+
+    sentNotifications := make([]ytstats.MilestoneNotification, 0, len(sentByNotification))
+    for _, notification := range sentByNotification {
+        sentNotifications = append(sentNotifications, notification)
+    }
+    return sentNotifications
 }
```

같은 패턴을 `dispatchApproachingAlertWorks`에도 동일하게 적용해야 한다.

#### 필요한 테스트

현재 성공 경로 테스트는 보이지만, 다음 두 회귀 테스트가 반드시 있어야 한다.

- 모든 방 전송 실패 시 sent batch가 호출되지 않는지
- 일부 방만 성공한 경우 해당 notification만 sent로 마킹되는지

### 4.2 `outbox/dispatcher_claim.go`: subscriber lookup failure 시 hot-loop 가능성

문제 파일:

- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_claim.go`

문제 지점:

```go
rooms, ok := roomsForItem(roomsByChannel, *item)
if !ok {
    subscriberLookupFailures++
    d.releaseOutboxLock(ctx, item.ID)
    continue
}
```

여기서 `roomsForItem(...)`이 `!ok`를 반환하는 경우는 “정상적으로 subscriber target을 계산하지 못한 경우”다. 그런데 현재 로직은 실패 backoff 없이 **lock만 풀고 끝낸다.**

그 결과는 다음과 같다.

- outbox row status는 여전히 `PENDING`
- `next_attempt_at`도 그대로
- 다음 cycle에서 거의 즉시 다시 claim 가능
- subscriber lookup이 일시적으로 깨진 상태면 같은 row를 계속 재처리

즉, 이건 DB/캐시 장애 시 **조용한 hot-loop**로 이어진다.

#### 권장 수정 방향

`enqueue delivery rows` 실패와 동일한 성격으로 보고, 적어도 backoff가 들어가야 한다. 가장 단순한 수정은 `markFailed(...)` 경로를 타게 만드는 것이다.

대표 diff는 다음과 같다.

```diff
 rooms, ok := roomsForItem(roomsByChannel, *item)
 if !ok {
     subscriberLookupFailures++
-    d.releaseOutboxLock(ctx, item.ID)
+    d.markFailed(ctx, item.ID, "subscriber lookup failed")
     continue
 }
```

더 정교하게 하려면 `markLookupRetry(...)` 같은 별도 함수를 두고 영구 실패가 아니라 “lookup 재시도” 카테고리로 관리하는 것이 더 좋다.

#### 운영상 기대 효과

- subscriber cache/repository 문제 시 반복 claim 폭주가 줄어든다.
- 문제 row가 backoff를 가지게 되어 tail latency와 DB churn이 줄어든다.
- outbox 장애 관측이 쉬워진다.

### 4.3 `acl/service.go`: cache sync 실패가 조용히 묻히는 불일치 문제

문제 파일:

- `hololive/hololive-kakao-bot-go/internal/service/acl/service.go`

대표 문제 코드:

```go
_ = s.cache.Del(ctx, key)
if len(rooms) > 0 {
    _, _ = s.cache.SAdd(ctx, key, rooms)
}
```

그리고 아래 경로들도 모두 비슷하다.

- `syncSettingsToValkey`
- `syncModeToValkey`
- `AddRoom`
- `RemoveRoom`

문제는 두 가지다.

첫째, 오류를 완전히 무시한다. DB와 메모리는 성공했는데 cache만 실패해도, 서비스는 성공처럼 보인다.

둘째, `Del -> SAdd` 순서의 전체 교체는 원자적이지 않다. `Del` 후 `SAdd`에서 실패하면 잠시 비어 있는 set이 노출되거나, 더 나쁘게는 영구 불일치가 남을 수 있다.

현재 hot path `IsRoomAllowed`는 메모리를 주로 보므로 즉시 큰 장애로 드러나지 않을 수 있다. 하지만 재시작 후 초기화나 다중 인스턴스 운영에서는 일관성 문제로 이어질 수 있다.

#### 권장 수정 방향

최소 수준으로는 cache 동기화 함수가 `error`를 반환하도록 바꾸고, 호출부에서 경고 로그나 롤백 판단을 하게 해야 한다.

```diff
-func (s *Service) syncRoomsToValkey(ctx context.Context, mode ACLMode) {
+func (s *Service) syncRoomsToValkey(ctx context.Context, mode ACLMode) error {
     ...
-    _ = s.cache.Del(ctx, key)
+    if err := s.cache.Del(ctx, key); err != nil {
+        return fmt.Errorf("sync acl rooms: del %s: %w", key, err)
+    }

     if len(rooms) > 0 {
-        _, _ = s.cache.SAdd(ctx, key, rooms)
+        if _, err := s.cache.SAdd(ctx, key, rooms); err != nil {
+            return fmt.Errorf("sync acl rooms: sadd %s: %w", key, err)
+        }
     }
+
+    return nil
 }
```

더 나은 안은 temp key에 `SADD` 후 rename/swap 하는 방식이다. 사용 중인 Valkey client가 그 기능을 직접 제공하지 않으면 pipeline script로 감싸는 편이 낫다.

## 5. 구조 냄새와 AI 냄새의 실체

여기서 말하는 AI 냄새는 “AI가 썼다”는 추정이 아니다. 실제 유지보수 비용을 높이는 패턴을 가리킨다.

### 5.1 `hololive-shared/pkg/service/youtube/*`: 가장 큰 구조 hotspot

이 영역은 레포 전체에서 가장 중요한 구조 hotspot이다.

구조 게이트 실행 결과도 이를 뒷받침한다.

`./scripts/architecture/check-go-module-loc.sh` 결과:

```text
FAIL: Go module LOC threshold violations detected
 - exceeded:hololive/hololive-shared/pkg/service/youtube/poller/pollers.go:866>850
 - exceeded:hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go:618>520
```

`./scripts/architecture/check-file-loc.sh` 결과에도 `youtube` 하위의 큰 파일들이 다수 잡힌다.

문제는 단순히 크기가 아니다.

- `poller/pollers.go`는 poller 정의 + persistence + watermarking + notification construction이 섞여 있다.
- `poller/scheduler.go`는 스케줄 heap, sync, worker lifecycle, rate limiting이 함께 있다.
- `outbox/*`는 durable delivery 쪽이 성숙했지만, 여전히 dispatch claim/enqueue/send/finalize가 패키지 단위로 너무 뭉쳐 있다.
- `tracking/repository.go`는 repository라기보다 query/use-case helper에 가깝다.

즉, 이 영역은 “shared utility”가 아니라 사실상 **여러 개의 별도 서브시스템**이다.

#### 권장 재구성 경계

`pkg/service/youtube`를 적어도 아래처럼 쪼개는 것이 맞다.

- `poller/runtime` — scheduler lifecycle, worker control
- `poller/jobs` — poll unit, registration, priority, interval
- `poller/persistence` — watermark, state, batch repo
- `outbox/claim`, `outbox/delivery`, `outbox/finalize` — 처리 단계별 분리
- `tracking/query` — 운영 분석용 query/report read model

지금처럼 “youtube” 밑에 기능별로 조금만 나눈 구조는 이미 한계다.

### 5.2 `hololive-stream-ingester`: runtime + ops + domain 경계가 아직 끝나지 않음

이건 사용자가 처음부터 거슬린다고 지적한 부분이고, 전체 리뷰에서도 그대로 재확인되었다.

대표적인 증거는 `internal/ops/runtime_target_baseline_bridge.go`다.

```go
type CommunityShortsTargetBaseline = runtimeapp.CommunityShortsTargetBaseline
...
func CollectCommunityShortsTargetBaseline(...) {
    return runtimeapp.CollectCommunityShortsTargetBaseline(...)
}
```

이 파일은 사실상 “ops가 runtime을 우회해서 쓰는 임시 브리지”다. 즉, `ops`와 `runtime` 사이에 있어야 할 별도 domain package가 비어 있다는 뜻이다.

게다가 `CollectCommunityShortsContinuousObservationReport()`는 여러 report collector를 조합하는데, 각 collector 상당수가 다시 `ProvideDatabaseResources(...)`를 호출한다. 실제 검색 결과도 다수다.

- `community_shorts_route_report.go`
- `community_shorts_latency_period_report.go`
- `community_shorts_send_counts_report.go`
- `community_shorts_delivery_logs_report.go`
- `community_shorts_latency_cause_report.go`
- `community_shorts_continuous_observation_report.go`
- 그 외 다수

이 구조는 watch 모드나 반복 실행에서 DB/session churn을 불필요하게 키운다.

#### 권장 재구성 경계

- `internal/communityshorts` — operational channels, baseline, route policy, observation window 같은 domain 규칙
- `internal/youtubecontrol` — poll target, refresher, resolver, registration
- `internal/ops/session` — DB/repository bundle 재사용
- `internal/ops/reports/*` — 각 리포트는 session을 주입받아 계산만 수행
- `internal/runtime` — 프로세스 lifecycle만 담당

즉, 지금 필요한 것은 “ops를 더 많이 추가하는 것”이 아니라 **domain을 빼내고 ops를 소비자 역할로 축소하는 것**이다.

### 5.3 `hololive-kakao-bot-go/internal/app`: 문제를 이미 알고 있지만 아직 끝내지 못한 상태

이 영역은 특이하게도, 코드보다 문서가 더 솔직하다. `docs/current/APP_BOOTSTRAP_BOUNDARY_GUIDE.md`는 이미 문제를 정확하게 인정한다.

문서에는 이렇게 적혀 있다.

- `internal/app` 한 디렉터리에 DI / wiring / runtime lifecycle / HTTP router / bot bootstrap / service bootstrap / integration runtime / test helper가 동시에 들어 있다.
- 이를 `bootstrap`, `runtime`, `http`, `wiring`으로 나눠야 한다.

즉, 이 구조는 “숨겨진 문제”가 아니라 **이미 인지된 부채**다. 다만 아직 구현이 절반 정도만 진행된 상태다.

이런 상태에서 `*_additional_test.go`가 많이 붙고, 얇은 helper가 복제되기 쉽다. 그래서 이 영역의 AI 냄새는 “자동 생성스러운 코드”라기보다 **패치 누적형 냄새**에 가깝다.

### 5.4 `hololive-llm-sched`: prompt 자산과 코드가 과하게 밀착돼 있음

문제 파일:

- `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_prompt.go`

이 파일은 700 LOC가 넘고, large prompt text와 prompt builder, 구조화 응답 타입 일부가 한 파일에 같이 있다.

`//go:embed` 자체는 좋은 선택이지만, 지금 파일은 “자산을 embed했다”가 아니라 **거대한 prompt 스펙이 코드 파일 자체를 점령한 상태**에 가깝다.

권장 방향은 다음과 같다.

- prompt 본문은 `.md` 또는 `.tmpl` 자산으로 분리
- prompt builder는 변수 치환과 context assembly만 담당
- schema structs는 별도 파일로 분리
- summarizer cache key / versioning은 별도 파일에서 관리

이건 당장 correctness bug는 아니지만, prompt 변경 PR의 review 품질을 높이는 데 효과가 크다.

## 6. 문서와 거버넌스 문제

### 6.1 문서 일부가 현재 코드와 어긋남

문제 문서:

- `hololive/hololive-kakao-bot-go/docs/local/alarm-notification.md`

이 문서는 다음처럼 적고 있다.

- send 실패 시 requeue
- render 실패 시 claim key release

하지만 현재 dispatcher 구현은 render/send failure 모두 durable retry / DLQ 정책을 가지는 방향으로 더 성숙해져 있다. 즉, **문서가 더 오래된 의미론을 설명하고 있다.**

이건 신규 참여자에게 꽤 위험하다. 알람 장애를 볼 때 문서를 믿고 잘못된 경로를 따라갈 수 있기 때문이다.

### 6.2 구조 threshold 파일이 현재 코드 구조를 따라가지 못함

문제 파일:

- `docs/architecture/file-loc-thresholds.txt`

여기에는 여전히 없는 경로가 들어 있다.

- `hololive/hololive-stream-ingester/internal/app/community_shorts_*.go`

하지만 실제 코드는 `internal/ops`와 `internal/runtime` 쪽에 있다. 그 결과 `check-file-loc.sh`가 “missing”을 잔뜩 띄우고, 동시에 새 실제 파일들에는 threshold가 빠져 “missing-threshold”도 같이 뜬다.

이건 단순 귀찮음이 아니다. 구조 게이트가 noisy하면, 팀은 결국 그 게이트를 신뢰하지 않게 된다.

### 6.3 로컬 patch artifact가 저장소에 추적되고 있음

문제 파일:

- `admin-dashboard/frontend/hololive_review_proposed.patch`

이 파일 안에는 `/mnt/data/...` 절대 경로와 과거 추출 번들 경로가 그대로 들어 있다.

```diff
--- /mnt/data/hololive-bot-full-extracted/...
+++ /mnt/data/hololive-bot-patchwork/...
```

이건 명백한 작업용 산출물 누수다.

더 문제는 현재 `scripts/architecture/check-tracked-local-artifacts.sh`가 `*.patch`, `*.diff`, `*.orig`, `*.rej`를 금지 목록에 넣지 않았다는 점이다. 즉, 거버넌스가 실제 누수 유형 하나를 놓치고 있다.

#### 권장 수정

```diff
 while IFS= read -r path; do
   case "${path}" in
     .worktrees/*|\
     .tasklists/*|\
     .runlogs/*|\
     .codex/*|\
     .claude/*|\
     .serena/*|\
     .gemini/*|\
     BUNDLE_MANIFEST.txt|\
     *.tar.gz|\
+    *.patch|\
+    *.diff|\
+    *.orig|\
+    *.rej|\
     .idea/*|\
     .vscode/*|\
     .omc/*|\
     */.idea/*|\
     */.vscode/*|\
     */.omc/*)
       violations+=("${path}")
       ;;
   esac
 done
```

그리고 해당 patch 파일은 삭제하는 것이 맞다.

## 7. 이미 좋아진 부분도 분명히 있다

비판적으로 봐도, 현재 레포가 예전보다 좋아진 부분은 분명히 인정해야 한다.

- `docs/current` 중심 문서 계층이 이전보다 명확하다.
- dispatcher / queue consumer 쪽 durable retry / DLQ 설계는 많이 성숙했다.
- workspace / architecture 스크립트가 있어 최소한 구조 부채를 계측하려는 의도가 분명하다.
- `hololive-kakao-bot-go/internal/app` 구조 문제를 문서가 이미 공개적으로 인정하고 있다.
- 알람 5분 의미론 버그는 현재 번들 기준으로는 예전보다 해결 쪽으로 많이 전진했다.

즉, 이 레포는 “방향을 모르는 상태”는 아니다. 현재 문제는 **방향은 맞는데 마감이 덜 된 상태**다.

## 8. 전체 통합 우선순위

### 1단계 — 실제 버그부터 닫기

가장 먼저 해야 할 것은 구조 미화가 아니라 correctness다.

1. `hololive-shared/pkg/service/youtube/scheduler.go`
   - sent 마킹을 실제 send 성공 기준으로 변경
   - milestone / approaching 모두 동일하게 수정
   - all-fail / partial-fail 테스트 추가

2. `hololive-shared/pkg/service/youtube/outbox/dispatcher_claim.go`
   - subscriber lookup failure에 backoff 부여
   - bare release lock 제거

3. `hololive-kakao-bot-go/internal/service/acl/service.go`
   - Valkey sync 오류를 반환/로그 처리
   - `Del -> SAdd` 전체 교체의 실패 노출 강화

이 세 개는 미뤄서는 안 된다.

### 2단계 — 구조 hotspot을 패키지 경계로 분리

우선순위는 다음 순서가 맞다.

1. `hololive-shared/pkg/service/youtube/*`
2. `hololive-stream-ingester/internal/ops` + `internal/runtime`
3. `hololive-kakao-bot-go/internal/app`
4. `hololive-llm-sched/internal/service/majorevent/summarizer`

이 순서인 이유는, 앞의 둘이 가장 많은 운영 경로와 I/O를 잡고 있기 때문이다.

### 3단계 — 문서와 게이트를 current code에 맞추기

1. `docs/local/alarm-notification.md`를 current semantics에 맞게 고치거나 historical로 내리기
2. `docs/architecture/file-loc-thresholds.txt`를 현재 경로로 갱신
3. `check-tracked-local-artifacts.sh`에 patch/diff 계열 추가
4. `admin-dashboard/frontend/hololive_review_proposed.patch` 제거

이 단계는 사소해 보이지만, 실제로는 “구조 관리가 계속 유지되느냐”를 결정한다.

## 9. 코드 레벨로 아주 구체적인 가이드

### 9.1 `youtube/scheduler.go`

수정 목적은 단순하다.

- 전송 시도와 전송 성공을 구분한다.
- 성공한 notification만 sent batch로 넘긴다.

구현 원칙은 다음이 좋다.

- 방 단위 성공 여부를 모은다.
- notification 단위로 dedupe 한다.
- send failure는 여전히 로그로 남기되, sent batch에는 넣지 않는다.
- sent mark 실패는 별도 warn으로 남긴다.

또 테스트는 다음 세 개로 쪼개는 편이 좋다.

- `TestDispatchMilestoneAlertWorks_MarksOnlySuccessfulNotifications`
- `TestDispatchMilestoneAlertWorks_DoesNotMarkWhenAllRoomsFail`
- `TestDispatchApproachingAlertWorks_MarksOnlySuccessfulNotifications`

### 9.2 `outbox/dispatcher_claim.go`

이 파일은 “claim”, “enqueue”, “lock release”, “mark failed” 의미론을 분명하게 구분해야 한다.

현재 구조는 `roomsForItem(...)`에서 `!ok`가 난 경우를 enqueue failure보다 가볍게 취급한다. 이건 맞지 않다. target 계산 실패도 dispatch pipeline 입장에서는 실패다.

따라서 아래 두 안 중 하나를 택해야 한다.

- 단순안: `markFailed(ctx, item.ID, "subscriber lookup failed")`
- 권장안: `markLookupRetry(ctx, item.ID, reason)` 같은 별도 함수 추가

권장안이 더 좋은 이유는 모니터링 label을 분리할 수 있기 때문이다. lookup/cache/repository 계열 장애가 send 실패와 섞이지 않는다.

### 9.3 `acl/service.go`

이 파일은 메모리/DB/Valkey 세 계층을 동시에 관리한다. 이런 파일에서 오류를 먹으면 나중에 원인 추적이 거의 불가능해진다.

수정 기준은 다음과 같다.

- `syncSettingsToValkey`, `syncModeToValkey`, `syncRoomsToValkey`는 `error` 반환형으로 바꾼다.
- `SetEnabled`, `SetMode`, `AddRoom`, `RemoveRoom`은 cache sync error를 logger에 남기고, 필요하면 호출자에게 반환한다.
- 가능하면 “replace set”는 temp key → swap 방식으로 바꾼다.

가장 보수적인 적용 순서는 아래다.

1. 먼저 error를 무시하지 않도록만 바꾼다.
2. 이후 replace path를 원자화한다.
3. 마지막으로 cache sync 전용 helper/pipeline으로 정리한다.

### 9.4 `stream-ingester`

이 영역은 수정 순서가 중요하다. 한 번에 다 옮기면 import churn이 크다.

권장 순서는 다음이다.

1. `internal/communityshorts` 신설
   - `operational_channels.go`
   - `route_policy.go`
   - `target_baseline.go`
   - `observation_window.go`

2. `internal/ops/session` 신설
   - `type Bundle struct { DB ..., TrackingRepo ..., OutboxRepo ... }`
   - `func Open(ctx, cfg, logger) (*Bundle, func(), error)`

3. 각 report collector를 `CollectWithSession(...)` 형태로 이관

4. `runtime_target_baseline_bridge.go` 삭제

즉, 먼저 domain package를 만들고, 그 다음 session을 만들고, 마지막에 bridge를 제거해야 안전하다.

### 9.5 `bot/internal/app`

이 영역은 새 구조를 설계할 필요가 없다. 이미 문서가 잘 써 있다. 필요한 것은 실행이다.

안전한 방식은 다음이다.

- 외부 import path는 당장 바꾸지 않는다.
- 내부 구현만 `bootstrap/`, `runtime/`, `http/`, `wiring/`로 이동한다.
- 기존 `internal/app` 패키지에는 forwarding façade만 남긴다.

즉, 구조 분리 PR의 목표는 “모든 import를 한 번에 바꾸기”가 아니라 **책임 경계를 내부에서 먼저 끝내는 것**이다.

### 9.6 `llm-sched`

여기는 코드 분리가 가장 단순하다.

- `summarizer_prompt.go` → `prompt_assets.go`, `prompt_builder.go`, `prompt_schema.go`로 나눈다.
- 큰 prompt text는 `.md` 또는 `.tmpl` asset 파일로 뺀다.
- `bootstrap_llm_scheduler.go`는 runtime bootstrap과 client wiring을 분리한다.

이 영역은 correctness보다 reviewability와 변경 추적성이 핵심이다.

## 10. 이 번들에서 가장 먼저 손대야 할 파일 목록

정말 실무적으로 줄이면, 아래 파일들부터 순서대로 보는 것이 맞다.

1. `hololive/hololive-shared/pkg/service/youtube/scheduler.go`
2. `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_claim.go`
3. `hololive/hololive-kakao-bot-go/internal/service/acl/service.go`
4. `hololive/hololive-stream-ingester/internal/ops/runtime_target_baseline_bridge.go`
5. `hololive/hololive-stream-ingester/internal/ops/community_shorts_continuous_observation_report.go`
6. `hololive/hololive-kakao-bot-go/internal/app/*` 전체 분리 작업
7. `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_prompt.go`
8. `docs/architecture/file-loc-thresholds.txt`
9. `scripts/architecture/check-tracked-local-artifacts.sh`
10. `hololive/hololive-kakao-bot-go/docs/local/alarm-notification.md`
11. `admin-dashboard/frontend/hololive_review_proposed.patch`

## 11. 최종 판단

이번 전체 재리뷰에서 가장 중요한 결론은 이것이다.

이 레포는 이미 많은 부분이 정리되어 있고, 방향도 맞다. 하지만 아직 “끝난 구조”가 아니라 **반쯤 마감된 구조**다. 그래서 표면상으로는 잘 동작하는데, 실패 경로와 운영 문서, 거버넌스 스크립트, 패키지 경계에서 계속 작은 틈이 보인다.

그 틈 중 실제 버그로 당장 닫아야 하는 것은 세 가지다.

- `youtube/scheduler.go`의 sent 마킹 버그
- `outbox/dispatcher_claim.go`의 subscriber lookup failure hot-loop 가능성
- `acl/service.go`의 cache sync 오류 은닉

그 다음에 구조적으로 가장 큰 축은 두 가지다.

- `hololive-shared/pkg/service/youtube/*`
- `hololive-stream-ingester`의 `ops/runtime/domain` 경계 분리

그리고 마지막으로, 문서와 구조 게이트를 current code와 다시 일치시켜야 한다. 그래야 이번 정리가 일회성 패치가 아니라 유지되는 기준이 된다.
