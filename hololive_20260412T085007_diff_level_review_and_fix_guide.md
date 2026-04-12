# hololive-bot-full-20260412T085007Z diff-level 리뷰 및 수정 가이드

## 1. 결론

이번 추가 수정본은 이전 라운드에서 가장 컸던 문제였던 다음 네 가지를 제대로 정리했습니다.

1. 알림 대상 12채널인데 111채널 전체를 5개 poller로 계속 긁던 구조를 explicit target registration으로 고쳤습니다.
2. outbox 30초 polling, scheduler 1초 coarse tick, telemetry hot path 같은 큰 지연 경로는 이미 정리된 상태를 유지하고 있습니다.
3. shorts/community poller hot path에서 `ResolveVideoPublishedAt` / `ResolveCommunityPostPublishedAt`를 직접 호출하던 추가 HTML fetch를 제거했습니다.
4. stream-ingester가 startup 때 shared subscriber cache를 DB 기준으로 파괴적으로 rebuild하던 위험한 경로를 제거하고, startup source of truth를 DB로 고정하되 cache는 관측만 하도록 바꿨습니다.

즉, 과거의 주병목은 대부분 해소됐습니다. 지금 남은 핵심은 “느린 코드”가 아니라 다음 세 축입니다.

- `PendingPublishedAtResolver`가 같은 YouTube request budget을 공유하면서 scheduler 밖에서 돌아 실제 감지 poller에 jitter를 줄 수 있는 구조
- `published_at_retry_after`를 도입했지만 migration 057이 적용되지 않아도 조용히 degrade되는 schema soft-dependency
- resolver가 `retry_after`를 finalization 전에 지워서 finalize 실패 시 같은 YouTube fetch를 다시 반복할 수 있는 순서 문제

이 문서는 바로 수정 가능한 수준으로, 파일별 원인, 왜 문제인지, 정확한 diff 방향, 테스트 추가 목록, 배포 검증 체크리스트까지 포함합니다.

---

## 2. 이번 버전에서 이미 잘 고친 점

### 2.1 stream-ingester startup source of truth 정리

`hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go`

- `resolveYouTubePollTargets(...)`는 startup 시 DB의 alarm channel IDs를 authoritative source로 사용합니다.
- cache는 `logYouTubePollTargetStartupSourceState(...)`를 통해 divergence를 관측만 합니다.
- 이전 라운드에서 문제였던 “stream-ingester가 kakao-bot의 cache-first/DB-async write 모델을 무시하고 shared subscriber cache를 강제로 rebuild”하는 경로는 제거됐습니다.

이 방향은 맞습니다. 유지해야 합니다.

### 2.2 hot path published_at fetch 제거

`hololive-shared/pkg/service/youtube/poller/pollers.go`

- ShortsPoller/CommunityPoller는 더 이상 `ResolveVideoPublishedAt()` / `ResolveCommunityPostPublishedAt()`를 hot path에서 호출하지 않습니다.
- scraped `PublishedAt`이 이미 있으면 즉시 enqueue 가능 여부를 판단하고, 없으면 tracking/source/alarm_state만 저장한 뒤 resolver에 맡깁니다.

이 변경은 매우 중요합니다. 과거에는 같은 신규 short/post를 감지할 때 추가 HTML fetch가 hot path에 붙어 rate budget과 tail latency를 동시에 악화시켰습니다. 지금 구조가 맞습니다.

### 2.3 worktree/root drift 이슈 해소

이 번들에는 이전 라운드의 `.worktrees/...` 구조가 보이지 않습니다. 실제 배포 코드와 리뷰 대상 코드가 분리되어 있던 P0 리스크는 이번 번들에서는 해소된 것으로 보입니다.

---

## 3. 남은 핵심 이슈 요약

### P0. resolver finalize 실패 시 같은 YouTube fetch를 반복할 수 있음

현재 `PendingPublishedAtResolver.runOnce()`는 다음 순서로 동작합니다.

1. YouTube에서 `published_at` 조회 성공
2. `tracking.ClearPublishedAtRetryAfter(...)`
3. `repo.FinalizePublishedAtAndMaybeEnqueue(...)`

문제는 3번이 실패하면 2번에서 이미 retry gate를 풀어버렸다는 점입니다. 따라서 다음 resolver cycle에서 같은 candidate가 다시 retryable 상태가 되고, 같은 YouTube page fetch를 반복할 수 있습니다.

