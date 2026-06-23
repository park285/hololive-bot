# Live-status 스크래퍼 폴백 페이싱 플랜 코드 리뷰 및 v3 개선안

리뷰 기준일: 2026-06-23  
검증 기준 브랜치: `main`  
리뷰 대상: `Live-status 스크래퍼 폴백 페이싱 Implementation Plan (v2, 4-way 리뷰 반영)`

## 결론

v2 플랜의 큰 방향은 맞다. `youtube-producer` 프로세스 안에서 1차 폴링과 Holodex live-status 폴백이 같은 YouTube scraper client와 같은 rate limiter를 공유하고 있으므로, fallback에서 14개 채널을 한 번에 non-blocking admission으로 던지는 현재 구조는 실제로 `admission_deferred` 대량 발생과 live-session 오종료 위험을 만든다.

다만 v2를 그대로 구현하면 아직 두 가지 P0급 빈틈이 남는다.

첫째, v2의 “deferred를 failed map에 넣어 session 보존”이라는 계약은 provider 레벨에서는 맞지만, `LivePoller`와 `liveBatchPoller` 레벨까지 내려가면 아직 soft가 아니다. 현재 `LivePoller.pollBatchChannel`은 `failures[channelID]`가 있으면 무조건 error를 반환하고, `liveBatchPoller.Poll`은 이 per-channel error들을 join해서 batch poll 자체를 실패 처리한다. 즉 `GetChannelsLiveStatusWithFailures`가 nil hard error를 반환해도 scheduler 관점에서는 all-deferred batch가 계속 실패 run으로 보일 수 있다. 반대로 non-WithFailures 진입점에서 all-deferred를 empty streams + nil error로 바꾸면, 단일 `Poll` 경로는 empty stream을 “방송 없음”으로 해석하고 `markEndedSessions`를 호출할 수 있다. 따라서 v3에는 “deferred 때문에 session close는 막되 poller run은 hard failure로 보지 않는” 명시적인 cross-package contract가 필요하다.

둘째, `WaitWithBucket` 롤백 수정은 zero-wait local commit까지 포함해야 한다. 현재 `reserveLocalWait`는 첫 호출 또는 interval이 이미 지난 호출에서도 `lastTime`을 commit하지만 `reserved=false`를 반환한다. v2 문구대로 “waitLocal이 reservation handle을 반환”만 구현하면, 기다림이 없던 local commit 뒤 distributed limiter가 에러를 반환하는 경로는 여전히 local slot을 태운다. 이 경로가 실제 Valkey 장애 시 가장 흔할 수 있으므로 반드시 테스트로 고정해야 한다.

따라서 v3의 판정은 다음과 같다.

- 방향성: 유지한다.
- 구현 순서: “scraper blocking seam + WaitWithBucket 정확한 rollback”과 “live-status deferred contract”를 먼저 분리해서 고정한다.
- `GetChannelsLiveStatus`의 all-deferred soft 처리는 단독으로 넣지 않는다. session-sensitive caller가 WithFailures 또는 typed deferred를 통해 반드시 구분할 수 있게 만든 뒤 적용한다.
- 공식 스케줄 페이지 batch 대체는 현 코드 기준 불가로 확정해도 된다.

## 코드로 확인된 사실

### 1. 공식 스케줄 batch는 live-status 대체 소스가 아니다

`htmlscraper.fetchAllStreams`는 `/lives/hololive`를 한 번 요청하고 page cache + singleflight로 dedupe한다. 따라서 “1회 요청 batch 소스가 존재하는가?”라는 Task 0 질문 자체는 의미가 있다.

하지만 이 경로의 `buildStream`은 모든 stream에 `domain.StreamStatusUpcoming`을 하드코딩한다. 반면 YouTube channel page 기반 `convertEventToStream`은 scraper event의 `Status`가 `LIVE`일 때만 `domain.StreamStatusLive`로 매핑한다. 즉 공식 스케줄은 “일정/예정”에는 쓸 수 있어도 live-session 상태 판정에는 쓸 수 없다.

v2의 Task 0 결론은 코드와 일치한다. 이 결정은 v3에서 “이미 검증 완료된 non-issue”로 격상하고, 구현 PR에서는 다시 흔들지 않는 편이 낫다.

### 2. 현재 scraper fetch admission은 non-blocking만 있다

