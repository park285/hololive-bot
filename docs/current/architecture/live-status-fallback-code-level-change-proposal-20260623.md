# Live-status 폴백 페이싱 코드레벨 변경안

작성일: 2026-06-23  
대상 PR: #139 보강 문서  
기준: `Live-status 스크래퍼 폴백 페이싱 Implementation Plan v2` + PR #139 코드 리뷰 문서

## 목적

이 문서는 기존 플랜의 방향을 다시 설명하는 문서가 아니다. 실제 구현 PR을 만들 때 어떤 파일을 어떤 형태로 고치면 되는지, 함수 시그니처와 에러 계약을 어떻게 잡아야 하는지, 어떤 테스트가 먼저 깨지고 어떤 코드로 통과시킬지를 코드레벨로 고정하기 위한 변경안이다.

핵심 목표는 세 가지다.

1. Holodex `/users/live` 장애 시 YouTube per-channel fallback이 14채널을 non-blocking admission으로 버스트하지 않게 한다.
2. fallback이 limiter/cooldown/budget 때문에 시도하지 못한 채널을 “방송 없음”으로 해석하지 않게 한다.
3. deferred 상태는 session close를 막되 scheduler hard failure로 계속 떠오르지 않게 한다.

## 구현 불변식

아래 불변식은 코드 리뷰에서 반드시 지켜야 한다.

1. `sharedRL`은 분리하지 않는다. 같은 egress/IP에 limiter를 두 개 만들면 1차 폴링과 fallback이 각각 3초 gate를 가져 실제 요청률이 두 배가 된다.
2. 기존 `htmlscraper.Service.FetchFromYouTubeProducer`는 non-blocking 동작을 유지한다. 이 메서드는 `FetchChannel` 경로에서도 사용되므로 in-place blocking 변경 금지다.
3. blocking admission은 live-status fallback 전용 public method로만 노출한다.
4. `deferred`는 `failed`와 다르다. deferred는 “이번 cycle에서 판단 유예”이며 session close도, scheduler hard error도 만들면 안 된다.
5. 단일 `LivePoller.Poll`과 batch `LivePoller.PollBatch`는 동일한 deferred contract를 사용해야 한다.
6. 공식 스케줄 페이지는 live-status source가 아니다. 현재 parser가 모든 stream을 `StreamStatusUpcoming`으로 만든다는 사실을 전제로 한다.

## Patch 1. live-status deferred contract package 추가

### 새 파일

`hololive/hololive-shared/pkg/service/youtube/livestatus/deferred.go`

### 의도

`holodexprovider` 내부 sentinel만 만들면 `poller` package가 deferred와 hard failure를 구분할 수 없다. import cycle 없이 의미를 공유하려면 아주 작은 독립 package가 필요하다.

이 package는 stdlib만 import한다. `scraper`, `holodexprovider`, `poller`를 import하지 않는다.

```go
package livestatus

import (
	"errors"
	"fmt"
	"strings"
)

var ErrDeferred = errors.New("live status deferred")

type DeferredReason string

const (
	DeferredReasonUnknown                      DeferredReason = "unknown"
	DeferredReasonPerCycleCap                  DeferredReason = "per_cycle_cap"
	DeferredReasonWallClockBudget              DeferredReason = "wall_clock_budget"
	DeferredReasonContextDone                  DeferredReason = "context_done"
	DeferredReasonYouTubeCooldown              DeferredReason = "youtube_cooldown"
	DeferredReasonAdmissionDeferred            DeferredReason = "admission_deferred"
	DeferredReasonDistributedLimiterUnavailable DeferredReason = "distributed_limiter_unavailable"
)

type DeferredError struct {
	Reason    DeferredReason
	ChannelID string
	Err       error
}

func NewDeferred(reason DeferredReason, channelID string, err error) error {
	if reason == "" {
		reason = DeferredReasonUnknown
	}
	return &DeferredError{
		Reason:    reason,
		ChannelID: strings.TrimSpace(channelID),
		Err:       err,
	}
}

func (e *DeferredError) Error() string {
	if e == nil {
		return ErrDeferred.Error()
	}
	if e.ChannelID != "" && e.Err != nil {
		return fmt.Sprintf("%s: channel=%s reason=%s: %v", ErrDeferred, e.ChannelID, e.Reason, e.Err)
	}
	if e.ChannelID != "" {
		return fmt.Sprintf("%s: channel=%s reason=%s", ErrDeferred, e.ChannelID, e.Reason)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: reason=%s: %v", ErrDeferred, e.Reason, e.Err)
	}
	return fmt.Sprintf("%s: reason=%s", ErrDeferred, e.Reason)
}

func (e *DeferredError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *DeferredError) Is(target error) bool {
	return target == ErrDeferred
}

func (e *DeferredError) LiveStatusDeferred() bool {
	return true
}

type deferredMarker interface {
	LiveStatusDeferred() bool
}

func IsDeferred(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDeferred) {
		return true
	}
	var marker deferredMarker
	return errors.As(err, &marker) && marker.LiveStatusDeferred()
}

func ReasonOf(err error) DeferredReason {
	var deferred *DeferredError
	if errors.As(err, &deferred) && deferred != nil && deferred.Reason != "" {
		return deferred.Reason
	}
	if IsDeferred(err) {
		return DeferredReasonUnknown
	}
	return ""
}
```

### 테스트

`hololive/hololive-shared/pkg/service/youtube/livestatus/deferred_test.go`

```go
func TestDeferredErrorMatchesSentinel(t *testing.T) {
	err := NewDeferred(DeferredReasonPerCycleCap, "UCtest", errors.New("cap reached"))
	if !errors.Is(err, ErrDeferred) {
		t.Fatalf("errors.Is(..., ErrDeferred) = false")
	}
	if !IsDeferred(err) {
		t.Fatalf("IsDeferred = false")
	}
	if got := ReasonOf(err); got != DeferredReasonPerCycleCap {
		t.Fatalf("ReasonOf = %q", got)
	}
}
```

## Patch 2. distributed limiter unavailable sentinel 추가

### 수정 파일

`hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/ratelimiter/ratelimiter.go`  
또는 새 파일: `.../ratelimiter/errors.go`

### 의도