이건 남은 I/O 병목 중 가장 실질적인 문제입니다. 특히 DB 일시 장애, row not found, transaction rollback, outbox insert 실패가 있으면 resolver가 같은 external fetch를 다시 태워버립니다.

### P0. migration 057이 적용되지 않아도 서비스가 조용히 기동됨

`tracking/alarm_state_repository.go`는 `hasPublishedAtRetryAfterColumn(db)`를 매 호출마다 확인하고, 컬럼이 없으면 `published_at_retry_after` 관련 동작을 조용히 skip합니다.

즉 migration 057이 빠진 상태에서도 서비스는 뜨지만, resolver backoff가 사실상 꺼집니다. 그러면 같은 pending candidate를 run마다 다시 긁게 되어 request budget을 빠르게 소모합니다.

이건 운영상 매우 위험합니다. startup에서 fail-fast 해야 합니다.

### P1. resolver가 scheduler 밖에서 shared YouTube budget을 경쟁적으로 소비함

resolver는 `sharedScraperClient`를 그대로 사용합니다. 즉 detection poller와 같은 rate limiter를 공유합니다. 그런데 resolver는 scheduler 관리 대상이 아니고 별도 goroutine loop로 동작합니다.

현재 하드코딩 값은 다음과 같습니다.

- interval: 10초
- batchSize: 20
- maxResolvePerRun: 2
- maxRunDuration: 6초
- minDetectedAge: 20초
- failureBackoffTTL: 5분

`Resolve*PublishedAt()` 한 번이 rate limiter 기준으로 3초/request budget을 소비하는 구조라면, backlog가 있을 때 resolver는 분당 최대 12회 수준까지 external fetch를 소비할 수 있습니다. 현재 steady-state detection poller가 대략 3.11 RPM 수준인 점을 감안하면, resolver가 detection보다 훨씬 큰 비율로 budget을 가져갈 수 있다는 뜻입니다.

이건 다시 유튜브 알림 지연으로 이어질 수 있습니다.

### P1. runtime poll-target refresher가 startup snapshot의 operational channel 목록에 묶여 있음

`youTubePollTargetRefresher`는 생성 시점의 `operationalChannels []communityShortsOperationalChannel`를 그대로 들고 갑니다. runtime refresh는 alarm channel IDs만 다시 읽고, operational channel roster 자체는 다시 읽지 않습니다.

즉 멤버 roster가 바뀌거나 새 채널이 운영 대상에 편입되어도, refresh로는 반영되지 않고 재기동이 필요합니다. 지금은 target selection이 많이 좋아졌지만, operational roster가 정적 snapshot인 점은 남아 있는 구조적 냄새입니다.

### P2. `hasPublishedAtRetryAfterColumn()`의 반복 metadata I/O

지금은 `ListPendingPublishedAtResolutionsPage`, `MarkPublishedAtRetryAfter`, `ClearPublishedAtRetryAfter`가 모두 `db.Migrator().HasColumn(...)`을 호출합니다. migration fail-fast를 넣으면 이 반복 schema probe는 더 이상 필요 없습니다. 남겨두면 metadata I/O와 코드 복잡도만 남습니다.

---

## 4. 파일별 분석

### 4.1 `hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go`

현재 상태는 대체로 맞습니다.

좋은 점:

- `resolveYouTubePollTargets(...)`로 startup source를 DB로 고정
- `sharedScraperClient`를 scheduler/poller/resolver가 공유
- subscriber cache는 rebuild하지 않고 관측만 수행

남은 문제:

- resolver 관련 schema requirement 검증이 없음
- resolver control 값이 전부 하드코딩
- `newYouTubePollTargetRefresher(...)`가 operational channel loader가 아니라 startup snapshot을 받음

### 4.2 `hololive-stream-ingester/internal/runtime/youtube_poll_targets.go`

좋은 점:

- startup source가 DB authoritative
- cache divergence를 관측만 하는 구조

남은 문제:

- runtime refresher와 공유되는 operational channel source가 정적 slice임

### 4.3 `hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh.go`

좋은 점:

- empty cache grace
- cache-only additions grace
- DB validation을 통한 shrink/addition 확인
- notification target은 `ForceImmediateFirstRun = true`

남은 문제:

- `operationalChannels`가 startup snapshot
- 새 멤버 roster를 runtime에 반영하지 못함