`scraping.FetchPolicy`에는 `MaxAttempts`, `PerAttemptTimeout`, retry delay 관련 필드만 있고 admission blocking 여부를 담는 필드가 없다. `fetchPage`는 `resolveFetchPolicy`로 retry policy만 해석한 뒤 매 attempt마다 `fetchPagePreflight(ctx, pageURL)`를 호출한다. `fetchPagePreflight`는 항상 `TryReserveWithBucket`을 사용하고, local 또는 distributed admission이 허용되지 않으면 `AdmissionDeferredError`를 반환한다.

따라서 v2의 “fallback 전용 blocking admission seam”은 필요하다. 단, bool만 넘기기보다 private 레벨에서는 `fetchPagePreflight(ctx, pageURL, resolvedPolicy)`처럼 정책 전체를 넘기는 쪽이 향후 필드 추가에 안전하다. public policy는 그대로 `FetchPolicy.AdmissionBlocking bool`이면 충분하다.

### 3. `fetchChannelSourcePage`는 이미 policy variadic을 받는다

`fetchChannelSourcePage(ctx, operation, channelID, pageURL, source, policy ...FetchPolicy)`는 이미 내부에서 `fetchPage(ctx, pageURL, policy...)`로 전달한다. 따라서 `GetUpcomingEventsWaitAdmission` 구현은 생각보다 작아도 된다.

권장 형태는 다음과 같다.

```go
func (c *Client) GetUpcomingEvents(ctx context.Context, channelID string) ([]*UpcomingEvent, error) {
    return c.getUpcomingEvents(ctx, channelID)
}

func (c *Client) GetUpcomingEventsWaitAdmission(ctx context.Context, channelID string) ([]*UpcomingEvent, error) {
    return c.getUpcomingEvents(ctx, channelID, LiveStatusFallbackFetchPolicy)
}
```

여기서 `getUpcomingEvents`는 URL 생성과 parser 로직을 공유하고, `fetchChannelSourcePage(..., policy...)`만 호출하면 된다. 별도 HTTP 경로나 parser 복제는 필요 없다.

### 4. `WaitWithBucket`에는 실제 rollback 비대칭이 있다

현재 `WaitWithBucket`은 local wait를 먼저 수행하고 distributed wait를 나중에 수행한다.

```go
if err := r.waitLocal(ctx); err != nil {
    return err
}
return r.waitDistributed(ctx, bucket)
```

`TryReserveWithBucket`은 local reservation을 먼저 잡은 뒤 distributed admission이 실패하거나 거절되면 `rollbackLocalReservation`을 호출한다. 반면 `WaitWithBucket`은 distributed error에서 rollback하지 않는다. v2가 지적한 reliability bug는 실제다.

v3에서는 다음 두 경로를 모두 테스트해야 한다.

- waited commit rollback: 두 번째 호출처럼 local interval 때문에 실제 timer를 기다린 뒤 distributed limiter가 error를 반환하는 경우
- zero-wait commit rollback: 첫 호출 또는 interval이 이미 지난 호출처럼 wait는 0이지만 `lastTime`은 commit된 뒤 distributed limiter가 error를 반환하는 경우

두 번째가 빠지면 Valkey 장애 직후 첫 request가 local slot을 태우는 문제는 그대로 남는다.

### 5. shared limiter starvation 전제는 코드와 맞다

`youtube_producer_runtime_builder.go`의 resource builder는 `ProvideYouTubeProducerRateLimiterWithConfig`로 만든 `sharedRL`을 `polling.BuildSharedClient`에 넣고, 같은 `scraperClient`를 `ProvideScraperServiceWithYouTubeProducer`를 통해 Holodex provider의 scraper service에도 넣는다. 즉 같은 프로세스에서 live poller와 Holodex fallback이 같은 scraper client, 같은 rate limiter를 공유한다.

또한 live batch registration의 기본 chunk size는 40이다. 14채널은 한 batch 안에 들어가므로, Holodex `/users/live` 장애 시 fallback 루프가 채널 수만큼 YouTube channel page를 연속으로 시도할 수 있다.

### 6. 현재 live poller failure contract는 “실패 채널은 session close를 막는다”이다