v2 플랜의 “Valkey admission error는 deferred” 규칙을 string matching 없이 구현한다. Holodex provider가 내부 ratelimiter package를 직접 import할 수 없으므로 `scraping`과 public `scraper`에서 predicate를 re-export한다.

### internal ratelimiter package

```go
package ratelimiter

import "errors"

var ErrDistributedLimiterUnavailable = errors.New("distributed rate limiter unavailable")

func IsDistributedLimiterUnavailable(err error) bool {
	return errors.Is(err, ErrDistributedLimiterUnavailable)
}
```

`nextDistributedWait`와 `tryReserveDistributedAdmission`의 distributed `Allow` error wrapping을 바꾼다.

```go
func (r *RateLimiter) nextDistributedWait(ctx context.Context, bucket string) (time.Duration, bool, error) {
	decision, err := r.distributed.Allow(ctx, bucket, r.distributedLimit, r.distributedWindow)
	if err != nil {
		return 0, false, fmt.Errorf("%w: distributed rate limiter allow failed: %w", ErrDistributedLimiterUnavailable, err)
	}
	// 나머지는 기존 유지
}

func (r *RateLimiter) tryReserveDistributedAdmission(ctx context.Context, bucket string) (AdmissionDecision, error) {
	if r.distributed == nil {
		return AdmissionDecision{Allowed: true}, nil
	}
	decision, err := r.distributed.Allow(ctx, bucket, r.distributedLimit, r.distributedWindow)
	if err != nil {
		return AdmissionDecision{}, fmt.Errorf("%w: distributed rate limiter allow failed: %w", ErrDistributedLimiterUnavailable, err)
	}
	// 나머지는 기존 유지
}
```

### scraping package re-export

새 파일 권장:

`hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/ratelimiter_errors.go`

```go
package scraping

import "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/ratelimiter"

func IsDistributedRateLimiterUnavailable(err error) bool {
	return ratelimiter.IsDistributedLimiterUnavailable(err)
}
```

### public scraper package re-export

`hololive/hololive-shared/pkg/service/youtube/scraper/scraper.go`

```go
var IsDistributedRateLimiterUnavailable = scraping.IsDistributedRateLimiterUnavailable
```

## Patch 3. `WaitWithBucket` local reservation rollback 보정

### 수정 파일

`hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/ratelimiter/ratelimiter.go`

### 문제

현재 `WaitWithBucket`은 local gate를 먼저 소비하고 distributed wait에서 에러가 나면 local slot을 되돌리지 않는다. `TryReserveWithBucket`은 이미 distributed admission 실패 시 rollback한다. blocking path가 fallback에 들어가면 이 비대칭 때문에 Valkey 장애 시 local limiter가 헛소비된다.

### 변경 형태

핵심은 “실제로 timer wait가 있었는가”가 아니라 “`lastTime`을 commit했는가”를 반환해야 한다는 점이다. 첫 요청처럼 wait가 0이어도 `lastTime`은 commit되므로 rollback 대상이다.

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

func (r *RateLimiter) waitLocal(ctx context.Context) (localWaitReservation, bool, error) {
	if err := ctx.Err(); err != nil {
		return localWaitReservation{}, false, fmt.Errorf("rate limiter wait canceled: %w", err)
	}

	waitTime, reservation, committed, err := r.reserveLocalWait(ctx)
	if err != nil || !committed {
		return localWaitReservation{}, false, err
	}
	if waitTime <= 0 {
		return reservation, true, nil
	}
	if err := r.waitForLocalReservation(ctx, waitTime, reservation); err != nil {
		// waitForLocalReservation가 cancel 시 자체 rollback을 수행하므로 호출자에게는 committed=false로 알린다.
		return localWaitReservation{}, false, err
	}
	return reservation, true, nil
}

func (r *RateLimiter) reserveLocalWait(ctx context.Context) (time.Duration, localWaitReservation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, localWaitReservation{}, false, fmt.Errorf("rate limiter wait canceled: %w", err)
	}

	if r.interval <= 0 {
		return 0, localWaitReservation{}, false, nil
	}

	now := time.Now()
	if r.lastTime.IsZero() {
		reservation := r.commitLocalReservationLocked(now)
		return 0, reservation, true, nil
	}
	nextAllowed := r.lastTime.Add(r.interval)
	if !now.Before(nextAllowed) {
		reservation := r.commitLocalReservationLocked(now)
		return 0, reservation, true, nil
	}
	reservation := r.commitLocalReservationLocked(nextAllowed)
	return nextAllowed.Sub(now), reservation, true, nil
}

func (r *RateLimiter) commitLocalReservationLocked(next time.Time) localWaitReservation {
	previous := r.lastTime
	r.commitLocalWait(next)
	return localWaitReservation{prevLastTime: previous, seq: r.seq}
}
```

`tryReserveLocalAdmission`의 `reserveLocalAdmissionLocked`도 중복을 줄이고 싶다면 내부에서 `commitLocalReservationLocked`를 재사용할 수 있다. 단, 이 변경은 optional이다.

### 테스트

`hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/ratelimiter/ratelimiter_rollback_test.go`

```go
type distributedLimiterFunc func(context.Context, string, int, time.Duration) (ratelimit.Decision, error)

func (f distributedLimiterFunc) Allow(ctx context.Context, bucket string, limit int, window time.Duration) (ratelimit.Decision, error) {
	return f(ctx, bucket, limit, window)
}

func TestWaitWithBucketRollsBackImmediateLocalReservationOnDistributedError(t *testing.T) {
	r := New(time.Hour)
	require.NoError(t, r.ConfigureDistributed(distributedLimiterFunc(func(context.Context, string, int, time.Duration) (ratelimit.Decision, error) {
		return ratelimit.Decision{}, errors.New("valkey down")
	}), 1, time.Second))

	err := r.WaitWithBucket(context.Background(), "bucket")
	require.Error(t, err)

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lastTime.IsZero() {
		t.Fatalf("lastTime was not rolled back: %v", r.lastTime)
	}
}