### 4.4 `hololive-shared/pkg/service/youtube/poller/published_at_resolver.go`

좋은 점:

- hot path에서 빠진 published_at resolution을 backlog resolver로 분리
- DB-visible `published_at_retry_after` 도입
- `maxRunDuration` 도입

남은 문제:

- `ClearPublishedAtRetryAfter`가 finalize 전에 호출됨
- resolver throughput 제어가 config가 아니라 magic number
- detection poller와 같은 request budget을 scheduler 밖에서 경쟁적으로 소비

### 4.5 `hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go`

좋은 점:

- finalize 시 DB update / claim / outbox insert가 transaction으로 묶여 있음

남은 문제:

- retry_after clear가 transaction 안에 포함되어 있지 않음
- finalize 실패에 대한 backoff 기록이 repository layer와 분리되어 있어 순서 오류가 나기 쉬움

### 4.6 `hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go`

좋은 점:

- pending candidate query가 `published_at_retry_after <= now`를 지원
- mark/clear 메서드 분리

남은 문제:

- `hasPublishedAtRetryAfterColumn()`를 매번 호출
- migration 없을 때 silent degrade

### 4.7 `hololive-shared/pkg/service/youtube/poller/pollers.go`

좋은 점:

- shorts/community hot path에서 published_at 추가 fetch 제거
- immediate enqueue는 scraped `PublishedAt`이 이미 있는 경우만 수행

이 파일은 이번 라운드에서 유지 방향이 맞습니다. 되돌리면 안 됩니다.

---

## 5. 최우선 수정안 1: migration 057 fail-fast + schema capability 캐싱

### 문제

`published_at_retry_after` 컬럼이 없으면 resolver backoff가 조용히 꺼집니다. 현재는 각 repository 메서드가 `HasColumn`으로 이를 감지하고 조용히 skip합니다. 운영에서는 “문제없이 기동했는데 실제로는 retry_after가 전혀 작동하지 않는 상태”가 됩니다.

### 수정 목표

1. stream-ingester startup에서 migration 057 미적용이면 즉시 실패
2. fail-fast 이후 runtime hot path에서 `HasColumn` 반복 호출 제거
3. repository는 `published_at_retry_after` 사용 가능하다는 전제를 갖고 단순화

### 권장 diff

#### 5.1 runtime startup에 schema validation 추가

파일: `hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go`

```diff
@@
 		sharedScraperClient := buildSharedYouTubeScraperClient(cfg.Scraper, infra.cacheService, infra.sharedRL)
+		if err := validatePublishedAtResolverSchema(ctx, infra.postgresService); err != nil {
+			infra.cleanup()
+			return nil, fmt.Errorf("validate published_at resolver schema: %w", err)
+		}
 		scraperScheduler, outboxDispatcher, pollerRegistrations, err = buildStreamIngesterYouTubeComponents(
```

파일 신규: `hololive-stream-ingester/internal/runtime/published_at_resolver_schema.go`

```diff
+package runtime
+
+import (
+	"context"
+	"fmt"
+
+	"github.com/kapu/hololive-shared/pkg/domain"
+	"github.com/kapu/hololive-shared/pkg/service/database"
+)
+
+func validatePublishedAtResolverSchema(ctx context.Context, postgresService database.Client) error {
+	if postgresService == nil {
+		return fmt.Errorf("postgres service is nil")
+	}
+	db := postgresService.GetGormDB()
+	if db == nil || db.Migrator() == nil {
+		return fmt.Errorf("gorm db or migrator is nil")
+	}
+	if !db.WithContext(ctx).Migrator().HasColumn(&domain.YouTubeCommunityShortsAlarmState{}, "published_at_retry_after") {
+		return fmt.Errorf("missing migration 057: youtube_community_shorts_alarm_states.published_at_retry_after")
+	}
+	return nil
+}
```

#### 5.2 repository의 schema capability를 생성 시점에 고정

파일: `hololive-shared/pkg/service/youtube/tracking/repository.go`

```diff
 type GormRepository struct {
-	db *gorm.DB
+	db                        *gorm.DB
+	hasPublishedAtRetryAfter bool
 }
 
 func NewRepository(db *gorm.DB) *GormRepository {
-	return &GormRepository{db: db}
+	return &GormRepository{
+		db:                        db,
+		hasPublishedAtRetryAfter: hasPublishedAtRetryAfterColumn(db),
+	}
 }
```