`LivePoller.pollBatchChannel`은 `failures[channelID]`가 있으면 `pollLiveStreams`를 호출하지 않고 error를 반환한다. 이 때문에 `markEndedSessions`도 호출되지 않는다. 이 점은 v2의 “failed map을 session 보존 계약으로 사용한다”는 전제와 맞다.

그러나 이 구조는 deferred까지 error로 반환한다. 즉 session close는 막지만 batch run 자체는 실패할 수 있다. v3에서는 “session close skip”과 “poller hard failure”를 분리해야 한다.

## v2에서 유지할 부분

아래 항목은 그대로 유지하는 것이 좋다.

1. 분리 rate limiter는 금지한다. 같은 egress/IP에 독립 local 3s gate를 두 개 만들면 단일 프로세스 안에서 사실상 2 req / 3s가 되어 429 가능성을 키운다.
2. blocking admission은 fallback 전용 method로 격리한다. 기존 `FetchFromYouTubeProducer`와 `FetchChannel` 경로를 in-place로 바꾸면 channel schedule 조회가 갑자기 blocking으로 변한다.
3. `LiveStatusFallbackFetchPolicy.MaxAttempts = 1`은 타당하다. retry가 preflight를 재진입하므로 blocking admission과 retry를 함께 쓰면 attempt마다 다시 대기한다.
4. cursor rotation과 per-cycle cap은 필요하다. blocking만 넣으면 한 cycle에서 14채널 전체를 순차 대기하여 primary polling을 더 오래 점유할 수 있다.
5. cooldown, admission deferred, budget 미시도는 “방송 없음”이 아니라 “이번 cycle 판단 유예”로 분류해야 한다.
6. official schedule batch는 live-status 대체로 사용하지 않는다.

## v3에서 반드시 바꿔야 할 부분

### P0-1. `deferred`를 cross-package typed contract로 만든다

v2는 `errLiveStatusFallbackDeferred`를 holodexprovider 내부 sentinel로 제안한다. 이것만으로는 부족하다. `LivePoller`와 `liveBatchPoller`는 holodexprovider 내부 sentinel을 알 수 없고, 알 수 없다면 deferred를 hard poller error처럼 취급한다.

권장안은 dependency가 가벼운 별도 package를 두는 것이다.

예시:

```go
// pkg/service/youtube/livestatus/deferred.go
package livestatus

var ErrDeferred = errors.New("live status deferred")

type DeferredError struct {
    Reason string
    Err    error
}

func (e *DeferredError) Error() string { ... }
func (e *DeferredError) Unwrap() error { return e.Err }

func IsDeferred(err error) bool {
    if errors.Is(err, ErrDeferred) {
        return true
    }
    var marker interface{ LiveStatusDeferred() bool }
    return errors.As(err, &marker) && marker.LiveStatusDeferred()
}
```

`holodexprovider`는 budget/cap 미시도, ctx budget 만료, `scraper.ErrTransientCooldown`, `scraper.IsAdmissionDeferred`, distributed admission unavailable을 이 타입으로 감싼다. `poller` package는 이 package만 import해서 `livestatus.IsDeferred(fetchErr)`를 판단한다. 이렇게 하면 holodexprovider와 poller 사이에 import cycle 없이 의미를 공유할 수 있다.

### P0-2. `LivePoller`는 deferred일 때 session close를 skip하되 hard error로 반환하지 않아야 한다

현재 구조에서 `failures[channelID]`를 그대로 error 반환하면 `liveBatchPoller.Poll`이 joined error를 반환한다. all-deferred를 provider에서 soft 처리해도 batch poller는 실패처럼 보인다.

권장 변경:

```go
func (p *LivePoller) pollBatchChannel(..., failures map[string]error, ...) error {
    if fetchErr, ok := failures[channelID]; ok {
        if livestatus.IsDeferred(fetchErr) {
            // 판단 유예: streams 없음으로 markEndedSessions를 호출하지 않고,
            // scheduler에는 hard failure로 올리지 않는다.
            return nil
        }
        return fmt.Errorf("failed to get live streams: %w", fetchErr)
    }
    return p.pollLiveStreams(ctx, channelID, streams, now)
}
```

이렇게 하면 deferred는 “이번 cycle skip”이 되고, 실제 parser/blocked/forbidden 등 hard failure만 poller error가 된다.

### P0-3. 단일 `Poll` 경로가 non-WithFailures soft 결과를 오해하지 않게 한다