func TestWaitWithBucketRollsBackWaitedLocalReservationOnDistributedError(t *testing.T) {
	r := New(30 * time.Millisecond)

	// 첫 호출은 distributed 없이 local slot만 만든다.
	require.NoError(t, r.WaitWithBucket(context.Background(), "bucket"))

	r.mu.Lock()
	before := r.lastTime
	r.mu.Unlock()

	require.NoError(t, r.ConfigureDistributed(distributedLimiterFunc(func(context.Context, string, int, time.Duration) (ratelimit.Decision, error) {
		return ratelimit.Decision{}, errors.New("valkey down")
	}), 1, time.Second))

	err := r.WaitWithBucket(context.Background(), "bucket")
	require.Error(t, err)

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.lastTime.Equal(before) {
		t.Fatalf("lastTime advanced after distributed error: before=%v after=%v", before, r.lastTime)
	}
}
```

## Patch 4. `FetchPolicy.AdmissionBlocking`과 fallback preset 추가

### 수정 파일

`hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/client.go`  
`hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/client_http.go`  
`hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/client_resolve.go`  
`hololive/hololive-shared/pkg/service/youtube/scraper/scraper.go`

### `FetchPolicy` 변경

```go
type FetchPolicy struct {
	MaxAttempts       int
	PerAttemptTimeout time.Duration
	BaseDelay         time.Duration
	Jitter            time.Duration
	MaxDelay          time.Duration
	AdmissionBlocking bool
}
```

### preset 추가

`MaxAttempts: 1`이 중요하다. `retry.WithRetry`는 attempt마다 `fetchPagePreflight`를 다시 호출한다. blocking admission + retry를 같이 두면 attempt마다 다시 limiter wait를 점유하므로 live-status fallback에는 retry를 넣지 않는다.

```go
LiveStatusFallbackFetchPolicy = FetchPolicy{
	MaxAttempts:       1,
	PerAttemptTimeout: defaultFetchPerAttemptTimeout(15 * time.Second),
	AdmissionBlocking: true,
}
```

### `resolveFetchPolicy` 변경

```go
func resolveFetchPolicy(policy ...FetchPolicy) FetchPolicy {
	resolved := DefaultFetchPolicy
	if len(policy) == 0 {
		return resolved
	}

	override := policy[0]
	if override.MaxAttempts > 0 {
		resolved.MaxAttempts = override.MaxAttempts
	}
	if override.PerAttemptTimeout > 0 {
		resolved.PerAttemptTimeout = override.PerAttemptTimeout
	}
	if override.BaseDelay > 0 {
		resolved.BaseDelay = override.BaseDelay
	}
	if override.Jitter > 0 {
		resolved.Jitter = override.Jitter
	}
	if override.MaxDelay > 0 {
		resolved.MaxDelay = override.MaxDelay
	}
	// bool은 zero-value와 explicit false를 구분하지 않는다.
	// FetchPolicy override가 제공되면 admission mode는 override 값을 그대로 따른다.
	resolved.AdmissionBlocking = override.AdmissionBlocking
	return resolved
}
```

### `fetchPagePreflight` 변경

bool parameter보다 resolved policy를 넘기는 것이 다음 필드 추가에 안전하다.

```go
func (c *Client) fetchPagePreflight(ctx context.Context, pageURL string, policy FetchPolicy) error {
	if cooldownRemaining := c.backoffState.HardCooldownRemaining(); cooldownRemaining > 0 {
		return fmt.Errorf("in cooldown for %v: %w", cooldownRemaining.Round(time.Second), ErrRateLimited)
	}

	bucket := distributedBucketFromURL(pageURL)
	if policy.AdmissionBlocking {
		if err := c.rateLimiter.WaitWithBucket(ctx, bucket); err != nil {
			return fmt.Errorf("rate limiter wait admission failed: %w", err)
		}
		return nil
	}

	decision, err := c.rateLimiter.TryReserveWithBucket(ctx, bucket)
	if err != nil {
		return fmt.Errorf("rate limiter admission failed: %w", err)
	}
	if !decision.Allowed {
		return newRateLimitAdmissionDeferredError(bucket, decision)
	}
	return nil
}
```

### `fetchPage` 호출부 변경

```go
err := retry.WithRetry(ctx, c.fetchPageRetryOptions(pageURL, resolvedPolicy), func(ctx context.Context) error {
	if err := c.fetchPagePreflight(ctx, pageURL, resolvedPolicy); err != nil {
		return err
	}
	// 이후 기존 유지
})
```

### public re-export

`hololive/hololive-shared/pkg/service/youtube/scraper/scraper.go`

```go
var LiveStatusFallbackFetchPolicy = scraping.LiveStatusFallbackFetchPolicy
```

## Patch 5. YouTube producer에 fallback 전용 blocking method 추가

### 수정 파일

`hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/videos.go`

### 변경 형태

기존 `GetUpcomingEvents`는 그대로 두고 내부 helper만 분리한다.

```go
func (c *Client) GetUpcomingEvents(ctx context.Context, channelID string) ([]*UpcomingEvent, error) {
	return c.getUpcomingEvents(ctx, channelID)
}

func (c *Client) GetUpcomingEventsWaitAdmission(ctx context.Context, channelID string) ([]*UpcomingEvent, error) {
	return c.getUpcomingEvents(ctx, channelID, LiveStatusFallbackFetchPolicy)
}

func (c *Client) getUpcomingEvents(ctx context.Context, channelID string, policy ...FetchPolicy) ([]*UpcomingEvent, error) {
	url := fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)

	html, err := c.fetchChannelSourcePage(ctx, "upcoming_events", channelID, url, FailureSourceHTML, policy...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
	}

	jsonStr, err := extractYtInitialData(html)
	if err != nil {
		logStructureWarning("upcoming_events", channelID, "ytInitialData extraction failed", "error", err)
		return nil, c.recordParserDrift(ctx, "upcoming_events", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
	}

	data := gjson.Parse(jsonStr)
	events, err := parseUpcomingEventsFromInitialData(&data)
	if err != nil {
		logStructureWarning("upcoming_events", channelID, "failed to parse initial data", "error", err)
		return nil, c.recordParserDrift(ctx, "upcoming_events", "parse_initial_data", channelID, url, FailureSourceHTML, html, err)
	}
	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
	return events, nil
}
```

이 코드는 기존 parser, snapshot, channel health, parser drift 기록을 모두 재사용한다. 복제된 parser path를 만들지 않는다.

## Patch 6. htmlscraper에 wait-admission 전용 method 추가

### 수정 파일

`hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/htmlscraper/youtube_fallback.go`

### 변경 형태

```go
func (s *Service) FetchFromYouTubeProducer(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	return s.fetchFromYouTubeProducer(ctx, channelID, false)
}