파일: `hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go`

```diff
-	if hasPublishedAtRetryAfterColumn(r.db) {
+	if r.hasPublishedAtRetryAfter {
 		query = query.
 			Select("kind, post_id, content_id, channel_id, detected_at, published_at_retry_after").
 			Where("(published_at_retry_after IS NULL OR published_at_retry_after <= ?)", yttimestamp.Normalize(referenceNow))
 	} else {
@@
-	if !hasPublishedAtRetryAfterColumn(r.db) {
+	if !r.hasPublishedAtRetryAfter {
 		return nil
 	}
@@
-	if !hasPublishedAtRetryAfterColumn(r.db) {
+	if !r.hasPublishedAtRetryAfter {
 		return nil
 	}
```

### 테스트 추가

- `hololive-stream-ingester/internal/runtime/published_at_resolver_schema_test.go`
  - migration 컬럼이 있으면 통과
  - 컬럼이 없으면 startup error
- `hololive-shared/pkg/service/youtube/tracking/alarm_state_repository_test.go`
  - repository 생성 시 capability가 고정되는지 확인

---

## 6. 최우선 수정안 2: resolver의 `retry_after` clear 순서 수정

### 문제

현재 resolver는 성공적으로 `published_at`를 얻은 직후 `ClearPublishedAtRetryAfter()`를 먼저 호출하고, 그 다음 `FinalizePublishedAtAndMaybeEnqueue()`를 호출합니다.

```go
publishedAt, err := r.resolveCandidatePublishedAt(...)
...
_ = tracking.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID)
result, err := repo.FinalizePublishedAtAndMaybeEnqueue(...)
```

이 순서는 틀렸습니다. finalize가 실패하면 다음 run에서 같은 external fetch를 다시 하게 됩니다.

### 수정 목표

1. retry_after clear는 finalize 성공 후에만 일어나야 함
2. finalize 실패 시에는 별도 `finalizeFailureBackoff`를 기록해야 함
3. 가능하면 clear까지 transaction에 포함해 atomic하게 처리

### 권장 diff A: 최소 수정안

파일: `hololive-shared/pkg/service/youtube/poller/published_at_resolver.go`

```diff
@@
-			_ = tracking.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID)
 			observePublishedAtResolutionSuccess(candidate.Kind)
 
 			result, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, candidate, *publishedAt, r.routeDecider)
 			if err != nil {
+				_ = tracking.MarkPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID, time.Now().Add(failureBackoffTTL))
 				r.logger.Warn("Pending published_at resolver failed to finalize candidate",
 					slog.String("kind", string(candidate.Kind)),
 					slog.String("post_id", candidate.PostID),
 					slog.String("content_id", candidate.ContentID),
 					slog.Any("error", err),
 				)
 				continue
 			}
+			_ = tracking.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID)
 			if result.enqueued {
```

이 수정만으로도 “finalize 실패 후 즉시 동일 fetch 재시도”는 막을 수 있습니다.

### 권장 diff B: 더 좋은 수정안

더 좋은 방법은 clear 자체를 finalize transaction 안으로 넣는 것입니다.

파일: `hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go`

```diff
-func (r *publishedAtResolverRepository) FinalizePublishedAtAndMaybeEnqueue(
+func (r *publishedAtResolverRepository) FinalizePublishedAtAndMaybeEnqueue(
 	ctx context.Context,
 	candidate trackingrepo.PublishedAtResolutionCandidate,
 	publishedAt time.Time,
 	routeDecider NotificationRouteDecider,
 ) (publishedAtFinalizeResult, error) {
@@
 	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
 		txRepo := trackingrepo.NewRepository(tx)
@@
+		if err := txRepo.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID); err != nil {
+			return fmt.Errorf("clear published_at retry after: %w", err)
+		}
+
 		notification, reason, err := r.finalizeCandidateState(ctx, tx, txRepo, candidate, normalizedPublishedAt, routeDecider)
```

그리고 resolver는 clear 호출을 완전히 제거합니다.

```diff
-			_ = tracking.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID)
 			observePublishedAtResolutionSuccess(candidate.Kind)
```

### 테스트 추가

- `hololive-shared/pkg/service/youtube/poller/published_at_resolver_test.go`
  - finalize 실패 시 `published_at_retry_after`가 미래 시각으로 설정되는지
  - finalize 성공 시에만 `published_at_retry_after`가 clear되는지
