# hololive 추가 수정본 정밀 리뷰 및 최종 수정 지침

대상 번들: `hololive-bot-full-20260411T171100Z.tar.gz`

검토 방식: 정적 코드 리뷰 기준입니다. 이 환경에서는 워크스페이스가 요구하는 Go 1.26.2 툴체인을 실행할 수 없어 테스트를 직접 돌리지는 못했습니다. 따라서 아래 평가는 **현재 소스와 테스트 코드, 호출 경로, 데이터 흐름, 스케줄링 구조**를 기준으로 한 정밀 리뷰입니다.

---

## 1. 최종 판정

이번 수정본은 이전까지 가장 치명적이었던 구조적 병목, 즉 **알림 대상 12채널인데 111채널 전체를 5개 poller로 계속 긁는 구조**를 제대로 제거했습니다. 이 부분은 이제 주병목이 아닙니다.

현재 기준으로 남은 핵심 이슈는 아래 두 축입니다.

1. **정확성 P0**  
   stream-ingester가 시작할 때 subscriber cache를 **DB 기준으로 파괴적 재구축**하는데, Kakao 알람 서비스는 **cache를 먼저 쓰고 DB는 비동기 반영**합니다. 이 둘이 충돌하면, stream-ingester 재시작 시점에 **더 최신인 cache 상태가 오래된 DB 상태로 덮여** 신규 구독이 사라지거나 제거된 구독이 되살아날 수 있습니다.

2. **지연/예산 P1**  
   새로 도입된 `PendingPublishedAtResolver`가 **poller와 동일한 YouTube shared rate limiter**를 사용하면서, backlog가 쌓일 경우 **실제 감지 poller보다 resolver가 예산을 더 많이 먹을 수 있는 구조**입니다. hot path에서 fetch를 뺀 방향은 맞지만, 지금 구현은 그 부담을 resolver로 옮긴 상태입니다.

즉, 이번 버전은 **주병목 제거는 성공**, 그러나 **제어면(source-of-truth)과 resolver budget 통제**가 아직 완전히 닫히지 않았습니다.

---

## 2. 이번 수정본에서 실제로 좋아진 점

아래는 이번 수정본에서 이미 잘 고쳐진 부분입니다.

### 2.1 타깃 집합 분리

`hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go`와  
`hololive-stream-ingester/internal/app/stream_ingester_poller_registrations.go` 기준으로,

- `videos`
- `shorts`
- `community`
- `live`

는 `notificationChannelIDs`만 타고,

- `channel_stats`

만 `statsChannelIDs`를 타도록 정리되었습니다.

이로써 이전의 `111 * 5 = 555 jobs` 구조가, 현재는 `12 * 4 + 111 * 1 = 159 jobs` 구조로 바뀌었습니다. 이 부분은 구조적으로 맞습니다.

### 2.2 outbox 고정 지연 감소

`hololive-shared/pkg/service/youtube/outbox/dispatcher.go` 기준으로,

- `PollInterval`이 30초 -> 2초로 감소했고
- 시작 직후 `processOnce()`를 한 번 바로 돌립니다.

그래서 "outbox row는 이미 생겼는데 30초 ticker까지 그냥 기다리는" 바닥 지연은 사라졌습니다.

### 2.3 scheduler coarse tick 제거

`hololive-shared/pkg/service/youtube/poller/scheduler.go` 기준으로,

- 1초 ticker 기반 dispatch가 없어졌고
- due time 기반 timer로 바뀌었으며
- worker 채널이 잠시 가득 차면 50ms 후 재시도합니다.

이 방향은 맞습니다.

### 2.4 subscriber lookup fallback 강화

`hololive-shared/pkg/service/alarm/targets.go` 기준으로,

- cache miss 시 DB fallback이 생겼고
- 같은 channel에 대한 DB load는 `singleflight`로 묶였습니다.

이전처럼 cache miss를 곧바로 "구독자 없음"으로 단정하는 위험은 크게 줄었습니다.

### 2.5 community/shorts hot path 추가 fetch 제거

`hololive-shared/pkg/service/youtube/poller/pollers.go` 기준으로,

- shorts/community poller는 더 이상 poll hot path에서 `ResolveVideoPublishedAt()` / `ResolveCommunityPostPublishedAt()`를 호출하지 않습니다.
- 그 책임은 `PendingPublishedAtResolver`로 이동했습니다.

이것 역시 방향 자체는 맞습니다.

---

## 3. 남은 이슈 우선순위

| 우선순위 | 분류 | 현재 문제 | 영향 |
|---|---|---|---|
| P0 | 정확성 / AI 냄새 | startup 시 stream-ingester가 subscriber cache를 DB로 파괴적 재구축 | 최신 구독 상태 소실 가능 |
| P1 | I/O 병목 / 지연 | PendingPublishedAtResolver가 shared YouTube budget을 잠식 가능 | shorts/community 알림 지연 재발 |
| P1 | 성능 병목 / DB | resolver가 growing-limit 방식으로 pending 후보를 다시 읽음 | backlog 시 O(n²) 성격의 DB 스캔 |
| P1 | 운영 안정성 | resolver가 poller와 다른 scraper client를 씀 | proxy 토글/런타임 상태 불일치 |
| P1 | 지연 | outbox가 tick당 한 번만 claim + delivery 처리 | backlog 시 2초 계단형 지연 |
| P2 | AI 냄새 / 운영성 | explicit target 검증이 panic 기반 | misconfig 시 crash로 종료 |
| P2 | 관측성 | resolver skip metric 이름이 실제 의미와 다름 | 운영 해석 오류 |

이제부터는 위 순서대로 고치면 됩니다.

---

## 4. P0: subscriber cache source-of-truth 충돌 수정

### 4.1 문제의 본질

현재 구조는 다음과 같습니다.

#### stream-ingester 쪽

`hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go`

- YouTube runtime 시작 시 `warmSubscriberCacheFromDB(...)`를 호출합니다.

`hololive-stream-ingester/internal/app/stream_ingester_alarm_cache.go`

- 이 함수는 `sharedalarm.RebuildSubscriberCacheFromRepository(...)`를 호출합니다.
- 즉 기존 subscriber cache key를 지운 뒤 DB snapshot으로 다시 채웁니다.

#### Kakao alarm service 쪽

`hololive-kakao-bot-go/internal/service/notification/alarm_service.go`

- `AddAlarm()` / `RemoveAlarm()`에서 cache를 먼저 갱신합니다.

`hololive-kakao-bot-go/internal/service/notification/alarm_persistence.go`