func (s *Service) FetchFromYouTubeProducerWaitAdmission(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	return s.fetchFromYouTubeProducer(ctx, channelID, true)
}

func (s *Service) fetchFromYouTubeProducer(ctx context.Context, channelID string, waitAdmission bool) ([]*domain.Stream, error) {
	var (
		events []*scraper.UpcomingEvent
		err    error
	)

	switch {
	case s.fetchUpcoming != nil:
		// 테스트 주입은 기존 동작을 유지한다.
		events, err = s.fetchUpcoming(ctx, channelID)
	case s.youtubeProducer != nil && waitAdmission:
		events, err = s.youtubeProducer.GetUpcomingEventsWaitAdmission(ctx, channelID)
	case s.youtubeProducer != nil:
		events, err = s.youtubeProducer.GetUpcomingEvents(ctx, channelID)
	default:
		return nil, fmt.Errorf("youtube producer not configured")
	}
	if err != nil {
		return nil, fmt.Errorf("youtube producer error: %w", err)
	}
	return s.convertEventsToStreams(events, channelID), nil
}

func (s *Service) convertEventsToStreams(events []*scraper.UpcomingEvent, channelID string) []*domain.Stream {
	streams := make([]*domain.Stream, 0, len(events))
	for _, event := range events {
		streams = append(streams, s.convertEventToStream(event, channelID))
	}
	return streams
}
```

주의할 점은 `FetchChannel` 경로가 계속 `FetchFromYouTubeProducer`를 호출한다는 것이다. live-status fallback만 `FetchFromYouTubeProducerWaitAdmission`으로 바꾼다.

## Patch 7. Holodex live-status fallback config 추가

### 수정 파일

`hololive/hololive-shared/pkg/config/internal/settings/config_api_operational.go`  
`hololive/hololive-shared/pkg/config/internal/settings/config_youtube.go`  
`hololive/hololive-shared/pkg/config/internal/settings/config.go`  
`hololive/hololive-shared/pkg/config/internal/settings/config_validation.go`

### config type

```go
type HolodexLiveStatusFallbackConfig struct {
	MaxPerCycle     int
	WallClockBudget time.Duration
	DeadlineMargin  time.Duration
}
```

`HolodexConfig`에 필드 추가:

```go
type HolodexConfig struct {
	BaseURL              string
	APIKey               string
	Timeout              time.Duration
	PerAttemptTimeout    time.Duration
	MaxRetryAttempts     int
	Transport            HolodexTransportConfig
	Concurrency          HolodexConcurrencyConfig
	DistributedRateLimit DistributedRateLimitConfig
	LiveStatusFallback   HolodexLiveStatusFallbackConfig
}
```

### default

```go
func DefaultHolodexOperationalConfig() HolodexConfig {
	return HolodexConfig{
		// 기존 필드 유지
		LiveStatusFallback: HolodexLiveStatusFallbackConfig{
			MaxPerCycle:     4,
			WallClockBudget: 12 * time.Second,
			DeadlineMargin:  500 * time.Millisecond,
		},
	}
}
```

### env loader

`loadHolodexConfig`에 추가:

```go
LiveStatusFallback: HolodexLiveStatusFallbackConfig{
	MaxPerCycle:     sharedenv.Int("HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE", d.LiveStatusFallback.MaxPerCycle),
	WallClockBudget: time.Duration(sharedenv.Int("HOLODEX_LIVE_STATUS_FALLBACK_WALL_CLOCK_BUDGET_SECONDS", int(d.LiveStatusFallback.WallClockBudget/time.Second))) * time.Second,
	DeadlineMargin:  time.Duration(sharedenv.Int("HOLODEX_LIVE_STATUS_FALLBACK_DEADLINE_MARGIN_MS", int(d.LiveStatusFallback.DeadlineMargin/time.Millisecond))) * time.Millisecond,
},
```

### validation

`validateWithRequired`에 Holodex validation을 추가한다.

```go
if err := validateHolodexConfig(&c.Holodex); err != nil {
	return err
}
```

새 validation 함수:

```go
func validateHolodexConfig(config *HolodexConfig) error {
	if config == nil {
		return nil
	}
	fallback := config.LiveStatusFallback
	if fallback.MaxPerCycle <= 0 {
		return fmt.Errorf("HOLODEX_LIVE_STATUS_FALLBACK_MAX_PER_CYCLE must be positive")
	}
	if fallback.WallClockBudget <= 0 {
		return fmt.Errorf("HOLODEX_LIVE_STATUS_FALLBACK_WALL_CLOCK_BUDGET_SECONDS must be positive")
	}
	if fallback.DeadlineMargin < 0 {
		return fmt.Errorf("HOLODEX_LIVE_STATUS_FALLBACK_DEADLINE_MARGIN_MS must be >= 0")
	}
	return nil
}
```

## Patch 8. holodexprovider `Service`에 fallback state/config 추가

### 수정 파일

`hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/service.go`

### struct 변경

```go
type Service struct {
	requester    apiclient.Requester
	scraper      *htmlscraper.Service
	logger       *slog.Logger
	cacheManager *CacheManager
	mapper       *streammapping.StreamMapper
	filter       *streammapping.StreamFilter
	retry        *retryScheduler
	concurrency  config.HolodexConcurrencyConfig

	liveFallbackMu     sync.Mutex
	liveFallbackCursor int
	liveStatusFallback config.HolodexLiveStatusFallbackConfig
}
```

`service.go` import에 `sync`가 추가된다.

### constructor 변경

`NewHolodexServiceWithConfig`에서 설정한다.

```go
service := &Service{
	requester:          requester,
	scraper:            scraperService,
	logger:             logger,
	cacheManager:       NewCacheManager(cacheClient, logger),
	mapper:             streammapping.NewStreamMapper(logger),
	filter:             streammapping.NewStreamFilter(logger),
	concurrency:        holodexCfg.Concurrency,
	liveStatusFallback: holodexCfg.LiveStatusFallback,
}
```

테스트에서 zero config Service를 직접 만들 수 있다면 fallback 사용 시 default를 보정하는 helper도 둔다.

```go
func (h *Service) effectiveLiveStatusFallbackConfig() config.HolodexLiveStatusFallbackConfig {
	d := config.DefaultHolodexOperationalConfig().LiveStatusFallback
	if h == nil {
		return d
	}
	cfg := h.liveStatusFallback
	if cfg.MaxPerCycle <= 0 {
		cfg.MaxPerCycle = d.MaxPerCycle
	}
	if cfg.WallClockBudget <= 0 {
		cfg.WallClockBudget = d.WallClockBudget
	}
	if cfg.DeadlineMargin < 0 {
		cfg.DeadlineMargin = d.DeadlineMargin
	}
	return cfg
}
```

## Patch 9. holodexprovider fallback loop를 cap/budget/rotation/3분류로 교체

### 수정 파일

`hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/service_channels_live.go`

### import 추가

```go
import (
	// 기존 유지
	"github.com/kapu/hololive-shared/pkg/service/youtube/livestatus"
)
```

### `GetChannelsLiveStatus` soft 처리

기존 코드는 `len(streams)==0 && len(failed)>0`이면 에러를 반환한다. 이 조건은 deferred-only 결과에서 세션/스케줄러 semantics를 깨뜨릴 수 있다. hard failure가 하나라도 있을 때만 error로 올린다.

```go
func (h *Service) GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error) {
	streams, unresolved, err := h.GetChannelsLiveStatusWithFailures(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	if len(streams) == 0 && hasHardChannelsLiveStatusFailures(unresolved) {
		return nil, fmt.Errorf("get channels live status: %w", joinChannelsLiveStatusFailures(channelIDs, unresolved))
	}
	return streams, nil
}

func hasHardChannelsLiveStatusFailures(failures map[string]error) bool {
	for _, err := range failures {
		if err == nil {
			continue
		}
		if !livestatus.IsDeferred(err) {
			return true
		}
	}
	return false
}
```

### fallback result type

```go
type channelsLiveStatusFallbackResult struct {
	streams  []*domain.Stream
	failed   map[string]error
	deferred map[string]error
}
```

### fallback caller 변경

```go
func (h *Service) tryChannelsLiveStatusFallback(ctx context.Context, channelIDs []string, err error) ([]*domain.Stream, map[string]error, error, bool) {
	if !h.shouldUseFallback(ctx, err) || h.scraper == nil {
		return nil, nil, nil, false
	}

	h.logger.Warn("Using scraper fallback for channels live status", slog.Any("error", err))
	result := h.getChannelsLiveStatusFromScraper(ctx, channelIDs)
	h.logChannelsLiveStatusFallbackFailures(channelIDs, result.failed, result.deferred)
	return h.resolveChannelsLiveStatusFallback(ctx, channelIDs, result.streams, result.failed, result.deferred)
}
```

로그 함수는 failed와 deferred를 분리한다.

```go
func (h *Service) logChannelsLiveStatusFallbackFailures(channelIDs []string, failed, deferred map[string]error) {
	if len(failed) > 0 {
		h.logger.Warn("Scraper live status fallback failed for some channels",
			slog.Int("channel_count", len(channelIDs)),
			slog.Int("failed_count", len(failed)))
	}
	if len(deferred) > 0 {
		h.logger.Debug("Scraper live status fallback deferred for some channels",
			slog.Int("channel_count", len(channelIDs)),
			slog.Int("deferred_count", len(deferred)))
	}
}
```

### fallback resolution 변경

중요한 점은 source-level hard error 검사를 deferred가 아닌 failed subset에만 적용하는 것이다.

```go
func (h *Service) resolveChannelsLiveStatusFallback(
	ctx context.Context,
	channelIDs []string,
	allStreams []*domain.Stream,
	failed map[string]error,
	deferred map[string]error,
) ([]*domain.Stream, map[string]error, error, bool) {
	if sourceLevelErr := firstChannelsLiveStatusSourceLevelError(channelIDs, failed); sourceLevelErr != nil {
		return nil, nil, fmt.Errorf("fetch channels live status from scraper: %w", sourceLevelErr), false
	}
	if len(failed) > 0 && len(failed) == len(channelIDs) {
		return nil, nil, fmt.Errorf("fetch channels live status from scraper: %w", joinChannelsLiveStatusFailures(channelIDs, failed)), false
	}

	unresolved := mergeChannelsLiveStatusFailures(failed, deferred)
	if len(unresolved) > 0 {
		return allStreams, unresolved, nil, true
	}
	if len(allStreams) == 0 {
		return nil, nil, nil, false
	}

	h.cacheManager.SetChannelsLiveStatusStreams(ctx, channelIDs, allStreams, 30*time.Second)
	return allStreams, nil, nil, true
}

func mergeChannelsLiveStatusFailures(failed, deferred map[string]error) map[string]error {
	if len(failed) == 0 && len(deferred) == 0 {
		return nil
	}
	merged := make(map[string]error, len(failed)+len(deferred))
	for channelID, err := range deferred {
		merged[channelID] = err
	}
	for channelID, err := range failed {
		merged[channelID] = err
	}
	return merged
}
```

### cursor reservation

동시 fallback에서도 data race와 중복 window를 줄이기 위해 cursor read와 advance를 한 lock 안에서 수행한다. 여기서 selected channel은 “이번 cycle의 fallback attempt budget을 배정받은 채널”로 본다. selected 후 wall-clock budget 때문에 실제 HTTP가 시작되지 못하면 deferred로 기록한다.

```go
func (h *Service) reserveLiveStatusFallbackChannels(channelIDs []string, maxPerCycle int) ([]string, map[string]struct{}) {
	if len(channelIDs) == 0 || maxPerCycle <= 0 {
		return nil, nil
	}
	h.liveFallbackMu.Lock()
	defer h.liveFallbackMu.Unlock()

	start := h.liveFallbackCursor % len(channelIDs)
	limit := min(maxPerCycle, len(channelIDs))
	selected := make([]string, 0, limit)
	selectedSet := make(map[string]struct{}, limit)
	for offset := 0; offset < limit; offset++ {
		channelID := channelIDs[(start+offset)%len(channelIDs)]
		selected = append(selected, channelID)
		selectedSet[channelID] = struct{}{}
	}
	h.liveFallbackCursor = (start + len(selected)) % len(channelIDs)
	return selected, selectedSet
}
```

### budget context

```go
func (h *Service) liveStatusFallbackContext(ctx context.Context, cfg config.HolodexLiveStatusFallbackConfig) (context.Context, context.CancelFunc, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	now := time.Now()
	deadline := now.Add(cfg.WallClockBudget)
	if parentDeadline, ok := ctx.Deadline(); ok {
		parentBudgetDeadline := parentDeadline.Add(-cfg.DeadlineMargin)
		if parentBudgetDeadline.Before(deadline) {
			deadline = parentBudgetDeadline
		}
	}
	if !deadline.After(now) {
		return nil, nil, context.DeadlineExceeded
	}
	fallbackCtx, cancel := context.WithDeadline(ctx, deadline)
	return fallbackCtx, cancel, nil
}
```

### fallback loop 본체

```go
func (h *Service) getChannelsLiveStatusFromScraper(ctx context.Context, channelIDs []string) channelsLiveStatusFallbackResult {
	result := channelsLiveStatusFallbackResult{
		streams: make([]*domain.Stream, 0, len(channelIDs)),
	}
	if len(channelIDs) == 0 {
		return result
	}

	cfg := h.effectiveLiveStatusFallbackConfig()
	fallbackCtx, cancel, err := h.liveStatusFallbackContext(ctx, cfg)
	if err != nil {
		result.deferred = deferAllChannels(channelIDs, livestatus.DeferredReasonContextDone, err)
		return result
	}
	defer cancel()

	selected, selectedSet := h.reserveLiveStatusFallbackChannels(channelIDs, cfg.MaxPerCycle)
	result.deferred = deferUnselectedChannels(channelIDs, selectedSet)

	for _, channelID := range selected {
		if err := fallbackCtx.Err(); err != nil {
			putChannelError(&result.deferred, channelID, livestatus.NewDeferred(livestatus.DeferredReasonWallClockBudget, channelID, err))
			continue
		}

		streams, err := h.scraper.FetchFromYouTubeProducerWaitAdmission(fallbackCtx, channelID)
		if err != nil {
			reason, deferred := classifyLiveStatusFallbackDeferredError(err)
			if deferred {
				putChannelError(&result.deferred, channelID, livestatus.NewDeferred(reason, channelID, err))
				continue
			}
			putChannelError(&result.failed, channelID, err)
			continue
		}
		result.streams = append(result.streams, streams...)
	}

	return result
}

func putChannelError(target *map[string]error, channelID string, err error) {
	if err == nil {
		return
	}
	if *target == nil {
		*target = make(map[string]error, 1)
	}
	(*target)[channelID] = err
}

func deferAllChannels(channelIDs []string, reason livestatus.DeferredReason, cause error) map[string]error {
	deferred := make(map[string]error, len(channelIDs))
	for _, channelID := range channelIDs {
		deferred[channelID] = livestatus.NewDeferred(reason, channelID, cause)
	}
	return deferred
}

func deferUnselectedChannels(channelIDs []string, selected map[string]struct{}) map[string]error {
	if len(channelIDs) == 0 {
		return nil
	}
	deferred := make(map[string]error)
	for _, channelID := range channelIDs {
		if _, ok := selected[channelID]; ok {
			continue
		}
		deferred[channelID] = livestatus.NewDeferred(livestatus.DeferredReasonPerCycleCap, channelID, nil)
	}
	if len(deferred) == 0 {
		return nil
	}
	return deferred
}
```

### deferred classification

```go
func classifyLiveStatusFallbackDeferredError(err error) (livestatus.DeferredReason, bool) {
	switch {
	case err == nil:
		return "", false
	case stdErrors.Is(err, context.Canceled), stdErrors.Is(err, context.DeadlineExceeded):
		return livestatus.DeferredReasonContextDone, true
	case stdErrors.Is(err, scraper.ErrTransientCooldown):
		return livestatus.DeferredReasonYouTubeCooldown, true
	case scraper.IsAdmissionDeferred(err):
		return livestatus.DeferredReasonAdmissionDeferred, true
	case scraper.IsDistributedRateLimiterUnavailable(err):
		return livestatus.DeferredReasonDistributedLimiterUnavailable, true
	default:
		return "", false
	}
}
```

`ErrRateLimited`, `ErrForbidden`, `ErrBlockedResponse`는 여기서 deferred로 만들지 않는다. 이들은 source-level hard error 후보로 남긴다.

## Patch 10. `LivePoller`가 deferred를 hard error로 반환하지 않게 수정

### 수정 파일

`hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers/live_poller.go`

### import 추가

```go
import "github.com/kapu/hololive-shared/pkg/service/youtube/livestatus"
```

### `Poll`을 detailed provider 우선 경로로 변경

현재 단일 `Poll`은 provider가 detailed interface를 구현해도 `GetChannelsLiveStatus`만 호출한다. 이 상태에서 `GetChannelsLiveStatus`가 all-deferred를 empty streams + nil error로 숨기면 `pollLiveStreams`가 empty stream을 정상 결과로 보고 `markEndedSessions`를 호출할 수 있다.

따라서 단일 `Poll`도 detailed provider를 우선 사용한다.

```go
func (p *LivePoller) Poll(ctx context.Context, channelID string) error {
	streams, failures, err := p.fetchLiveStreams(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get live streams: %w", err)
	}
	return p.pollBatchChannel(ctx, channelID, streams, failures, time.Now())
}

func (p *LivePoller) fetchLiveStreams(ctx context.Context, channelID string) ([]*domain.Stream, map[string]error, error) {
	if p.liveStatusProvider != nil {
		if detailed, ok := p.liveStatusProvider.(LiveStatusWithFailuresProvider); ok {
			return detailed.GetChannelsLiveStatusWithFailures(ctx, []string{channelID})
		}
		streams, err := p.liveStatusProvider.GetChannelsLiveStatus(ctx, []string{channelID})
		return streams, nil, err
	}
	if p.client == nil {
		return nil, nil, errors.New("live poller has no status provider or scraper client")
	}

	events, err := p.client.GetUpcomingEvents(ctx, channelID)
	if err != nil {
		return nil, nil, err
	}
	return streamsFromUpcomingEvents(channelID, events), nil, nil
}
```

### `pollBatchChannel` 변경

```go
func (p *LivePoller) pollBatchChannel(
	ctx context.Context,
	channelID string,
	streams []*domain.Stream,
	failures map[string]error,
	now time.Time,
) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if fetchErr, ok := failures[channelID]; ok {
		if livestatus.IsDeferred(fetchErr) {
			// 판단 유예: markEndedSessions를 호출하지 않고 scheduler hard error로도 올리지 않는다.
			return nil
		}
		return fmt.Errorf("failed to get live streams: %w", fetchErr)
	}
	return p.pollLiveStreams(ctx, channelID, streams, now)
}
```

이 변경 이후 `liveBatchPoller.Poll`의 `joinLiveBatchErrors`는 deferred channel을 error로 보지 않는다. hard failure만 join된다.

## Patch 11. live batch fallback budget units 보정

### 수정 파일

`hololive/hololive-youtube-producer/internal/runtime/polling/live_batch_poller.go`

### 변경안

현재 `liveBatchYouTubeScraperFallbackUnits`는 `channelCount * scraper.FetchPageMaxAttempts`를 쓴다. live-status fallback policy가 `MaxAttempts=1`이면 budget accounting이 실제보다 3배 과대평가된다.

최소 변경은 다음이다.

```go
func liveBatchYouTubeScraperFallbackUnits(channelCount int) float64 {
	if channelCount < 1 {
		channelCount = 1
	}
	attempts := scraper.LiveStatusFallbackFetchPolicy.MaxAttempts
	if attempts <= 0 {
		attempts = scraper.FetchPageMaxAttempts
	}
	return float64(channelCount * attempts)
}
```

더 정확한 계산은 `min(channelCount, cfg.LiveStatusFallback.MaxPerCycle) * attempts`지만, 현재 registration builder가 Holodex fallback config를 받지 않는다. 따라서 이 문서 기준 구현에서는 policy attempts만 먼저 반영하고, cap-aware budget은 후속 PR로 분리한다.

## Patch 12. 테스트 변경안

### 12.1 scraping preflight tests

파일: `hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/client_http_admission_test.go`

```go
func TestFetchPagePreflightBlockingWaitsInsteadOfDeferring(t *testing.T) {
	c := NewClient(WithRateLimiter(NewRateLimiter(50 * time.Millisecond)))
	const pageURL = "https://www.youtube.com/channel/UCtest"

	if err := c.fetchPagePreflight(context.Background(), pageURL, DefaultFetchPolicy); err != nil {
		t.Fatal(err)
	}
	if err := c.fetchPagePreflight(context.Background(), pageURL, DefaultFetchPolicy); !IsAdmissionDeferred(err) {
		t.Fatalf("non-blocking second call: want admission deferred, got %v", err)
	}

	policy := LiveStatusFallbackFetchPolicy
	start := time.Now()
	if err := c.fetchPagePreflight(context.Background(), pageURL, policy); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("blocking preflight returned too fast: %s", elapsed)
	}
}