- `hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository_test.go`
  - transaction 실패 시 retry_after clear가 rollback되는지

---

## 7. 우선 수정안 3: resolver를 config화하고 detection budget을 침범하지 않게 제한

### 문제

현재 resolver 제어값은 다음처럼 전부 하드코딩입니다.

파일: `hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go`

```go
return poller.NewPendingPublishedAtResolverWithControls(
    postgresService.GetGormDB(),
    scraperClient,
    routeDecider,
    10*time.Second,
    20,
    2,
    6*time.Second,
    20*time.Second,
    5*time.Minute,
    logger,
)
```

문제는 이것이 detection poller와 동일한 shared scraper budget을 scheduler 밖에서 소비한다는 점입니다. backlog가 쌓이면 resolver가 detection보다 더 공격적으로 slot을 가져갈 수 있습니다.

### 수정 목표

1. magic number 제거
2. env/config로 resolver throughput을 운영에서 조절 가능하게 함
3. 기본값을 detection에 우호적으로 더 보수적으로 설정

### 권장 기본값

초기 기본값은 아래 정도가 안전합니다.

- interval: 15초
- batchSize: 10
- maxResolvePerRun: 1
- maxRunDuration: 2초
- minDetectedAge: 30초
- failureBackoffTTL: 5분

### 권장 diff

#### 7.1 config type 확장

파일: `hololive-shared/pkg/config/config_types.go`

```diff
 type ScraperConfig struct {
 	ProxyEnabled bool
 	ProxyURL     string
 	WorkerCount  int
 	Poll         ScraperPoll
+	PublishedAtResolver ScraperPublishedAtResolverConfig
 }
+
+type ScraperPublishedAtResolverConfig struct {
+	Enabled           bool
+	Interval          time.Duration
+	BatchSize         int
+	MaxResolvePerRun  int
+	MaxRunDuration    time.Duration
+	MinDetectedAge    time.Duration
+	FailureBackoffTTL time.Duration
+}
```

#### 7.2 config loading 추가

파일: `hololive-shared/pkg/config/config.go`

```diff
 		Scraper: ScraperConfig{
 			ProxyEnabled: sharedenv.Bool("SCRAPER_PROXY_ENABLED", false),
 			ProxyURL:     sharedenv.String("SCRAPER_PROXY_URL", ""),
 			WorkerCount: intAliasEnv([]string{
 				"SCRAPER_SCHEDULER_WORKER_COUNT",
 				"SCRAPER_WORKER_COUNT",
 			}, DefaultScraperWorkerCount()),
 			Poll: loadScraperPoll(),
+			PublishedAtResolver: ScraperPublishedAtResolverConfig{
+				Enabled:           sharedenv.Bool("SCRAPER_PUBLISHED_AT_RESOLVER_ENABLED", true),
+				Interval:          time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_INTERVAL_SECONDS", 15)) * time.Second,
+				BatchSize:         sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_BATCH_SIZE", 10),
+				MaxResolvePerRun:  sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RESOLVE_PER_RUN", 1),
+				MaxRunDuration:    time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RUN_DURATION_SECONDS", 2)) * time.Second,
+				MinDetectedAge:    time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_MIN_DETECTED_AGE_SECONDS", 30)) * time.Second,
+				FailureBackoffTTL: time.Duration(sharedenv.Int("SCRAPER_PUBLISHED_AT_RESOLVER_FAILURE_BACKOFF_SECONDS", 300)) * time.Second,
+			},
 		},
```

#### 7.3 builder 반영

파일: `hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go`

```diff
 func buildPendingPublishedAtResolver(
+	scraperCfg config.ScraperConfig,
 	postgresService database.Client,
 	scraperClient *scraper.Client,
 	routeDecider poller.NotificationRouteDecider,
 	logger *slog.Logger,
 ) *poller.PendingPublishedAtResolver {
 	if postgresService == nil || scraperClient == nil {
 		return nil
 	}
+	if !scraperCfg.PublishedAtResolver.Enabled {
+		return nil
+	}
+	resolverCfg := scraperCfg.PublishedAtResolver
 
 	return poller.NewPendingPublishedAtResolverWithControls(
 		postgresService.GetGormDB(),
 		scraperClient,
 		routeDecider,
-		10*time.Second,
-		20,
-		2,
-		6*time.Second,
-		20*time.Second,
-		5*time.Minute,
+		resolverCfg.Interval,
+		resolverCfg.BatchSize,
+		resolverCfg.MaxResolvePerRun,
+		resolverCfg.MaxRunDuration,
+		resolverCfg.MinDetectedAge,
+		resolverCfg.FailureBackoffTTL,
 		logger,
 	)
 }
```