- DB 반영은 `persistAlarmAsync()` / `removeAlarmAsync()`로 비동기입니다.

즉 현재 시스템의 제어면은 사실상

- **실시간 authoritative source = cache**
- **복구용 source = DB**

인데, stream-ingester startup만 **DB를 authoritative처럼 취급**하고 있습니다.

이건 이번 수정본에서 새로 생긴 가장 큰 정확성 리스크입니다.

### 4.2 왜 이게 AI 냄새인가

이 구조는 기능 단위로 보면 각각 그럴듯합니다.

- “startup이면 DB에서 rebuild하자”
- “사용자 응답 빠르게 하려면 cache 먼저 쓰고 DB는 async로 쓰자”

하지만 둘을 함께 놓으면 source-of-truth가 서로 충돌합니다. 이런 식의 **국소 최적화 두 개를 이어 붙여 전역 의미가 깨지는 패턴**은 전형적인 AI 냄새입니다.

### 4.3 수정 원칙

stream-ingester startup은 다음 규칙으로 바꿔야 합니다.

1. **subscriber cache가 이미 살아 있으면 절대 파괴적 rebuild를 하지 않는다.**
2. **cache가 비어 있을 때만 DB에서 rebuild한다.**
3. startup target resolution도 같은 원칙으로 **cache-first, DB-fallback**으로 통일한다.

### 4.4 코드 수정안

#### 파일 1: `hololive-stream-ingester/internal/app/stream_ingester_alarm_cache.go`

현재는 무조건 `RebuildSubscriberCacheFromRepository`를 호출합니다. 이를 cache-cold일 때만 rebuild하도록 바꿉니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/stream_ingester_alarm_cache.go b/hololive/hololive-stream-ingester/internal/app/stream_ingester_alarm_cache.go
@@
 package app
 
 import (
 	"context"
 	"fmt"
 	"log/slog"
 
 	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
+	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
 	"github.com/kapu/hololive-shared/pkg/service/cache"
 	"github.com/kapu/hololive-shared/pkg/service/database"
 )
 
 var rebuildSubscriberCacheFromRepository = sharedalarm.RebuildSubscriberCacheFromRepository
 