func TestResolveFetchPolicyPropagatesAdmissionBlocking(t *testing.T) {
	resolved := resolveFetchPolicy(LiveStatusFallbackFetchPolicy)
	if !resolved.AdmissionBlocking {
		t.Fatal("AdmissionBlocking not propagated")
	}
	if resolved.MaxAttempts != 1 {
		t.Fatalf("MaxAttempts = %d, want 1", resolved.MaxAttempts)
	}
}
```

### 12.2 htmlscraper method split tests

파일: `hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/htmlscraper/youtube_fallback_test.go`

```go
func TestFetchFromYouTubeProducerKeepsInjectedFetcherBehavior(t *testing.T) {
	called := 0
	svc := NewTestServiceWithHTTPClient(nil, slog.Default(), "", func(ctx context.Context, channelID string) ([]*scraper.UpcomingEvent, error) {
		called++
		return []*scraper.UpcomingEvent{{VideoID: "video", Title: "title", Status: "LIVE"}}, nil
	})

	streams, err := svc.FetchFromYouTubeProducer(context.Background(), "UCtest")
	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Equal(t, domain.StreamStatusLive, streams[0].Status)
	require.Equal(t, 1, called)
}

func TestFetchFromYouTubeProducerWaitAdmissionUsesInjectedFetcherInTests(t *testing.T) {
	called := 0
	svc := NewTestServiceWithHTTPClient(nil, slog.Default(), "", func(ctx context.Context, channelID string) ([]*scraper.UpcomingEvent, error) {
		called++
		return nil, nil
	})

	_, err := svc.FetchFromYouTubeProducerWaitAdmission(context.Background(), "UCtest")
	require.NoError(t, err)
	require.Equal(t, 1, called)
}
```

blocking 자체는 이 injection test로 검증하지 않는다. 실제 blocking 검증은 fake `BrowserSnapshotFetcher`를 붙인 `scraper.Client` integration test에서 한다.

### 12.3 holodexprovider fallback classification tests

파일: `hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/service_channels_live_fallback_test.go`

테스트 helper는 실제 존재하는 constructor를 기준으로 만든다. 존재하지 않는 `NewWithFetchUpcoming` 같은 helper를 새로 가정하지 않는다.

필수 케이스:

```go
func TestChannelsLiveStatusFallbackDefersUnattemptedChannelsByCap(t *testing.T) {
	// 5채널, MaxPerCycle=2
	// fetchUpcoming 호출 수는 2 이하
	// unresolved map에는 나머지 3채널이 livestatus.ErrDeferred로 들어간다.
}