호출부도 수정합니다.

파일: `hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go`

```diff
-			publishedAtResolver = buildPendingPublishedAtResolver(
+			publishedAtResolver = buildPendingPublishedAtResolver(
+				cfg.Scraper,
 				infra.postgresService,
 				sharedScraperClient,
 				routeDecider,
 				logger,
 			)
```

### 추가 권장 사항

장기적으로는 resolver를 scheduler-managed maintenance job으로 편입시키는 것이 가장 좋습니다. 다만 현재 구조에서 diff 크기를 최소화하려면 config화 + 보수적 기본값이 먼저입니다.

### 테스트 추가

- `hololive-shared/pkg/config/config_test.go`
  - 새 env 변수 parsing 검증
- `hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers_test.go`
  - resolver enabled/disabled 케이스

---

## 8. 우선 수정안 4: runtime poll target refresher에 operational channel loader 주입

### 문제

현재 `youTubePollTargetRefresher`는 startup 시점의 `operationalChannels []communityShortsOperationalChannel`를 복사해 들고 있습니다.

```go
type youTubePollTargetRefresher struct {
    ...
    operationalChannels []communityShortsOperationalChannel
}
```

그리고 refresh 때마다 이 정적 slice와 현재 alarm channel IDs를 교차시킵니다. 즉 alarm set은 runtime에 바뀌어도 operational roster는 runtime에 바뀌지 않습니다.

### 수정 목표

1. alarm set뿐 아니라 operational roster도 runtime refresh 가능하게 함
2. load 실패 시 마지막 정상 roster를 유지
3. refresh 동작을 startup snapshot에 덜 의존하게 만듦

### 권장 diff

파일: `hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh.go`

```diff
 type youTubePollTargetRefresher struct {
 	cacheService        cache.Client
 	scheduler           *poller.Scheduler
 	registrations       []providers.ChannelPollerRegistration
-	operationalChannels []communityShortsOperationalChannel
+	loadOperationalChannels func(context.Context) ([]communityShortsOperationalChannel, error)
+	lastOperationalChannels []communityShortsOperationalChannel
 	loadAlarmChannelIDs func(context.Context) ([]string, error)
@@
 func newYouTubePollTargetRefresher(
 	cacheService cache.Client,
 	scheduler *poller.Scheduler,
 	registrations []providers.ChannelPollerRegistration,
-	operationalChannels []communityShortsOperationalChannel,
+	loadOperationalChannels func(context.Context) ([]communityShortsOperationalChannel, error),
 	loadAlarmChannelIDs func(context.Context) ([]string, error),
 	logger *slog.Logger,
 ) *youTubePollTargetRefresher {
-	if cacheService == nil || scheduler == nil || len(registrations) == 0 || loadAlarmChannelIDs == nil {
+	if cacheService == nil || scheduler == nil || len(registrations) == 0 || loadAlarmChannelIDs == nil || loadOperationalChannels == nil {
 		return nil
 	}
@@
-		operationalChannels: append([]communityShortsOperationalChannel(nil), operationalChannels...),
+		loadOperationalChannels: loadOperationalChannels,
+		lastOperationalChannels: nil,
```

`refresh()` 초반에 roster refresh를 추가합니다.

```diff
+	operationalChannels, err := r.loadOperationalChannels(ctx)
+	if err != nil {
+		if len(r.lastOperationalChannels) == 0 {
+			if r.logger != nil {
+				r.logger.Warn("Failed to refresh operational channels for YouTube poll targets",
+					slog.Any("error", err))
+			}
+			return
+		}
+		operationalChannels = append([]communityShortsOperationalChannel(nil), r.lastOperationalChannels...)
+	} else {
+		r.lastOperationalChannels = append([]communityShortsOperationalChannel(nil), operationalChannels...)
+	}
```

기존 `resolveYouTubePollTargetsFromAlarmChannelIDs(..., r.operationalChannels)`는 모두 `operationalChannels` 변수로 교체합니다.