현재 `LivePoller.fetchLiveStreams`는 provider가 있으면 무조건 `GetChannelsLiveStatus(ctx, []string{channelID})`를 호출한다. detailed provider가 있어도 단일 Poll에서는 `GetChannelsLiveStatusWithFailures`를 쓰지 않는다.

v2처럼 non-WithFailures `GetChannelsLiveStatus`가 all-deferred에서 empty streams + nil error를 반환하면, 단일 Poll은 empty streams를 정상 결과로 보고 `markEndedSessions`를 호출할 수 있다.

따라서 v3에서 둘 중 하나를 선택해야 한다.

권장안 A:

- `LivePoller.fetchLiveStreams`도 provider가 `LiveStatusWithFailuresProvider`이면 detailed path를 우선 사용한다.
- 단일 채널 failure가 deferred이면 `livestatus.ErrDeferred`를 반환하거나 별도 skip 결과를 도입한다.
- `Poll`에서는 deferred error를 hard failure로 올리지 않고 session close만 skip한다.

권장안 B:

- non-WithFailures `GetChannelsLiveStatus`는 all-deferred를 nil error로 숨기지 않는다.
- 대신 UI/API처럼 session mutation이 없는 caller만 별도 soft wrapper를 사용한다.

현 코드 구조상 A가 더 일관적이다. “WithFailures는 session-sensitive, non-WithFailures는 best-effort UI”라는 주석을 함수 위에 명시해야 한다.

### P1-1. `WaitWithBucket` rollback은 zero-wait local commit까지 포함한다

v3 patch shape는 다음이 안전하다.

```go
func (r *RateLimiter) WaitWithBucket(ctx context.Context, bucket string) error {
    bucket = normalizeBucket(bucket)
    reservation, committed, err := r.waitLocal(ctx)
    if err != nil {
        return err
    }
    if err := r.waitDistributed(ctx, bucket); err != nil {
        if committed {
            r.rollbackLocalReservation(reservation)
        }
        return err
    }
    return nil
}
```

중요한 점은 `waitLocal` 또는 `reserveLocalWait`가 “timer wait가 있었는가”가 아니라 “local lastTime을 commit했는가”를 반환해야 한다는 것이다. 첫 호출도 commit이므로 rollback 대상이다.

테스트는 최소 두 개가 필요하다.

- `TestWaitWithBucketRollsBackWaitedLocalReservationOnDistributedError`
- `TestWaitWithBucketRollsBackImmediateLocalReservationOnDistributedError`

두 테스트 모두 같은 package `ratelimiter`에서 `r.lastTime`을 직접 확인할 수 있다.

### P1-2. live batch budget accounting을 `MaxAttempts=1`에 맞춘다

`liveBatchYouTubeScraperFallbackUnits(channelCount)`는 현재 `channelCount * scraper.FetchPageMaxAttempts`를 반환한다. v3에서 live-status fallback 전용 policy를 `MaxAttempts=1`로 만들면 registration budget은 실제 fallback request unit보다 3배 크게 잡힌다.

보수적으로 크게 잡는 것이 안전한 경우도 있지만, 이 값은 global budget admission이나 scheduling 우선순위에 영향을 줄 수 있다. v3에서는 다음 중 하나를 택해야 한다.

- 의도적으로 보수 유지: 함수명 또는 주석에 “worst-case legacy budget; live-status fallback policy is capped separately”라고 남긴다.
- 실제 정책 반영: `liveBatchYouTubeScraperFallbackUnits`가 `scraper.LiveStatusFallbackFetchPolicy.MaxAttempts`를 사용한다.

cap과 wall-clock budget까지 들어간다면 실제 per-run upper bound는 `min(channelCount, liveStatusFallbackMaxPerCycle) * LiveStatusFallbackFetchPolicy.MaxAttempts`가 더 정확하다. 다만 이 함수가 config를 받지 않는 현재 구조에서는 runtime config 반영이 어렵다. 그래서 최소 v3에서는 `MaxAttempts=1` 차이를 문서화하고, 후속으로 registration config 주입을 검토하는 편이 현실적이다.

### P1-3. config 구조를 명확히 잡는다

v2는 `liveStatusFallbackMaxPerCycle`, `liveStatusFallbackWallClockBudget`를 config화하라고만 한다. 현재 `HolodexConfig`에는 fallback 전용 config가 없고, `Service`에는 `concurrency`만 저장된다.