func TestChannelsLiveStatusFallbackRotatesCursor(t *testing.T) {
	// 5채널, MaxPerCycle=2, 3 cycle 호출
	// 각 channelID가 최소 1회 selected/attempted 되었는지 확인한다.
}

func TestChannelsLiveStatusFallbackAllDeferredIsSoftForWithFailures(t *testing.T) {
	// fetcher가 scraper.ErrTransientCooldown을 반환
	// GetChannelsLiveStatusWithFailures: err == nil, streams == empty, failures len == channels
	// 모든 failures 값은 livestatus.IsDeferred == true
}

func TestGetChannelsLiveStatusDeferredOnlyDoesNotReturnHardError(t *testing.T) {
	// non-WithFailures entrypoint도 all-deferred에서 error를 반환하지 않는다.
}

func TestChannelsLiveStatusFallbackHardSourceLevelErrorStillHard(t *testing.T) {
	// fetcher가 scraper.ErrForbidden wrapping error를 반환
	// source-level hard error로 fallback 실패가 유지되는지 확인한다.
}
```

### 12.4 poller deferred semantics tests

파일: `hololive/hololive-shared/pkg/service/youtube/poller/internal/pollers/live_poller_batch_test.go` 또는 `live_poller_test.go`

핵심은 deferred channel에서 `markEndedSessions` 경로가 호출되지 않는 것이다. 이 경로는 DB query/update를 수행하므로 fake DB의 query/exec 호출 수로 확인할 수 있다.

```go
type deferredLiveStatusProvider struct{}