-func warmSubscriberCacheFromDB(ctx context.Context, cacheService cache.Client, postgresService database.Client, logger *slog.Logger) error {
+type subscriberCacheWarmResult struct {
+	Summary sharedalarm.CacheWarmSummary
+	Rebuilt bool
+}
+
+func warmSubscriberCacheFromDBIfCacheCold(
+	ctx context.Context,
+	cacheService cache.Client,
+	postgresService database.Client,
+	logger *slog.Logger,
+) (subscriberCacheWarmResult, error) {
+	if cacheService == nil {
+		return subscriberCacheWarmResult{}, fmt.Errorf("warm subscriber cache from db if cache cold: cache service is nil")
+	}
+
+	channelIDs, err := cacheService.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
+	if err != nil {
+		return subscriberCacheWarmResult{}, fmt.Errorf("warm subscriber cache from db if cache cold: read channel registry: %w", err)
+	}
+	if len(channelIDs) > 0 {
+		if logger != nil {
+			logger.Info("subscriber_cache_rebuild_skipped",
+				slog.Int("existing_channel_registry_count", len(channelIDs)),
+			)
+		}
+		return subscriberCacheWarmResult{Rebuilt: false}, nil
+	}
+
 	repo := sharedalarm.NewRepository(postgresService, logger)
 
 	summary, err := rebuildSubscriberCacheFromRepository(ctx, cacheService, repo)
 	if err != nil {
-		return fmt.Errorf("warm subscriber cache from db: %w", err)
+		return subscriberCacheWarmResult{}, fmt.Errorf("warm subscriber cache from db if cache cold: %w", err)
 	}
 
 	if logger == nil {
-		return nil
+		return subscriberCacheWarmResult{Summary: summary, Rebuilt: true}, nil
 	}
 	logger.Info("subscriber_cache_rebuilt_from_db",
 		slog.Int("alarms_loaded", summary.AlarmCount),
 		slog.Int("rooms_loaded", summary.RoomCount),
 		slog.Int("channels_loaded", summary.ChannelCount),
 		slog.Int("keys_deleted", summary.KeysDeleted),
 	)
 
-	return nil
+	return subscriberCacheWarmResult{Summary: summary, Rebuilt: true}, nil
 }
```

핵심은 이름도 semantics에 맞게 바꾸는 것입니다. 지금 이름인 `warmSubscriberCacheFromDB`는 warm인지 rebuild인지가 불명확합니다.

#### 파일 2: `hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go`

startup에서 새 helper를 사용하도록 바꿉니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go b/hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go
@@
-	if warnErr := warmSubscriberCacheOnYouTubeStartup(ctx, spec.name, features.youtubeEnabled, func(ctx context.Context) error {
-		return warmSubscriberCacheFromDB(ctx, infra.cacheService, infra.postgresService, logger)
+	if warnErr := warmSubscriberCacheOnYouTubeStartup(ctx, spec.name, features.youtubeEnabled, func(ctx context.Context) error {
+		_, err := warmSubscriberCacheFromDBIfCacheCold(ctx, infra.cacheService, infra.postgresService, logger)
+		return err
 	}); warnErr != nil {
 		logger.Warn("Failed to warm subscriber cache from DB",
 			slog.String("runtime", spec.name),
 			slog.Any("error", warnErr),
 		)
 	}
```

#### 파일 3: 테스트 교체

`hololive-stream-ingester/internal/app/stream_ingester_alarm_cache_test.go`는 현재 “무조건 rebuild semantics여야 한다”는 전제를 테스트하고 있습니다. 이 테스트는 이제 바뀌어야 합니다.

추가해야 할 테스트는 아래 2개입니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/stream_ingester_alarm_cache_test.go b/hololive/hololive-stream-ingester/internal/app/stream_ingester_alarm_cache_test.go
@@
-func TestWarmSubscriberCacheFromDBUsesRebuildSemantics(t *testing.T) {
+func TestWarmSubscriberCacheFromDBIfCacheCold_RebuildsWhenRegistryEmpty(t *testing.T) {
@@
-func TestWarmSubscriberCacheFromDBLogsRebuildSummary(t *testing.T) {
+func TestWarmSubscriberCacheFromDBIfCacheCold_SkipsWhenRegistryAlreadyPopulated(t *testing.T) {
+	ctx := t.Context()
+	cacheSvc := cachemocks.NewLenientClient()
+	_, _ = cacheSvc.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{"UC_test"})
+
+	called := false
+	original := rebuildSubscriberCacheFromRepository
+	t.Cleanup(func() { rebuildSubscriberCacheFromRepository = original })
+	rebuildSubscriberCacheFromRepository = func(ctx context.Context, cacheService cache.Client, repo *sharedalarm.Repository) (sharedalarm.CacheWarmSummary, error) {
+		called = true
+		return sharedalarm.CacheWarmSummary{}, nil
+	}
+
+	result, err := warmSubscriberCacheFromDBIfCacheCold(ctx, cacheSvc, &databasemocks.Client{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
+	require.NoError(t, err)
+	assert.False(t, result.Rebuilt)
+	assert.False(t, called)
+}
```

### 4.5 추가 권장: startup target resolution도 cache-first로 통일

현재 `hololive-stream-ingester/internal/app/youtube_poll_targets.go`는 startup에서 DB만 읽습니다. refresher는 cache-first입니다. startup과 steady-state가 서로 다른 source를 보는 상태는 여전히 AI 냄새입니다.

아래처럼 `cache-first, DB-fallback` helper로 통일하는 것이 맞습니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/youtube_poll_targets.go b/hololive/hololive-stream-ingester/internal/app/youtube_poll_targets.go
@@
 import (
 	"context"
 	"fmt"
 
 	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
+	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
+	"github.com/kapu/hololive-shared/pkg/service/cache"
 	"github.com/kapu/hololive-shared/pkg/service/database"
 )
@@
 func resolveYouTubePollTargets(
 	ctx context.Context,
+	cacheService cache.Client,
 	postgresService database.Client,
 	operationalChannels []communityShortsOperationalChannel,
 ) (youtubePollTargets, error) {
-	alarmChannelIDs, err := loadAlarmChannelIDs(ctx, postgresService)
+	alarmChannelIDs, err := loadAlarmChannelIDsFromCacheOrDB(ctx, cacheService, postgresService)
 	if err != nil {
 		return youtubePollTargets{}, err
 	}
@@
 }
+
+func loadAlarmChannelIDsFromCacheOrDB(ctx context.Context, cacheService cache.Client, postgresService database.Client) ([]string, error) {
+	if cacheService != nil {
+		channelIDs, err := cacheService.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
+		if err == nil && len(channelIDs) > 0 {
+			return channelIDs, nil
+		}
+	}
+	return loadAlarmChannelIDs(ctx, postgresService)
+}
```

그리고 call site도 수정합니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go b/hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go
@@
-		ytPollTargets, err = resolveYouTubePollTargets(ctx, infra.postgresService, operationalChannels)
+		ytPollTargets, err = resolveYouTubePollTargets(ctx, infra.cacheService, infra.postgresService, operationalChannels)
```

이렇게 해야 startup과 refresher가 같은 source-of-truth 규칙을 따릅니다.

---

## 5. P1: PendingPublishedAtResolver budget 통제

### 5.1 현재 문제

`hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go`의 `buildPendingPublishedAtResolver()`를 보면 resolver는 새 `scraper.Client`를 만들고, 여기에 poller와 같은 `sharedRL`을 넣습니다.

`hololive-shared/pkg/service/youtube/poller/published_at_resolver.go`의 `runOnce()`를 보면,

- `15s` 주기로 실행되고
- `batchSize=50`이며
- limit를 `50, 100, 150...`으로 계속 늘리면서
- 각 candidate마다 추가 HTTP resolve를 시도합니다.

즉 backlog가 생기면 resolver는 poller와 같은 예산 통 안에서 **자기 일을 가능한 만큼 계속 밀어넣는 구조**입니다.

hot path에서 fetch를 뺀 대신, 지금은 resolver가 예산을 잡아먹을 수 있습니다. 이건 **병목을 없앤 것이 아니라 lane을 바꾼 것**에 가깝습니다.

### 5.2 수정 원칙

resolver에는 세 가지 제약이 필요합니다.

1. **shared budget 위에 resolver 전용 soft budget을 하나 더 둔다.**
2. **너무 최근에 detected된 후보는 바로 resolve하지 않는다.**
3. **같은 후보의 반복 실패에 backoff를 둔다.**

### 5.3 코드 수정안

#### 파일 1: `hololive-shared/pkg/service/youtube/poller/published_at_resolver.go`

resolver 구조체를 확장합니다.

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/poller/published_at_resolver.go b/hololive/hololive-shared/pkg/service/youtube/poller/published_at_resolver.go
@@
 type PendingPublishedAtResolver struct {
 	db           *gorm.DB
 	client       *scraper.Client
 	routeDecider NotificationRouteDecider
 	interval     time.Duration
 	batchSize    int
+	maxResolvePerRun int
+	minDetectedAge   time.Duration
+	failureBackoffTTL time.Duration
+	softLimiter      *scraper.RateLimiter
+	backoffStore     pendingPublishedAtBackoffStore
 	logger       *slog.Logger
 }
@@
 func NewPendingPublishedAtResolver(
 	db *gorm.DB,
 	client *scraper.Client,
 	routeDecider NotificationRouteDecider,
 	interval time.Duration,
 	batchSize int,
+	maxResolvePerRun int,
+	minDetectedAge time.Duration,
+	failureBackoffTTL time.Duration,
+	softLimiter *scraper.RateLimiter,
+	backoffStore pendingPublishedAtBackoffStore,
 	logger *slog.Logger,
 ) *PendingPublishedAtResolver {
@@
+	if maxResolvePerRun <= 0 {
+		maxResolvePerRun = batchSize
+	}
+	if minDetectedAge <= 0 {
+		minDetectedAge = 20 * time.Second
+	}
+	if failureBackoffTTL <= 0 {
+		failureBackoffTTL = 5 * time.Minute
+	}
 	return &PendingPublishedAtResolver{
 		db:           db,
 		client:       client,
 		routeDecider: routeDecider,
 		interval:     interval,
 		batchSize:    batchSize,
+		maxResolvePerRun: maxResolvePerRun,
+		minDetectedAge: minDetectedAge,
+		failureBackoffTTL: failureBackoffTTL,
+		softLimiter: softLimiter,
+		backoffStore: backoffStore,
 		logger:       logger,
 	}
 }
@@
-	for {
-		if err := r.runOnce(ctx, time.Now()); err != nil && ctx.Err() == nil {
+	for {
+		detectedBefore := time.Now().Add(-r.minDetectedAge)
+		if err := r.runOnce(ctx, detectedBefore); err != nil && ctx.Err() == nil {
 			r.logger.Warn("Pending published_at resolver iteration failed",
 				slog.Any("error", err),
 			)
 		}
@@
-	seen := make(map[string]struct{})
-	for limit := r.batchSize; ; limit += r.batchSize {
-		candidates, err := trackingrepo.NewRepository(r.db).ListPendingPublishedAtResolutions(ctx, detectedBefore, limit)
+	processed := 0
+	var cursor *trackingrepo.PublishedAtResolutionCursor
+	for processed < r.maxResolvePerRun {
+		candidates, nextCursor, err := trackingrepo.NewRepository(r.db).ListPendingPublishedAtResolutionsPage(ctx, detectedBefore, cursor, minInt(r.batchSize, r.maxResolvePerRun-processed))
 		if err != nil {
 			return fmt.Errorf("run pending published_at resolver: list candidates: %w", err)
 		}
 		setPublishedAtResolverPendingCandidates(len(candidates))
 		if len(candidates) == 0 {
 			return nil
 		}
-
-		newCandidates := make([]trackingrepo.PublishedAtResolutionCandidate, 0, len(candidates))
-		for i := range candidates {
-			key := pendingPublishedAtCandidateKey(candidates[i])
-			if _, exists := seen[key]; exists {
-				continue
-			}
-			seen[key] = struct{}{}
-			newCandidates = append(newCandidates, candidates[i])
-		}
-		if len(newCandidates) == 0 {
-			return nil
-		}
-
-		for i := range newCandidates {
+
+		for i := range candidates {
 			select {
 			case <-ctx.Done():
 				return ctx.Err()
 			default:
 			}
+
+			candidate := candidates[i]
+			if r.backoffStore != nil {
+				active, err := r.backoffStore.Active(ctx, candidate)
+				if err == nil && active {
+					observePublishedAtResolverSkipped(candidate.Kind, "backoff_active")
+					continue
+				}
+			}
+
+			if r.softLimiter != nil {
+				if err := r.softLimiter.Wait(ctx); err != nil {
+					return fmt.Errorf("run pending published_at resolver: wait soft limiter: %w", err)
+				}
+			}
 
-			observePublishedAtResolutionAttempt(newCandidates[i].Kind)
-			publishedAt, err := r.resolveCandidatePublishedAt(ctx, newCandidates[i])
+			observePublishedAtResolutionAttempt(candidate.Kind)
+			publishedAt, err := r.resolveCandidatePublishedAt(ctx, candidate)
 			if err != nil {
-				observePublishedAtResolutionFailure(newCandidates[i].Kind)
+				observePublishedAtResolutionFailure(candidate.Kind)
+				if r.backoffStore != nil {
+					_ = r.backoffStore.Mark(ctx, candidate, r.failureBackoffTTL)
+				}
 				r.logger.Warn("Pending published_at resolver failed to resolve candidate",
-					slog.String("kind", string(newCandidates[i].Kind)),
-					slog.String("post_id", newCandidates[i].PostID),
-					slog.String("content_id", newCandidates[i].ContentID),
+					slog.String("kind", string(candidate.Kind)),
+					slog.String("post_id", candidate.PostID),
+					slog.String("content_id", candidate.ContentID),
 					slog.Any("error", err),
 				)
 				continue
 			}
 			if publishedAt == nil || publishedAt.IsZero() {
+				if r.backoffStore != nil {
+					_ = r.backoffStore.Mark(ctx, candidate, r.failureBackoffTTL)
+				}
+				observePublishedAtResolverSkipped(candidate.Kind, "published_at_empty")
 				continue
 			}
-			observePublishedAtResolutionSuccess(newCandidates[i].Kind)
+			if r.backoffStore != nil {
+				_ = r.backoffStore.Clear(ctx, candidate)
+			}
+			observePublishedAtResolutionSuccess(candidate.Kind)
 
-			result, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, newCandidates[i], *publishedAt, r.routeDecider)
+			result, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, candidate, *publishedAt, r.routeDecider)
 			if err != nil {
 				r.logger.Warn("Pending published_at resolver failed to finalize candidate",
-					slog.String("kind", string(newCandidates[i].Kind)),
-					slog.String("post_id", newCandidates[i].PostID),
-					slog.String("content_id", newCandidates[i].ContentID),
+					slog.String("kind", string(candidate.Kind)),
+					slog.String("post_id", candidate.PostID),
+					slog.String("content_id", candidate.ContentID),
 					slog.Any("error", err),
 				)
 				continue
 			}
 			if result.enqueued {
-				observePublishedAtResolverEnqueued(newCandidates[i].Kind)
+				observePublishedAtResolverEnqueued(candidate.Kind)
 				continue
 			}
-			observePublishedAtResolutionBackoffSkip(newCandidates[i].Kind)
+			observePublishedAtResolverSkipped(candidate.Kind, result.reason)
+			processed++
 		}
-
-		if len(candidates) < limit {
+		processed += len(candidates)
+		if nextCursor == nil {
 			return nil
 		}
+		cursor = nextCursor
 	}
+	return nil
 }
```

#### 파일 2: 새 backoff store 추가

새 파일을 하나 추가합니다.

`hololive-shared/pkg/service/youtube/poller/published_at_resolver_backoff.go`

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/poller/published_at_resolver_backoff.go b/hololive/hololive-shared/pkg/service/youtube/poller/published_at_resolver_backoff.go
new file mode 100644
@@
+package poller
+
+import (
+	"context"
+	"fmt"
+	"strings"
+	"time"
+
+	"github.com/kapu/hololive-shared/pkg/domain"
+	"github.com/kapu/hololive-shared/pkg/service/cache"
+	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
+)
+
+type pendingPublishedAtBackoffStore interface {
+	Active(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate) (bool, error)
+	Mark(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate, ttl time.Duration) error
+	Clear(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate) error
+}
+
+type cachePublishedAtBackoffStore struct {
+	cache cache.Client
+}
+
+func newCachePublishedAtBackoffStore(cacheSvc cache.Client) pendingPublishedAtBackoffStore {
+	if cacheSvc == nil {
+		return nil
+	}
+	return &cachePublishedAtBackoffStore{cache: cacheSvc}
+}
+
+func (s *cachePublishedAtBackoffStore) Active(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate) (bool, error) {
+	if s == nil || s.cache == nil {
+		return false, nil
+	}
+	return s.cache.Exists(ctx, publishedAtBackoffKey(candidate.Kind, candidate.PostID))
+}
+
+func (s *cachePublishedAtBackoffStore) Mark(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate, ttl time.Duration) error {
+	if s == nil || s.cache == nil {
+		return nil
+	}
+	return s.cache.Set(ctx, publishedAtBackoffKey(candidate.Kind, candidate.PostID), "1", ttl)
+}
+
+func (s *cachePublishedAtBackoffStore) Clear(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate) error {
+	if s == nil || s.cache == nil {
+		return nil
+	}
+	return s.cache.Del(ctx, publishedAtBackoffKey(candidate.Kind, candidate.PostID))
+}
+
+func publishedAtBackoffKey(kind domain.OutboxKind, postID string) string {
+	return fmt.Sprintf("youtube:published_at_backoff:%s:%s", strings.TrimSpace(string(kind)), strings.TrimSpace(postID))
+}
```

#### 파일 3: builder helper 수정

`hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go`

resolver에 soft limiter와 backoff store를 주입합니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go b/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go
@@
 func buildPendingPublishedAtResolver(
-	scraperCfg config.ScraperConfig,
 	postgresService database.Client,
-	cacheService cache.Client,
-	sharedRL *scraper.RateLimiter,
+	scraperClient *scraper.Client,
+	cacheService cache.Client,
 	routeDecider poller.NotificationRouteDecider,
 	logger *slog.Logger,
 ) *poller.PendingPublishedAtResolver {
 	if postgresService == nil {
 		return nil
 	}
-
-	proxyConfig := scraper.ProxyConfig{...}
-	resolverClient := scraper.NewClient(...)
+	if scraperClient == nil {
+		return nil
+	}
+	softLimiter := scraper.NewRateLimiter(15 * time.Second)
+	backoffStore := poller.NewCachePublishedAtBackoffStore(cacheService)
 
 	return poller.NewPendingPublishedAtResolver(
 		postgresService.GetGormDB(),
-		resolverClient,
+		scraperClient,
 		routeDecider,
 		15*time.Second,
 		50,
+		20,
+		20*time.Second,
+		5*time.Minute,
+		softLimiter,
+		backoffStore,
 		logger,
 	)
 }
```

여기서 `maxResolvePerRun=20` 정도로 시작하는 이유는, resolver가 poller를 이기지 못하게 하려는 의도입니다. 이후 운영 지표를 보고 상향하면 됩니다.

---

## 6. P1: resolver pending 후보 조회를 keyset pagination으로 교체

### 6.1 현재 문제

`hololive-shared/pkg/service/youtube/poller/published_at_resolver.go`

- `limit := batchSize; limit += batchSize` 패턴을 씁니다.

`hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go`

- `ORDER BY detected_at ASC LIMIT ?`만 사용합니다.

이 조합은 backlog가 커질수록 같은 앞부분 후보를 여러 번 다시 읽게 만듭니다. 즉 resolver가 YouTube 요청만 잡아먹는 것이 아니라, DB도 비효율적으로 읽습니다.

### 6.2 수정 원칙

- cursor를 `(detected_at, post_id)`로 둡니다.
- query는 keyset pagination으로 바꿉니다.
- partial index를 추가합니다.

### 6.3 코드 수정안

#### 파일 1: `hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go`

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go b/hololive/hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go
@@
+type PublishedAtResolutionCursor struct {
+	DetectedAt time.Time
+	PostID     string
+}
+
-func (r *GormRepository) ListPendingPublishedAtResolutions(
+func (r *GormRepository) ListPendingPublishedAtResolutionsPage(
 	ctx context.Context,
 	detectedBefore time.Time,
-	limit int,
-) ([]PublishedAtResolutionCandidate, error) {
+	cursor *PublishedAtResolutionCursor,
+	limit int,
+) ([]PublishedAtResolutionCandidate, *PublishedAtResolutionCursor, error) {
@@
-	if err := r.db.WithContext(ctx).
+	query := r.db.WithContext(ctx).
 		Model(&domain.YouTubeCommunityShortsAlarmState{}).
 		Select("kind, post_id, content_id, channel_id, detected_at").
 		Where("kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
 		Where("actual_published_at IS NULL").
 		Where("alarm_sent_at IS NULL").
 		Where("authorized_at IS NULL").
-		Where("detected_at < ?", yttimestamp.Normalize(detectedBefore)).
-		Order("detected_at ASC").
-		Limit(limit).
-		Find(&rows).Error; err != nil {
+		Where("detected_at < ?", yttimestamp.Normalize(detectedBefore))
+
+	if cursor != nil {
+		query = query.Where(
+			"(detected_at > ?) OR (detected_at = ? AND post_id > ?)",
+			yttimestamp.Normalize(cursor.DetectedAt),
+			yttimestamp.Normalize(cursor.DetectedAt),
+			cursor.PostID,
+		)
+	}
+
+	if err := query.
+		Order("detected_at ASC").
+		Order("post_id ASC").
+		Limit(limit).
+		Find(&rows).Error; err != nil {
-		return nil, fmt.Errorf("list pending published_at resolutions: query rows: %w", err)
+		return nil, nil, fmt.Errorf("list pending published_at resolutions page: query rows: %w", err)
 	}
@@
-	return candidates, nil
+	if len(candidates) == 0 {
+		return nil, nil, nil
+	}
+	last := candidates[len(candidates)-1]
+	nextCursor := &PublishedAtResolutionCursor{DetectedAt: last.DetectedAt, PostID: last.PostID}
+	if len(candidates) < limit {
+		nextCursor = nil
+	}
+	return candidates, nextCursor, nil
 }
```

#### 파일 2: migration 추가

새 migration 파일을 추가합니다.

`hololive-kakao-bot-go/scripts/migrations/056_add_ycsas_pending_published_at_resolution_index.sql`

```diff
diff --git a/hololive/hololive-kakao-bot-go/scripts/migrations/056_add_ycsas_pending_published_at_resolution_index.sql b/hololive/hololive-kakao-bot-go/scripts/migrations/056_add_ycsas_pending_published_at_resolution_index.sql
new file mode 100644
@@
+CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_resolution
+ON youtube_community_shorts_alarm_states (detected_at ASC, post_id ASC)
+WHERE actual_published_at IS NULL
+  AND alarm_sent_at IS NULL
+  AND authorized_at IS NULL
+  AND kind IN ('COMMUNITY_POST', 'NEW_SHORT');
```

`kind` 값은 실제 DB enum/string 저장값과 맞춰 확인 후 반영해야 합니다. 위 값은 현재 코드의 `domain.OutboxKindCommunityPost`, `domain.OutboxKindNewShort` 기준 제안입니다.

---

## 7. P1: resolver와 poller가 같은 scraper client를 공유하도록 수정

### 7.1 현재 문제

현재 poller와 resolver는 같은 `sharedRL`을 쓰지만, 서로 다른 `scraper.Client` 인스턴스를 씁니다.

이 구조의 문제는 두 가지입니다.

1. runtime proxy toggle은 scheduler 안의 poller들에게만 전파됩니다.
2. resolver는 같은 rate limiter를 써도 다른 client instance라서, proxy/state/runtime 설정이 완전히 같은 보장을 못 합니다.

특히 YouTube scraping 문제가 생겨 runtime에서 proxy를 끄거나 켜는 경우, poller는 반영되는데 resolver는 그대로 남을 수 있습니다.

### 7.2 수정 원칙

- `scraper.Client`는 builder에서 한 번만 만들고
- poller와 resolver가 **같은 인스턴스**를 공유해야 합니다.

### 7.3 코드 수정안

#### 파일 1: `hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go`

새 helper를 추가합니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go b/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go
@@
+func buildSharedYouTubeScraperClient(
+	scraperCfg config.ScraperConfig,
+	cacheService cache.Client,
+	sharedRL *scraper.RateLimiter,
+) *scraper.Client {
+	proxyConfig := scraper.ProxyConfig{
+		Enabled: scraperCfg.ProxyEnabled,
+		URL:     scraperCfg.ProxyURL,
+	}
+	return scraper.NewClient(
+		scraper.WithProxy(proxyConfig),
+		scraper.WithRateLimiter(sharedRL),
+		scraper.WithStateStore(cacheService),
+	)
+}
```

#### 파일 2: `hololive-stream-ingester/internal/app/stream_ingester_poller_registrations.go`

poller registrations builder가 client를 직접 만들지 않도록 바꿉니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/stream_ingester_poller_registrations.go b/hololive/hololive-stream-ingester/internal/app/stream_ingester_poller_registrations.go
@@
 func buildStreamIngesterChannelPollerRegistrations(
 	postgres database.Client,
-	scraperCfg config.ScraperConfig,
-	sharedRL *scraper.RateLimiter,
-	cacheSvc cache.Client,
+	scraperClient *scraper.Client,
 	routeDecider poller.NotificationRouteDecider,
 	notificationChannelIDs []string,
 	statsChannelIDs []string,
 ) []providers.ChannelPollerRegistration {
-	proxyConfig := scraper.ProxyConfig{...}
-	poll := scraperCfg.PollOrDefault()
+	poll := scraperCfg.PollOrDefault()
 	communityKeywords := []string{}
-
-	scraperClient := scraper.NewClient(...)
 	db := postgres.GetGormDB()
```

`poll := scraperCfg.PollOrDefault()` 때문에 `scraperCfg`는 그대로 필요합니다. 따라서 함수 시그니처는 `scraperCfg`는 유지하고 client만 외부에서 주입하는 형태가 좋습니다.

정확한 최종 형태는 아래가 더 안전합니다.

```diff
-func buildStreamIngesterChannelPollerRegistrations(
-	postgres database.Client,
-	scraperCfg config.ScraperConfig,
-	sharedRL *scraper.RateLimiter,
-	cacheSvc cache.Client,
+func buildStreamIngesterChannelPollerRegistrations(
+	postgres database.Client,
+	scraperCfg config.ScraperConfig,
+	scraperClient *scraper.Client,
 	routeDecider poller.NotificationRouteDecider,
 	notificationChannelIDs []string,
 	statsChannelIDs []string,
 ) []providers.ChannelPollerRegistration {
```

#### 파일 3: `hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go`

builder에서 shared client를 생성하고 poller/resolver에 같이 넘깁니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go b/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go
@@
 func buildStreamIngesterYouTubeComponents(
 	scraperCfg config.ScraperConfig,
 	postgresService database.Client,
@@
 ) (*poller.Scheduler, *outbox.Dispatcher, []providers.ChannelPollerRegistration) {
+	scraperClient := buildSharedYouTubeScraperClient(scraperCfg, cacheService, sharedRL)
+
 	pollerRegistrations := buildStreamIngesterChannelPollerRegistrations(
 		postgresService,
 		scraperCfg,
-		sharedRL,
-		cacheService,
+		scraperClient,
 		routeDecider,
 		notificationChannelIDs,
 		statsChannelIDs,
 	)
```

그리고 bootstrap의 resolver builder도 공유 client를 받도록 맞춰야 합니다.

이 변경이 들어가면 proxy toggle 불일치가 사라지고, resolver와 poller가 같은 scraping runtime 상태를 공유합니다.

---

## 8. P1: outbox를 drain mode로 바꿔 backlog 지연 제거

### 8.1 현재 문제

`hololive-shared/pkg/service/youtube/outbox/dispatcher.go`

현재 `processOnce()`는 매 tick마다

1. outbox claim 한 번
2. enqueue 한 번
3. delivery fetch/process 한 번

만 수행합니다.

즉 backlog가 큰 경우, 처리량은 충분해도 지연이 `2초 단위 계단`으로 누적됩니다.

### 8.2 수정 원칙

- idle일 때만 ticker를 기다리고
- backlog가 있으면 한 tick 안에서 몇 라운드 더 drain합니다.

### 8.3 코드 수정안

#### 파일 1: `hololive-shared/pkg/service/youtube/outbox/dispatcher_claim.go`

처리량을 외부에서 알 수 있도록 count를 반환합니다.

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_claim.go b/hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_claim.go
@@
-func (d *Dispatcher) processPerRoomBatch(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox) {
+func (d *Dispatcher) processPerRoomBatch(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox) int {
 	roomsByChannel := d.collectRoomsByChannel(ctx, outboxItems)
 	d.enqueueDeliveries(ctx, outboxItems, roomsByChannel)
-	d.processPendingDeliveries(ctx)
+	return d.processPendingDeliveries(ctx)
 }
@@
-func (d *Dispatcher) processPendingDeliveries(ctx context.Context) {
+func (d *Dispatcher) processPendingDeliveries(ctx context.Context) int {
 	rows, err := d.delivery.FetchAndLock(ctx, d.cfg.BatchSize, d.cfg.LockTimeout)
 	if err != nil {
 		d.logger.Error("Failed to fetch delivery rows", slog.Any("error", err))
-		return
+		return 0
 	}
 	if len(rows) == 0 {
-		return
+		return 0
 	}
@@
-	result := d.dispatchDeliveryRows(ctx, rows, outboxByID)
+	result := d.dispatchDeliveryRows(ctx, rows, outboxByID)
@@
+	return len(rows)
 }
```

#### 파일 2: `hololive-shared/pkg/service/youtube/outbox/dispatcher.go`

`processOnce()`를 drain loop로 바꿉니다.

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go b/hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go
@@
-func (d *Dispatcher) processOnce(ctx context.Context) {
-	outboxItems, err := d.claimOutboxBatch(ctx)
-	if err != nil {
-		d.logger.Error("Failed to fetch outbox items", slog.Any("error", err))
-		return
-	}
-
-	if len(outboxItems) > 0 {
-		d.logger.Debug("Processing outbox batch", slog.Int("count", len(outboxItems)))
-	}
-
-	d.processPerRoomBatch(ctx, outboxItems)
+func (d *Dispatcher) processOnce(ctx context.Context) {
+	d.processAvailable(ctx, 4)
+}
+
+func (d *Dispatcher) processAvailable(ctx context.Context, maxRounds int) {
+	if maxRounds <= 0 {
+		maxRounds = 1
+	}
+	for round := 0; round < maxRounds; round++ {
+		outboxItems, err := d.claimOutboxBatch(ctx)
+		if err != nil {
+			d.logger.Error("Failed to fetch outbox items", slog.Any("error", err))
+			return
+		}
+		deliveryCount := 0
+		if len(outboxItems) > 0 {
+			d.logger.Debug("Processing outbox batch", slog.Int("count", len(outboxItems)), slog.Int("round", round+1))
+			deliveryCount = d.processPerRoomBatch(ctx, outboxItems)
+		} else {
+			deliveryCount = d.processPendingDeliveries(ctx)
+		}
+		if len(outboxItems) == 0 && deliveryCount == 0 {
+			return
+		}
+	}
 }
```

운영적으로는 `maxRounds=4` 정도면 충분합니다. burst가 커도 한 tick에서 일정량 더 비워 주고, 무한 monopolization은 막을 수 있습니다.

---

## 9. P2: explicit-target 검증을 panic이 아니라 startup error로 변경

### 9.1 현재 문제

`hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go`의 `validateExplicitPollerRegistrations()`는 실패 시 `panic()`합니다.

이건 개발 중에는 빠르게 눈에 띄지만, 운영에서는 stack trace crash로 끝납니다. 현재 이 검증은 config validation 성격이므로 `error`를 반환하고 startup fail-fast 하는 것이 맞습니다.

### 9.2 코드 수정안

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go b/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go
@@
 func buildStreamIngesterYouTubeComponents(
@@
-	logger *slog.Logger,
-) (*poller.Scheduler, *outbox.Dispatcher, []providers.ChannelPollerRegistration) {
+	logger *slog.Logger,
+) (*poller.Scheduler, *outbox.Dispatcher, []providers.ChannelPollerRegistration, error) {
@@
-	validateExplicitPollerRegistrations(pollerRegistrations)
+	if err := validateExplicitPollerRegistrations(pollerRegistrations); err != nil {
+		return nil, nil, nil, err
+	}
@@
-	return scraperScheduler, outboxDispatcher, pollerRegistrations
+	return scraperScheduler, outboxDispatcher, pollerRegistrations, nil
 }
@@
-func validateExplicitPollerRegistrations(registrations []providers.ChannelPollerRegistration) {
+func validateExplicitPollerRegistrations(registrations []providers.ChannelPollerRegistration) error {
 	missing := make([]string, 0)
@@
 	if len(missing) == 0 {
-		return
+		return nil
 	}
-	panic(fmt.Sprintf(
+	return fmt.Errorf(
 		"stream-ingester poller registrations require explicit channel IDs: %s",
 		strings.Join(missing, ", "),
-	))
+	)
 }
```

그리고 bootstrap call site도 맞춰야 합니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go b/hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go
@@
-		scraperScheduler, outboxDispatcher, pollerRegistrations = buildStreamIngesterYouTubeComponents(
+		scraperScheduler, outboxDispatcher, pollerRegistrations, err = buildStreamIngesterYouTubeComponents(
 			...
 		)
+		if err != nil {
+			infra.cleanup()
+			return nil, err
+		}
```

이렇게 바꾸면 misconfiguration이 있을 때 훨씬 읽기 쉬운 startup error로 종료됩니다.

---

## 10. P2: resolver metric 이름 정정

### 10.1 현재 문제

`hololive-shared/pkg/service/youtube/poller/metrics.go`의

- `publishedAtResolutionBackoffSkipTotal`

은 이름상 “backoff 때문에 skip”처럼 보이는데, 현재 실제 코드는

- `already_claimed`
- `route_decider_rejected`
- `already_sent`

같은 이유에도 이 metric을 올립니다.

즉 metric 의미가 틀렸습니다.

### 10.2 수정안

metric을 일반 skip metric으로 바꾸고 reason label을 붙입니다.

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/poller/metrics.go b/hololive/hololive-shared/pkg/service/youtube/poller/metrics.go
@@
-	publishedAtResolutionBackoffSkipTotal *prometheus.CounterVec
+	publishedAtResolverSkippedTotal *prometheus.CounterVec
@@
-		publishedAtResolutionBackoffSkipTotal = promauto.NewCounterVec(prometheus.CounterOpts{
-			Name: "youtube_poller_published_at_resolution_backoff_skip_total",
-			Help: "resolver가 published_at 해석 불가 후보를 skip한 횟수",
-		}, []string{"kind"})
+		publishedAtResolverSkippedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
+			Name: "youtube_poller_published_at_resolver_skipped_total",
+			Help: "resolver가 후보를 enqueue 없이 건너뛴 횟수",
+		}, []string{"kind", "reason"})
@@
-func observePublishedAtResolutionBackoffSkip(kind domain.OutboxKind) {
+func observePublishedAtResolverSkipped(kind domain.OutboxKind, reason string) {
 	ensureMetrics()
-	publishedAtResolutionBackoffSkipTotal.WithLabelValues(string(kind)).Inc()
+	publishedAtResolverSkippedTotal.WithLabelValues(string(kind), reason).Inc()
 }
```

그리고 resolver call site에서 이유별로 분기해 주면 됩니다.

---

## 11. 아직 남아 있는 AI 냄새 정리

이번 수정본은 이전보다 훨씬 나아졌지만, 아직 아래 냄새가 남아 있습니다.

### 11.1 source-of-truth가 경로마다 다름

- Kakao alarm write path: cache-first, DB-async
- stream-ingester startup: DB-authoritative rebuild
- poll target refresher: cache-first, DB validation

이 셋은 동일한 제어면을 보고 있어야 합니다.

### 11.2 같은 역할의 scraper client를 두 개 만듦

poller와 resolver는 같은 외부 자원을 다루는데, builder가 다른 client instance를 따로 만듭니다. 이런 구조는 기능이 늘수록 runtime toggle과 state drift를 낳기 쉽습니다.

### 11.3 validation이 panic 기반

runtime builder의 validation이 error-return이 아니라 panic입니다. 이건 라이브러리/infra 계층보다는 임시 패치 성격의 코드에서 자주 보이는 패턴입니다.

### 11.4 metric 이름이 구현 의미와 어긋남

운영 지표 이름이 실제 skip 사유와 일치하지 않습니다. 이런 부분은 장애 시 사람을 잘못된 방향으로 이끕니다.

---

## 12. 최종 권장 patch 순서

아래 순서대로 적용하는 것이 가장 안전합니다.

### 1단계
`stream_ingester_alarm_cache.go`, `bootstrap_stream_ingester.go`, `youtube_poll_targets.go`

- startup cache rebuild를 cache-cold일 때만 수행
- startup target resolution을 cache-first로 통일

이 단계는 **정확성 P0**입니다. 가장 먼저 해야 합니다.

### 2단계
`published_at_resolver.go`, `published_at_resolver_backoff.go`, `tracking/alarm_state_repository.go`, migration `056`

- resolver에 soft budget, minDetectedAge, backoff, keyset pagination 적용

이 단계가 **현재 남은 가장 큰 지연/예산 병목**입니다.

### 3단계
`stream_ingester_runtime_builder_helpers.go`, `stream_ingester_poller_registrations.go`, `bootstrap_stream_ingester.go`

- poller/resolver가 shared scraper client를 사용하도록 정리

이 단계는 **runtime robustness**와 **proxy toggle 일관성**을 위한 단계입니다.

### 4단계
`outbox/dispatcher.go`, `outbox/dispatcher_claim.go`

- outbox drain loop 적용

이 단계는 **burst backlog 시 계단형 지연**을 줄입니다.

### 5단계
`stream_ingester_runtime_builder_helpers.go`, `bootstrap_stream_ingester.go`

- explicit-target validation panic -> error

이건 운영성 개선이지만, 1~4가 끝난 뒤 바로 묶어 넣는 것이 좋습니다.

---

## 13. 배포 후 꼭 봐야 할 로그와 지표

### 13.1 시작 직후

- `subscriber_cache_rebuild_skipped` 또는 `subscriber_cache_rebuilt_from_db`
- `Resolved YouTube poll targets`
- `Scraper scheduler initialized`

여기서 보고 싶은 값은

- notification target count가 기대치와 맞는지
- total_jobs가 159 근처인지
- rebuild가 불필요하게 자주 일어나지 않는지

입니다.

### 13.2 resolver 관련

새로 추가/수정할 지표는 최소 아래 정도가 좋습니다.

- `youtube_poller_published_at_resolution_attempt_total{kind}`
- `youtube_poller_published_at_resolution_failure_total{kind}`
- `youtube_poller_published_at_resolver_skipped_total{kind,reason}`
- `youtube_poller_published_at_resolver_enqueued_total{kind}`
- `youtube_poller_published_at_resolver_pending_candidates`

특히 아래 두 비율을 봐야 합니다.

1. `failure / attempt`
2. `skipped{reason="backoff_active"} / attempt`

이 값이 높으면 YouTube page stabilization 문제 또는 resolver budget이 너무 공격적이라는 뜻입니다.

### 13.3 outbox backlog

- tick당 한 번만 처리하던 구조를 drain mode로 바꾼 뒤에는, burst 상황에서 outbox pending count가 2초 단위 계단처럼 증가하지 않는지 봐야 합니다.

---

## 14. 테스트 추가 목록

아래 테스트는 반드시 같이 들어가야 합니다.

### startup cache / targets

- `TestWarmSubscriberCacheFromDBIfCacheCold_RebuildsWhenRegistryEmpty`
- `TestWarmSubscriberCacheFromDBIfCacheCold_SkipsWhenRegistryAlreadyPopulated`
- `TestResolveYouTubePollTargets_UsesCacheBeforeDB`
- `TestResolveYouTubePollTargets_FallsBackToDBWhenCacheEmpty`

### resolver

- `TestPendingPublishedAtResolver_RespectsMaxResolvePerRun`
- `TestPendingPublishedAtResolver_SkipsFreshCandidatesBeforeMinDetectedAge`
- `TestPendingPublishedAtResolver_AppliesFailureBackoff`
- `TestPendingPublishedAtResolver_KeysetPaginationStableWithSameDetectedAt`
- `TestPendingPublishedAtResolver_UsesSharedScraperClientProxyState`

### outbox

- `TestDispatcher_ProcessAvailable_DrainsMultipleRounds`
- `TestDispatcher_ProcessAvailable_StopsWhenIdle`

### validation

- `TestBuildStreamIngesterYouTubeComponents_ReturnsErrorWhenRegistrationMissingExplicitChannelIDs`

---

## 15. 최종 요약

현재 수정본은 이전까지의 최대 병목이었던 **과다 스크래핑 문제를 제대로 해결한 버전**입니다. 이 점은 분명히 좋습니다.

다만 이제 남은 핵심은 단순 튜닝이 아닙니다.

1. **subscriber cache를 누가 authoritative하게 볼 것인가**
2. **resolver가 YouTube request budget을 얼마나 가져갈 수 있는가**
3. **backlog 상황에서 DB/outbox가 얼마나 효율적으로 drain되는가**

이 세 가지를 마저 닫아야 “완성된 수정”이 됩니다.

가장 중요한 결론 한 줄로 정리하면 이렇습니다.

> 이번 버전은 주병목 제거에는 성공했지만, startup cache rebuild와 resolver budget 통제를 그대로 두면 정확성 문제와 shorts/community 지연이 다른 경로로 다시 나타날 수 있다.

따라서 실제 머지 기준은 아래 두 조건을 만족해야 합니다.

- startup은 **cache-first / DB-recovery-only** 원칙으로 통일할 것
- resolver는 **soft budget + minDetectedAge + failure backoff + keyset pagination**을 갖출 것

이 두 축이 들어가면, 현재 코드베이스는 구조적으로 훨씬 안정된 상태가 됩니다.