권장 구조:

```go
type HolodexLiveStatusFallbackConfig struct {
    MaxPerCycle     int
    WallClockBudget time.Duration
    DeadlineMargin  time.Duration
}

type HolodexConfig struct {
    ...
    LiveStatusFallback HolodexLiveStatusFallbackConfig
}
```

기본값은 다음 정도가 안전하다.

- `MaxPerCycle`: 4
- `WallClockBudget`: 12s 또는 `YOUTUBE_PRODUCER_REQUEST_INTERVAL_SECONDS * MaxPerCycle`보다 작게 시작
- `DeadlineMargin`: 250ms ~ 500ms

환경변수 예시:

- `HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE`
- `HOLODEX_LIVE_STATUS_FALLBACK_WALL_CLOCK_BUDGET_SECONDS`
- `HOLODEX_LIVE_STATUS_FALLBACK_DEADLINE_MARGIN_MS`

검증 규칙:

- `MaxPerCycle <= 0`이면 fallback scraper 시도 자체를 끄는 값인지, config error인지 결정한다. 운영 안정성을 위해 기본은 config error가 낫다.
- `WallClockBudget <= 0`이면 config error가 낫다.
- `DeadlineMargin < 0`은 config error다.

### P1-4. Valkey/distributed admission error 판별은 string matching으로 하지 않는다

v2는 “Valkey-admission-error는 deferred”라고 한다. 그런데 현재 error wrapping은 `distributed rate limiter allow failed: %w` 또는 preflight wrapper 정도라서 안정적인 sentinel이 없다.

v3에서는 ratelimiter 또는 scraper public layer에 sentinel/predicate를 추가해야 한다.

예시:

```go
var ErrDistributedRateLimiterUnavailable = errors.New("distributed rate limiter unavailable")
```

그리고 `nextDistributedWait`와 `tryReserveDistributedAdmission`에서 이 sentinel을 wrapping한다. holodexprovider는 `errors.Is(err, scraper.ErrDistributedRateLimiterUnavailable)` 또는 public predicate를 사용한다.

string contains로 `distributed rate limiter allow failed`를 찾는 방식은 테스트는 통과해도 장기 유지보수성이 낮다.

### P1-5. source-level hard error 검사는 deferred를 제외한 failed subset에만 적용한다

현재 `resolveChannelsLiveStatusFallback`은 `firstChannelsLiveStatusSourceLevelError(channelIDs, failed)`를 먼저 검사한다. v3에서 `failed`와 `deferred`를 합쳐 caller에 넘긴다면, source-level hard error 검사에는 반드시 실제 failed만 넣어야 한다.

추천 구조:

```go
streams, failed, deferred := h.getChannelsLiveStatusFromScraper(...)
if sourceLevelErr := firstChannelsLiveStatusSourceLevelError(channelIDs, failed); sourceLevelErr != nil { ... }
combined := mergeFailures(failed, deferred)
```

이 순서를 지키면 budget/cooldown/admission deferred가 source-level hard error로 승격되는 일을 막을 수 있다.

### P2-1. integration test는 `fetchUpcoming` injection으로는 blocking admission을 검증할 수 없다

`htmlscraper.NewTestServiceWithHTTPClient`는 `fetchUpcoming` function injection을 지원한다. 이 injection을 쓰면 `youtubeProducer.GetUpcomingEventsWaitAdmission`를 우회하므로 blocking admission을 검증하지 못한다.

실제 blocking path 검증은 다음처럼 해야 한다.

- `scraper.NewClient`에 `WithRateLimiter(scraper.NewRateLimiter(interval))`를 넣는다.
- network를 피하려면 `WithFetcherEngine(scraper.FetcherEngineBrowserSnapshot)` + fake `BrowserSnapshotFetcher`를 사용한다.
- fake fetcher는 `GetUpcomingEvents` parser가 읽을 수 있는 최소 HTML/ytInitialData를 반환한다.
- htmlscraper `Service`는 `NewServiceWithYouTubeProducer(..., client, ...)` 또는 테스트 helper 확장으로 실제 client를 물린다.
- `FetchFromYouTubeProducerWaitAdmission`을 연속 호출했을 때 두 번째 호출 시간이 interval 이상 지연되는지 boundary로 확인한다.