func (deferredLiveStatusProvider) GetChannelsLiveStatus(ctx context.Context, ids []string) ([]*domain.Stream, error) {
	return nil, nil
}

func (deferredLiveStatusProvider) GetChannelsLiveStatusWithFailures(ctx context.Context, ids []string) ([]*domain.Stream, map[string]error, error) {
	failures := make(map[string]error, len(ids))
	for _, id := range ids {
		failures[id] = livestatus.NewDeferred(livestatus.DeferredReasonPerCycleCap, id, nil)
	}
	return nil, failures, nil
}

func TestLivePollerDeferredFailureSkipsSessionCloseAndReturnsNil(t *testing.T) {
	db := newRecordingPollerDB()
	poller := NewLivePollerWithStatusProvider(deferredLiveStatusProvider{}, nil, db)

	err := poller.Poll(context.Background(), "UCtest")
	require.NoError(t, err)
	require.Zero(t, db.execCalls)
	require.Zero(t, db.queryCalls)
}

func TestLivePollerHardFailureReturnsErrorAndSkipsSessionClose(t *testing.T) {
	// provider가 hard failure를 failures map에 넣는다.
	// Poll은 error를 반환하되 markEndedSessions는 호출하지 않는다.
}
```

### 12.5 integration: 실제 scraper.Client path로 blocking 검증

`fetchUpcoming` injection을 쓰면 `GetUpcomingEventsWaitAdmission`을 우회하므로 blocking admission을 검증할 수 없다. fake fetcher를 `scraper.Client`에 붙여 실제 method path를 지나게 한다.

권장 방식:

1. `scraper.NewClient(scraper.WithRateLimiter(scraper.NewRateLimiter(interval)))`
2. `scraper.WithFetcherEngine(scraper.FetcherEngineBrowserSnapshot)`
3. `scraper.WithBrowserSnapshotFetcher(fakeFetcher)`
4. fake fetcher는 `GetUpcomingEvents` parser가 읽을 수 있는 최소 `ytInitialData` HTML을 반환한다.
5. `htmlscraper.NewServiceWithYouTubeProducer(..., client, ...)`를 사용한다.
6. `FetchFromYouTubeProducerWaitAdmission`을 두 번 연속 호출했을 때 두 번째 호출이 interval만큼 대기하는지 확인한다.

## 구현 순서 권장

실제 구현 PR은 아래 순서로 나누는 것이 가장 안전하다.

1. `livestatus` package + poller deferred semantics test를 먼저 추가한다. 이 단계에서 poller test는 실패해야 한다.
2. `WaitWithBucket` rollback과 distributed limiter sentinel을 추가한다. 이 단계는 scraper/ratelimiter unit test만으로 검증 가능하다.
3. `FetchPolicy.AdmissionBlocking`, `LiveStatusFallbackFetchPolicy`, `GetUpcomingEventsWaitAdmission`, `FetchFromYouTubeProducerWaitAdmission`을 추가한다.
4. Holodex config와 fallback cap/budget/rotation/3분류를 구현한다.
5. live batch fallback budget units를 policy attempts 기준으로 보정한다.
6. integration/race/local-ci를 돌린다.

## 실행 명령

module 단위로 실행하는 쪽이 안전하다.

```bash
cd hololive/hololive-shared
go test ./pkg/service/youtube/livestatus/...
go test ./pkg/service/youtube/scraper/internal/scraping/...
go test ./pkg/service/youtube/scraper/...
go test ./pkg/service/holodex/...
go test ./pkg/service/youtube/poller/...