호출부도 수정합니다.

파일: `hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go`

```diff
 			pollTargetRefresher = newYouTubePollTargetRefresher(
 				infra.cacheService,
 				scraperScheduler,
 				pollerRegistrations,
-				operationalChannels,
+				func(ctx context.Context) ([]communityShortsOperationalChannel, error) {
+					return resolveCommunityShortsOperationalChannels(infra.membersData)
+				},
 				func(ctx context.Context) ([]string, error) {
 					return loadAlarmChannelIDs(ctx, infra.postgresService)
 				},
 				logger,
 			)
```

### 테스트 추가

- `hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh_test.go`
  - operational roster가 runtime에 바뀌면 stats target이 갱신되는지
  - roster load 실패 시 마지막 정상값을 유지하는지

---

## 9. 선택 수정안 5: resolver를 detection scheduler budget과 더 분리하는 방향

이 항목은 바로 필수는 아니지만, 장기적으로 가장 깔끔한 방향입니다.

### 현재 문제

- resolver는 `sharedScraperClient`와 같은 rate limiter를 사용
- scheduler가 아닌 별도 loop라 due live/video poller와 공정하게 arbitration되지 않음
- 지금은 config화만 해도 개선되지만, 구조 자체는 maintenance traffic이 detection traffic을 침범할 수 있음

### 개선 방향 A: scheduler-managed maintenance poller

가장 좋은 구조는 resolver를 scheduler에 들어가는 “maintenance poller”처럼 취급하는 것입니다. 예를 들면 channelless job 혹은 special channel group을 지원해서 scheduler가 동일한 dispatch, priority, worker backpressure 규칙을 적용하게 만드는 것입니다.

이건 diff 폭이 크므로 이번 문서에서는 설계 방향만 제시합니다.

### 개선 방향 B: resolver 내부에 request budget cap 추가

현실적인 중간안은 resolver가 run마다 최대 1개 또는 2개만 resolve하도록 하고, detection poller의 expected RPM 합계와 resolver의 max RPM을 로그/metric으로 같이 노출하는 것입니다.

`published_at_resolver_started` 로그에 아래 값을 남기는 것을 권장합니다.

- interval
- batch_size
- max_resolve_per_run
- max_run_duration
- failure_backoff_ttl
- estimated_max_rpm

---

## 10. 선택 수정안 6: production에서 사용하지 않는 `RebuildSubscriberCacheFromRepository` 정리

현재 `hololive-shared/pkg/service/alarm/cache_warm.go`에는 `RebuildSubscriberCacheFromRepository()`와 관련 테스트가 남아 있습니다. 이 함수 자체가 나쁜 것은 아니지만, 이번 버전의 runtime 설계에서는 production path에서 사용하지 않습니다.

이 상태는 두 가지 리스크를 만듭니다.

1. 향후 누군가 startup correctness 문제를 보고 이 함수를 다시 호출하도록 연결할 수 있음
2. “warm”과 “rebuild” semantics가 같은 패키지 안에 섞여 있어 이해 비용이 커짐

권장 방향은 둘 중 하나입니다.

- maintenance/admin 전용 함수로 명확히 이름과 주석을 바꿔 두고 runtime에서는 절대 사용하지 않는다고 명시
- 운영에서 쓸 계획이 없다면 제거

지금 당장 P0는 아닙니다. 다만 AI 냄새 관점에서는 “실제로는 쓰지 않는 위험한 대안 경로가 남아 있는 상태”이므로 정리해 두는 편이 좋습니다.

---

## 11. 테스트 보강 목록

이번 버전에서 반드시 추가하거나 보강해야 할 테스트는 아래와 같습니다.

### 11.1 resolver / retry_after

파일: `hololive-shared/pkg/service/youtube/poller/published_at_resolver_test.go`

추가 케이스:

- `FinalizePublishedAtAndMaybeEnqueue` 실패 시 `published_at_retry_after`가 미래 시각으로 설정되는지
- finalize 성공 시 `published_at_retry_after`가 clear되는지
- migration 057이 없을 때 runtime startup이 실패하는지

### 11.2 repository transaction 경계

파일: `hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository_test.go`

추가 케이스:

- outbox insert 실패 시 transaction rollback으로 `actual_published_at`, `authorized_at`, `published_at_retry_after`가 일관되게 유지되는지