이 테스트가 없으면 Task 1의 unit test는 통과하지만 htmlscraper public seam이 잘못 배선되어도 놓칠 수 있다.

### P2-2. Go test path는 실제 module path로 적는다

v2의 `go test ./...scraper/...` 같은 패턴은 사람이 읽기에는 의도가 보이지만 CI 문서로는 애매하다. v3에는 실제 repo path를 적는 편이 낫다.

권장:

```bash
cd hololive/hololive-shared
go test ./pkg/service/youtube/scraper/...
go test ./pkg/service/holodex/...

cd ../hololive-youtube-producer
go test ./internal/runtime/polling/...
go test ./...
```

workspace root에서 `go work`가 보장되지 않는다면 각 module directory에서 실행한다.

## v3 권장 작업 순서

### Task A. live-status deferred contract 먼저 고정

- `pkg/service/youtube/livestatus` 같은 작은 package를 만든다.
- `ErrDeferred`, `DeferredError`, `IsDeferred(err)`를 정의한다.
- holodexprovider fallback의 budget/cap/cooldown/admission-deferred를 이 타입으로 감싼다.
- `LivePoller.pollBatchChannel`은 deferred failure를 session close skip + nil poller error로 처리한다.
- `LivePoller.fetchLiveStreams` 단일 path도 detailed provider를 우선 사용하도록 바꾼다.
- non-WithFailures `GetChannelsLiveStatus` 위에는 session-sensitive caller가 쓰면 안 된다는 주석을 추가하거나, all-deferred nil error 정책을 철회한다.

### Task B. scraper blocking admission seam 추가

- `FetchPolicy`에 `AdmissionBlocking bool` 추가.
- `LiveStatusFallbackFetchPolicy` 추가: `MaxAttempts: 1`, `AdmissionBlocking: true`, per-attempt timeout은 기존 default timeout 또는 명시값.
- `resolveFetchPolicy`는 bool을 숫자 필드와 다르게 항상 override한다는 주석을 둔다.
- `fetchPagePreflight`는 bool parameter 대신 resolved policy를 받는 형태를 우선 고려한다.
- blocking이면 `WaitWithBucket`, non-blocking이면 기존 `TryReserveWithBucket`.
- `GetUpcomingEventsWaitAdmission`은 기존 parser와 `fetchChannelSourcePage`를 재사용한다.
- htmlscraper에는 기존 `FetchFromYouTubeProducer`를 유지하고 `FetchFromYouTubeProducerWaitAdmission`만 추가한다.

### Task C. `WaitWithBucket` rollback 정확화

- local commit 여부와 reservation handle을 반환하도록 `waitLocal`을 바꾼다.
- zero-wait commit도 rollback 가능해야 한다.
- distributed allow error, distributed denied-without-retry-after, context cancel 모두 rollback 대상이다.
- distributed denied 후 sleep했다가 최종 allowed되는 정상 경로는 rollback하지 않는다.

### Task D. holodexprovider fallback pacing 구현

- `Service`에 `liveFallbackMu`, `liveFallbackCursor`, `liveStatusFallbackConfig`를 추가한다.
- config는 `HolodexConfig.LiveStatusFallback`에 둔다.
- `getChannelsLiveStatusFromScraper`는 `(streams, failed, deferred)`를 반환한다.
- rotated order는 cursor snapshot을 기준으로 만들고, cursor advance는 실제 attempt 수 기준으로 lock 안에서 처리한다.
- cap 초과 미시도와 budget 미시도는 `livestatus.DeferredError`로 deferred map에 넣는다.
- `scraper.ErrTransientCooldown`, `scraper.IsAdmissionDeferred`, distributed admission unavailable은 deferred로 분류한다.
- `scraper.ErrRateLimited`, `scraper.ErrForbidden`, `scraper.ErrBlockedResponse`, parser drift 등은 failed로 분류한다.
- source-level hard error는 failed subset에만 적용한다.

### Task E. live batch budget/metrics 정리

- `liveBatchYouTubeScraperFallbackUnits`와 `LiveStatusFallbackFetchPolicy.MaxAttempts=1`의 관계를 정한다.
- metrics는 최소 다음을 남긴다.
  - fallback requested channels
  - attempted channels
  - deferred channels by reason
  - failed channels by reason
  - streams found
  - fallback wall-clock elapsed
  - local gate wait duration 또는 admission wait duration