cd ../hololive-youtube-producer
go test ./internal/runtime/polling/...
go test ./...
```

race 검증:

```bash
cd hololive/hololive-shared
go test -race ./pkg/service/holodex/...
go test -race ./pkg/service/youtube/poller/...
```

전체 CI:

```bash
./scripts/ci/local-ci.sh
```

## PR 리뷰 체크리스트

- [ ] 기존 `FetchFromYouTubeProducer`는 blocking으로 바뀌지 않았다.
- [ ] live-status fallback만 `FetchFromYouTubeProducerWaitAdmission`을 사용한다.
- [ ] `fetchPagePreflight`의 hard cooldown check는 blocking/non-blocking 공통으로 먼저 실행된다.
- [ ] distributed limiter error는 sentinel/predicate로 분류한다. string matching 금지.
- [ ] `WaitWithBucket`은 zero-wait local commit도 distributed error에서 rollback한다.
- [ ] deferred-only 결과는 `GetChannelsLiveStatusWithFailures`에서 hard error가 아니다.
- [ ] deferred-only 결과는 `GetChannelsLiveStatus`에서도 hard error가 아니다.
- [ ] 단일 `LivePoller.Poll`이 detailed provider path를 우선 사용한다.
- [ ] deferred channel은 `markEndedSessions`를 호출하지 않고 nil error를 반환한다.
- [ ] hard failure channel은 `markEndedSessions`를 호출하지 않고 error를 반환한다.
- [ ] source-level hard error 검사는 deferred가 아닌 failed subset에만 적용한다.
- [ ] cap 초과/시간예산 초과/cooldown/admission-deferred/distributed-limiter-unavailable은 모두 typed deferred다.
- [ ] `liveBatchYouTubeScraperFallbackUnits`와 `LiveStatusFallbackFetchPolicy.MaxAttempts`가 불일치하지 않는다.
- [ ] fake `fetchUpcoming` injection test만으로 blocking 검증을 끝내지 않는다.