### 11.3 poll target refresher operational roster

파일: `hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh_test.go`

추가 케이스:

- runtime 중 operational roster가 바뀌면 stats target이 바뀌는지
- roster load error 시 마지막 정상 roster를 유지하는지

### 11.4 config parsing

파일: `hololive-shared/pkg/config/config_test.go`

추가 케이스:

- `SCRAPER_PUBLISHED_AT_RESOLVER_*` env parsing 검증

### 11.5 startup schema validation

파일 신규: `hololive-stream-ingester/internal/runtime/published_at_resolver_schema_test.go`

추가 케이스:

- 컬럼 존재 시 pass
- 컬럼 미존재 시 error message 확인

---

## 12. 배포 순서

권장 적용 순서는 아래와 같습니다.

1. migration 057 적용
2. startup schema validation 추가
3. resolver retry_after clear 순서 수정
4. resolver config화 및 기본값 보수화
5. runtime poll target refresher에 operational channel loader 주입
6. 선택적으로 unused rebuild path 정리

이 순서를 지켜야 하는 이유는, 1과 2가 없으면 3과 4를 넣어도 운영에서 조용히 degrade될 수 있기 때문입니다.

---

## 13. 배포 후 확인할 로그와 메트릭

### 반드시 확인할 로그

- `Resolved YouTube poll targets`
  - `notification_target_channels`
  - `stats_target_channels`
  - `dropped_alarm_targets`
- `youtube_poll_targets_startup_source_aligned` 또는 `youtube_poll_targets_startup_source_diverged`
- `subscriber_cache_observed_on_youtube_startup`
- `Pending published_at resolver started`
- `published_at_resolver_enqueued`
- `published_at_resolver_enqueue_skipped`
- `Pending published_at resolver failed to finalize candidate`

### 새로 추가 권장 로그

- `published_at_resolver_configured`
  - interval
  - batch_size
  - max_resolve_per_run
  - max_run_duration
  - failure_backoff_ttl
  - estimated_max_rpm
- `published_at_resolver_schema_validated`
- `youtube_poll_target_operational_channels_refreshed`
  - operational_channel_count
  - notification_target_channels
  - stats_target_channels

### 반드시 확인할 메트릭

- resolver attempt/success/failure/enqueued/skipped 카운터
- finalize failure 카운터
- `published_at_retry_after`가 현재 시각보다 미래인 pending row 개수
- scheduler due lag / worker saturation
- scraper rate limiter wait time

특히 배포 직후에는 아래 두 현상을 봐야 합니다.

1. detection poller 지연 없이 live/video 감지가 유지되는지
2. community/shorts backlog가 resolver에 의해 천천히 줄어들되, detection poller의 due lag를 키우지 않는지

---

## 14. 최종 평가

이번 추가 수정본은 이전 문제의 본질을 상당히 잘 고친 상태입니다. 특히 아래는 되돌리면 안 됩니다.

- DB-authoritative startup poll target resolution
- runtime cache refresh + DB validation
- shorts/community hot path에서 `Resolve*PublishedAt` 제거
- stream-ingester startup에서 shared subscriber cache rebuild 제거

지금 남은 핵심은 세 가지입니다.

1. `published_at_retry_after`를 진짜로 운영 안전장치로 만들 것인가
2. resolver finalize 실패 시 YouTube budget 재소모 루프를 막을 것인가
3. resolver가 detection poller budget을 침범하지 않게 운영 제어권을 줄 것인가

따라서 다음 패치의 우선순위는 다음 순서가 맞습니다.

- **P0:** migration 057 fail-fast
- **P0:** finalize 전에 retry_after를 clear하는 순서 수정
- **P1:** resolver config화 및 보수적 기본값
- **P1:** poll target refresher의 operational roster runtime refresh
- **P2:** unused rebuild path 정리

---

## 15. 검토 범위와 한계

이번 문서는 `hololive-bot-full-20260412T085007Z.tar.gz` 기준 정적 리뷰 결과입니다. 현재 환경에서는 저장소가 `go 1.26.2`를 요구하지만 실행 환경의 Go toolchain 다운로드가 막혀 있어 테스트를 실제로 돌리지는 못했습니다. 따라서 이 문서는 코드 경로, 데이터 흐름, migration 구조, 기존 테스트 의도를 기준으로 한 diff-level 수정 가이드입니다.