- log level은 deferred debug, failed warn이 기본이다. all-deferred를 warn으로 매 cycle 찍으면 장애 중 로그가 과해진다.

## 테스트 매트릭스

| 영역 | 테스트 | 목적 |
|---|---|---|
| scraping policy | `TestResolveFetchPolicyPropagatesAdmissionBlocking` | bool field가 숫자 override 규칙에 묻히지 않는지 확인 |
| scraping preflight | non-blocking 2nd call deferred, blocking 2nd call waits | 기존 경로 회귀와 신규 blocking 경로 검증 |
| scraping preflight | blocking ctx cancel | limiter wait가 context를 존중하는지 확인 |
| ratelimiter | waited local commit + distributed error rollback | 기존 v2가 의도한 rollback 검증 |
| ratelimiter | immediate local commit + distributed error rollback | v2에서 빠지기 쉬운 zero-wait rollback 검증 |
| htmlscraper | `FetchFromYouTubeProducer`는 non-blocking 유지 | `FetchChannel` 회귀 방지 |
| htmlscraper | `FetchFromYouTubeProducerWaitAdmission` 실 client pacing | public seam 배선 검증 |
| holodexprovider | cap N이면 한 cycle attempt <= N | fallback이 1차 polling을 장시간 점유하지 않게 함 |
| holodexprovider | cursor rotation으로 ceil(total/N) cycle 후 전 채널 attempt | starvation 방지 |
| holodexprovider | all-deferred WithFailures는 provider hard error 없음 | Holodex 장애 + YouTube gate busy 상황에서 source 전체 실패로 승격하지 않음 |
| poller | deferred failure는 markEndedSessions 미호출 + poller nil error | session 보존과 scheduler soft 처리 동시 검증 |
| poller | real failed failure는 markEndedSessions 미호출 + poller error | parser/blocked 같은 실제 장애는 운영에 보이게 함 |
| race | concurrent fallback calls | cursor lock 안전성 확인 |
| regression | official schedule fallback still upcoming-only | Task 0 결론 보호 |

## 구현 PR 분할 권장

한 PR에 scraper policy, ratelimiter rollback, holodex fallback pacing, poller semantics, config, metrics를 모두 넣으면 review blast radius가 크다. 가능한 경우 아래처럼 쪼개는 편이 안전하다.

1. PR 1: `FetchPolicy.AdmissionBlocking`, `LiveStatusFallbackFetchPolicy`, `WaitWithBucket` rollback, unit tests.
2. PR 2: `GetUpcomingEventsWaitAdmission`, htmlscraper wait-admission method, live-status deferred contract, poller deferred skip tests.
3. PR 3: holodexprovider cap/budget/cursor/config, fallback classification, integration tests.
4. PR 4: metrics/logging, budget accounting 정리.

긴급 장애 대응이라면 1~3을 한 PR로 묶을 수 있지만, 그 경우 PR description에 위 테스트 매트릭스를 그대로 체크리스트로 넣는 것이 좋다.

## v2 플랜에 대한 최종 수정 요약

- `errLiveStatusFallbackDeferred`는 holodexprovider 내부 sentinel이 아니라 poller도 판별 가능한 typed deferred contract로 바꾼다.
- `GetChannelsLiveStatus` all-deferred soft는 단독 적용 금지다. 단일 `LivePoller.Poll`이 empty streams를 “방송 없음”으로 해석하지 않도록 먼저 detailed provider path 또는 typed deferred skip을 도입한다.
- `WaitWithBucket` rollback은 zero-wait local commit까지 포함한다.
- distributed admission error 판별은 string matching이 아니라 sentinel/predicate로 한다.
- `liveBatchYouTubeScraperFallbackUnits`와 `LiveStatusFallbackFetchPolicy.MaxAttempts=1`의 불일치를 해소하거나 명시적으로 문서화한다.
- integration test는 `fetchUpcoming` injection이 아니라 실제 `scraper.Client` path를 지나야 한다.
- config는 `HolodexConfig.LiveStatusFallback`처럼 실제 constructor가 받을 수 있는 구조로 추가한다.
- official schedule batch 불가는 확정 사항으로 두고 재검토 루프에서 제외한다.
