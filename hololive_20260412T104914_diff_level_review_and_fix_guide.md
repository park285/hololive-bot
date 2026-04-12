# hololive-bot-full-20260412T104914Z 추가 수정본 diff 레벨 리뷰 및 수정 가이드

## 0. 전제와 결론

이번 번들은 이전 단계에서 남아 있던 큰 병목을 많이 정리했습니다. 특히 아래 두 축은 분명히 좋아졌습니다.

- `videos / shorts / community / live` 와 `channel_stats` 타깃 분리가 유지되고 있습니다.
- `PendingPublishedAtResolver`가 `poller` hot path 밖으로 분리되어, shorts/community 감지 시점의 추가 HTML fetch가 빠졌습니다.
- `published_at_retry_after`를 DB-visible 상태로 관리하면서 같은 콘텐츠를 너무 짧은 주기로 다시 긁는 문제를 줄이려는 방향이 반영되어 있습니다.
- worktree/root drift는 이번 번들에서는 사실상 사라졌고, 실제 코드 기준으로 리뷰해도 됩니다.

하지만 이번 버전에도 아직 **운영 리스크가 큰 P0/P1**이 남아 있습니다. 이번 문서에서 가장 중요한 결론은 아래 네 가지입니다.

### 가장 중요한 결론

1. **운영 채널 로더가 여전히 `member.ServiceAdapter`를 통해 members를 읽고 있고, 이 adapter는 repository 에러를 삼킨 뒤 빈 slice를 반환합니다.**  
   그 결과 startup과 runtime refresher가 **실패를 에러로 보지 않고 “운영 채널 0개”로 해석할 수 있습니다.** 이 상태가 실제로 발생하면 notification/stats 타깃이 모두 0으로 sync될 수 있습니다.

2. **`PublishedAtResolver.Enabled=false` 경로가 아직 완결되지 않았습니다.**  
   현재 bootstrap은 resolver schema를 무조건 검증하고 있고, 반대로 resolver를 정말 꺼버리면 shorts/community 중 `published_at`이 없는 콘텐츠는 **추적만 되고 알림은 영원히 enqueue되지 않을 수 있습니다.**

3. **scheduler RPM과 resolver RPM을 합친 “실제 YouTube request budget” 경고가 없습니다.**  
   지금은 poller budget만 계산하고 resolver는 별도 로그만 남깁니다. 그런데 둘은 **같은 scraper client / 같은 shared rate limiter**를 쓰므로, 설정이 틀어지면 resolver가 detection poller budget을 잡아먹고 알림 지연을 다시 키울 수 있습니다.

4. **resolver schema 검증이 column 하나만 보고 끝납니다.**  
   `published_at_retry_after` 컬럼만 확인하고, 실제로 backlog scan 성능을 지탱하는 `056`, `057` index는 확인하지 않습니다. 기능은 살아 있어도 backlog가 쌓이면 resolver query가 급격히 느려질 수 있습니다.

이 문서는 위 네 가지를 중심으로, **문서만 보고 바로 수정할 수 있도록** 파일별 변경 방향, unified diff 예시, 테스트 추가 포인트, 배포 순서를 전부 적었습니다.

---

## 1. 이번 버전에서 좋아진 점

이 부분은 먼저 인정하고 시작하는 것이 맞습니다. 지금 버전은 예전과 비교하면 구조가 많이 나아졌습니다.

### 1-1. 과다 스크래핑 구조는 유지되지 않고 있다

`hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go`에서 `notificationChannelIDs`와 `statsChannelIDs`가 분리되어 있고, `TargetGroup`도 명시되어 있습니다.  
즉 예전의 “12채널만 알림 대상인데 111채널 전체를 videos/shorts/community/live/stats 전부로 긁는 구조”는 지금 코드 기준으로는 재발하지 않습니다.

### 1-2. shorts/community hot path의 추가 fetch는 빠졌다

`hololive-shared/pkg/service/youtube/poller/pollers.go`를 보면, poller loop 안에서 더 이상 `ResolveVideoPublishedAt()` / `ResolveCommunityPostPublishedAt()`를 직접 호출하지 않습니다.  
이건 I/O 병목을 줄이는 올바른 방향입니다.

### 1-3. finalize 전에 retry_after를 clear하던 순서 문제는 정리되었다

`hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go`를 보면 `ClearPublishedAtRetryAfter()`는 finalize transaction 내부, 그리고 notification insert 이후에 실행됩니다.  
즉 이전 단계에서 걱정했던 “finalize 실패 시 같은 YouTube fetch를 반복하는 순서 문제”는 이번 버전에서 많이 개선됐습니다.

### 1-4. cache를 DB 기준으로 파괴적으로 rebuild하던 경로는 없어졌다

이전 버전에서 걱정했던 `stream-ingester` startup의 shared alarm cache destructive rebuild는 이번 버전에서는 빠졌습니다.  
이건 `kakao-bot`의 cache-first / DB-async write 모델과 충돌하지 않는 쪽으로 정리된 것입니다.

---

## 2. 남은 P0: 운영 채널 로더가 member repository 실패를 “빈 roster”로 오해한다

### 문제 요약

현재 startup과 runtime poll target refresher 모두 운영 채널 목록을 아래 경로로 읽습니다.

- `bootstrap_stream_ingester.go`
  - 초기 startup: `resolveCommunityShortsOperationalChannels(infra.membersData)`
  - refresher loader: `resolveCommunityShortsOperationalChannels(infra.membersData)`

그런데 `infra.membersData`의 실제 구현은 `member.ServiceAdapter`이고, 이 구현의 `GetAllMembers()`는 repository 에러를 그대로 올리지 않습니다.

### 실제 코드 근거

`hololive-shared/pkg/service/member/adapter.go`

```go
func (a *ServiceAdapter) GetAllMembers() []*domain.Member {
    if a == nil || a.cache == nil || a.cache.repo == nil {
        return []*domain.Member{}
    }
    members, err := a.cache.repo.GetAllMembers(a.ctx)
    if err != nil {
        a.logger.Warn("repository lookup failed in GetAllMembers", "error", err)
        return []*domain.Member{}
    }
    return members
}
```

즉 repository 장애가 나도 이 함수는 에러를 반환하지 않고 빈 slice를 반환합니다.

그리고 `hololive-stream-ingester/internal/runtime/channel_target_validation.go`의 `resolveCommunityShortsOperationalChannels(...)`는 이 빈 slice를 그대로 정상 입력으로 취급합니다.

그 결과 다음 두 가지 failure mode가 생깁니다.

### 실제 장애 모드

#### 장애 모드 A: startup 시 운영 채널 0개로 부팅

startup 시점에 member repository가 순간적으로 실패하면:

- `resolveCommunityShortsOperationalChannels(infra.membersData)`는 에러 없이 빈 channel set 반환
- `resolveYouTubePollTargets(...)`는 empty operational set 기준으로 target 계산
- 결과적으로 notification/stats 타깃이 0개가 될 수 있음
- 서비스는 “정상 부팅한 것처럼” 보이지만 실제 감지는 멈춤

#### 장애 모드 B: refresher가 기존 타깃을 0개로 내려버림

runtime 중 member repository가 잠깐 실패하면:

- `withOperationalChannelLoader(func(ctx) { return resolveCommunityShortsOperationalChannels(infra.membersData) })`
- loader는 에러를 주지 않고 빈 operational roster를 반환
- `resolveOperationalChannels()`는 fallback path로 가지 못함
- refresher는 이것을 정상 shrink로 해석하고 poll target을 0으로 sync할 수 있음

이건 단순 지연이 아니라 **감지 중단**입니다.

### 왜 AI 냄새인가

이건 전형적인 “interface는 오류 없는 조회처럼 보이지만, 실제로는 repository를 감싸면서 에러를 숨기는” 패턴입니다.

- `domain.MemberDataProvider`는 read-only 조회용으로는 적절합니다.
- 하지만 runtime control-plane에서 “운영 채널의 authoritative source”를 계산할 때는 부적절합니다.
- 지금은 **control-plane 결정**에 cache/adapter 추상화를 재사용하면서, 에러 semantics가 사라졌습니다.

즉 “도메인 읽기 인터페이스”와 “운영 결정용 authoritative read”가 섞여 있습니다.

### 수정 원칙

운영 채널 계산은 **반드시 에러를 반환하는 repository 경로**를 타야 합니다.

- startup initial resolution: repository 기반
- runtime refresher loader: repository 기반
- adapter / cache snapshot은 UI/검색/편의 조회에만 사용

### 수정 diff

#### 2-1. `stream_ingester_runtime_builder.go`에 `memberRepo`를 실어준다

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder.go b/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder.go
index 1111111..2222222 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder.go
@@
 type streamIngesterInfrastructure struct {
     cacheService     cache.Client
     postgresService  database.Client
+    memberRepo       *member.Repository
     membersData      member.DataProvider
     irisClient       iris.Sender
     settingsService  settings.ReadWriter
@@
     return &streamIngesterInfrastructure{
         cacheService:     infra.Cache,
         postgresService:  infra.Postgres,
+        memberRepo:       infra.MemberRepo,
         membersData:      membersData,
         irisClient:       irisClient,
         settingsService:  settingsService,
```

#### 2-2. `channel_target_validation.go`에 repository 기반 helper를 추가한다

핵심은 기존 함수를 완전히 없애는 것이 아니라, 내부 로직을 재사용 가능한 `fromMembers` helper로 분리하는 것입니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/channel_target_validation.go b/hololive/hololive-stream-ingester/internal/runtime/channel_target_validation.go
index 3333333..4444444 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/channel_target_validation.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/channel_target_validation.go
@@
 package runtime

 import (
+    "context"
     "fmt"
     "strings"

     "github.com/kapu/hololive-shared/pkg/domain"
     sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
+    "github.com/kapu/hololive-shared/pkg/service/member"
 )
@@
 func resolveCommunityShortsOperationalChannels(membersData domain.MemberDataProvider) ([]communityShortsOperationalChannel, error) {
     if membersData == nil {
         return nil, fmt.Errorf("members data provider is nil")
     }
-
-    members := membersData.GetAllMembers()
-    channels := make([]communityShortsOperationalChannel, 0, len(members))
-    seenChannelIDs := make(map[string]struct{}, len(members))
-    for i := range members {
-        member := members[i]
-        if member == nil || member.IsGraduated {
-            continue
-        }
-        channelID := strings.TrimSpace(member.ChannelID)
-        if channelID != "" {
-            if _, exists := seenChannelIDs[channelID]; exists {
-                continue
-            }
-            seenChannelIDs[channelID] = struct{}{}
-        }
-        channels = append(channels, communityShortsOperationalChannel{
-            ownerLabel: communityShortsTargetOwnerLabel(member),
-            channelID:  channelID,
-            enabled:    channelID != "",
-        })
-    }
-
-    return channels, nil
+    return resolveCommunityShortsOperationalChannelsFromMembers(membersData.GetAllMembers()), nil
+}
+
+func resolveCommunityShortsOperationalChannelsFromRepository(
+    ctx context.Context,
+    repo *member.Repository,
+) ([]communityShortsOperationalChannel, error) {
+    if repo == nil {
+        return nil, fmt.Errorf("member repository is nil")
+    }
+    members, err := repo.GetAllMembers(ctx)
+    if err != nil {
+        return nil, fmt.Errorf("load members from repository: %w", err)
+    }
+    return resolveCommunityShortsOperationalChannelsFromMembers(members), nil
+}
+
+func resolveCommunityShortsOperationalChannelsFromMembers(members []*domain.Member) []communityShortsOperationalChannel {
+    channels := make([]communityShortsOperationalChannel, 0, len(members))
+    seenChannelIDs := make(map[string]struct{}, len(members))
+    for i := range members {
+        member := members[i]
+        if member == nil || member.IsGraduated {
+            continue
+        }
+        channelID := strings.TrimSpace(member.ChannelID)
+        if channelID != "" {
+            if _, exists := seenChannelIDs[channelID]; exists {
+                continue
+            }
+            seenChannelIDs[channelID] = struct{}{}
+        }
+        channels = append(channels, communityShortsOperationalChannel{
+            ownerLabel: communityShortsTargetOwnerLabel(member),
+            channelID:  channelID,
+            enabled:    channelID != "",
+        })
+    }
+    return channels
 }
```

#### 2-3. bootstrap와 refresher loader를 repository 기반으로 바꾼다

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go b/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go
index 5555555..6666666 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go
@@
-        operationalChannels, err = resolveCommunityShortsOperationalChannels(infra.membersData)
+        operationalChannels, err = resolveCommunityShortsOperationalChannelsFromRepository(ctx, infra.memberRepo)
         if err != nil {
             infra.cleanup()
             return nil, fmt.Errorf("resolve community shorts operational channels: %w", err)
         }
@@
         pollTargetRefresher = newYouTubePollTargetRefresher(
@@
-        ).withOperationalChannelLoader(func(ctx context.Context) ([]communityShortsOperationalChannel, error) {
-            return resolveCommunityShortsOperationalChannels(infra.membersData)
+        ).withOperationalChannelLoader(func(ctx context.Context) ([]communityShortsOperationalChannel, error) {
+            return resolveCommunityShortsOperationalChannelsFromRepository(ctx, infra.memberRepo)
         })
```

### 테스트 추가

#### 반드시 추가할 테스트

1. `hololive-stream-ingester/internal/runtime/channel_target_validation_test.go`
   - `TestResolveCommunityShortsOperationalChannelsFromRepository_ReturnsErrorOnRepositoryFailure`
   - `TestResolveCommunityShortsOperationalChannelsFromRepository_SkipsGraduatedAndDedupesChannelIDs`

2. `hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh_test.go`
   - `TestYouTubePollTargetRefresher_OperationalChannelLoaderError_UsesLastKnownOperationalChannels`
   - `TestYouTubePollTargetRefresher_DoesNotShrinkTargetsToZeroOnMemberRepositoryFailure`

3. `hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_test.go`
   - `TestBuildStreamIngesterRuntime_FailsWhenOperationalChannelRepositoryLoadFails`

### 이 수정의 기대 효과

이 변경 하나로 runtime control-plane의 source of truth가 “cache/adapter의 조용한 empty”가 아니라 “repository success/error”로 돌아옵니다.  
즉 감지 지연 이슈보다 더 위험한 **무음 감지 중단**을 막을 수 있습니다.

---

## 3. 남은 P0/P1: `PublishedAtResolver.Enabled=false` 경로가 반쯤만 구현되어 있다

### 문제 요약

이번 버전은 `ScraperPublishedAtResolverConfig.Enabled`를 도입했습니다. 그런데 현재 구현은 두 방향이 서로 모순됩니다.

- `buildPendingPublishedAtResolver(...)`는 `Enabled=false`면 `nil`을 반환
- 그런데 bootstrap은 `validatePublishedAtResolverSchema(...)`를 **무조건** 호출
- 동시에 shorts/community poller는 `published_at`이 없으면 notification enqueue를 건너뜀
- 따라서 resolver를 정말 끄면 `published_at`이 없는 shorts/community 일부는 **영원히 알림이 안 감**

즉 현재 “resolver disable”은 완전한 rollback path가 아닙니다.

### 실제 코드 근거

`bootstrap_stream_ingester.go`

```go
sharedScraperClient := buildSharedYouTubeScraperClient(...)
if err := validatePublishedAtResolverSchema(ctx, infra.postgresService); err != nil { ... }
publishedAtResolver = buildPendingPublishedAtResolver(...)
```

`stream_ingester_runtime_builder_helpers.go`

```go
resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
if !resolverCfg.Enabled {
    return nil
}
```

`pollers.go`의 shorts/community 공통 패턴

```go
var routePublishedAt time.Time
if dbPost.PublishedAt != nil {
    routePublishedAt = *dbPost.PublishedAt
}
if routePublishedAt.IsZero() {
    continue
}
```

즉 `PublishedAtResolver`가 비활성화되면 `routePublishedAt`이 비어 있는 아이템은 outbox로 가지 않습니다.

### 왜 AI 냄새인가

이건 전형적인 “feature flag를 추가했지만, disable 시나리오의 의미를 끝까지 정의하지 않은 상태”입니다.

- `Enabled=false`가 “resolver 없이도 기존처럼 동작”인지
- `Enabled=false`가 “resolver가 필요한 콘텐츠는 알림을 포기”인지
- `Enabled=false`면 schema 검증도 건너뛰어야 하는지

이 세 가지가 현재 코드에서 서로 일치하지 않습니다.

### 올바른 의미 정의

이 플래그는 아래처럼 정의해야 합니다.

- `Enabled=true`
  - 현재 구조 유지
  - poller는 감지만 하고 missing `published_at`은 resolver가 처리
  - startup에서 schema 검증 필요

- `Enabled=false`
  - resolver loop는 띄우지 않음
  - startup schema 검증도 하지 않음
  - 대신 shorts/community poller는 **legacy inline resolve fallback**을 켜서 기존 의미를 보존

즉 `Enabled=false`는 “기능 비활성”이 아니라 “비동기 resolver 비활성 + 동기 fallback 유지”여야 합니다.

### 수정 diff

#### 3-1. bootstrap에서 schema 검증을 resolver enabled일 때만 수행한다

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go b/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go
index 6666666..7777777 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go
@@
         routeDecider := buildCommunityShortsRouteDecider(communityShortsPolicy)
         sharedScraperClient := buildSharedYouTubeScraperClient(cfg.Scraper, infra.cacheService, infra.sharedRL)
-        if err := validatePublishedAtResolverSchema(ctx, infra.postgresService); err != nil {
-            infra.cleanup()
-            return nil, fmt.Errorf("validate published_at resolver schema: %w", err)
-        }
-        logger.Info("published_at_resolver_schema_validated")
+        resolverCfg := effectivePublishedAtResolverConfig(cfg.Scraper)
+        if resolverCfg.Enabled {
+            if err := validatePublishedAtResolverSchema(ctx, infra.postgresService); err != nil {
+                infra.cleanup()
+                return nil, fmt.Errorf("validate published_at resolver schema: %w", err)
+            }
+            logger.Info("published_at_resolver_schema_validated")
+        }
```

#### 3-2. poller constructor에 `inlineResolveMissingPublishedAt` 플래그를 추가한다

`stream_ingester_poller_registrations.go`

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go b/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go
index 8888888..9999999 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go
@@
 func buildStreamIngesterChannelPollerRegistrationsWithClient(
@@
 ) []providers.ChannelPollerRegistration {
     poll := scraperCfg.PollOrDefault()
+    resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
+    inlineResolveMissingPublishedAt := !resolverCfg.Enabled
     communityKeywords := []string{}
     db := postgres.GetGormDB()

     videosPoller := poller.NewVideosPoller(scraperClient, db, 10)
-    shortsPoller := poller.NewShortsPoller(scraperClient, db, 10, routeDecider)
-    communityPoller := poller.NewCommunityPoller(scraperClient, db, 10, communityKeywords, routeDecider)
+    shortsPoller := poller.NewShortsPoller(scraperClient, db, 10, routeDecider, inlineResolveMissingPublishedAt)
+    communityPoller := poller.NewCommunityPoller(scraperClient, db, 10, communityKeywords, routeDecider, inlineResolveMissingPublishedAt)
     statsPoller := poller.NewChannelStatsPoller(scraperClient, db)
     livePoller := poller.NewLivePoller(scraperClient, db)
```

#### 3-3. shorts poller에 fallback inline resolve를 넣는다

`hololive-shared/pkg/service/youtube/poller/pollers.go`

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/poller/pollers.go b/hololive/hololive-shared/pkg/service/youtube/poller/pollers.go
index aaaaaaa..bbbbbbb 100644
--- a/hololive/hololive-shared/pkg/service/youtube/poller/pollers.go
+++ b/hololive/hololive-shared/pkg/service/youtube/poller/pollers.go
@@
 import (
     "context"
+    "errors"
     "fmt"
     "log/slog"
     "strings"
     "time"
@@
 type ShortsPoller struct {
     client       *scraper.Client
     db           *gorm.DB
     repo         batchRepository
     maxResults   int
     routeDecider NotificationRouteDecider
+    inlineResolveMissingPublishedAt bool
 }

-func NewShortsPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int, routeDecider NotificationRouteDecider) *ShortsPoller {
+func NewShortsPoller(
+    scraperClient *scraper.Client,
+    db *gorm.DB,
+    maxResults int,
+    routeDecider NotificationRouteDecider,
+    inlineResolveMissingPublishedAt bool,
+) *ShortsPoller {
@@
     return &ShortsPoller{
         client:       scraperClient,
         db:           db,
         repo:         newBatchRepository(db),
         maxResults:   maxResults,
         routeDecider: routeDecider,
+        inlineResolveMissingPublishedAt: inlineResolveMissingPublishedAt,
     }
 }
@@
         for _, short := range newShorts {
             canonicalPostID := normalizeContentID(domain.OutboxKindNewShort, short.VideoID)
             resourceVideoID := normalizeShortVideoResourceID(short.VideoID)
             publishedAt := yttimestamp.NormalizePtr(short.PublishedAt)
+            if isInitialized && publishedAt == nil && p.inlineResolveMissingPublishedAt {
+                resolvedPublishedAt, resolveErr := p.client.ResolveVideoPublishedAt(ctx, resourceVideoID)
+                switch {
+                case resolveErr == nil:
+                    publishedAt = yttimestamp.NormalizePtr(resolvedPublishedAt)
+                case errors.Is(resolveErr, scraper.ErrPublishedAtNotFound):
+                    // keep nil; tracking row will remain pending without enqueue
+                default:
+                    slog.Warn("inline short published_at resolve failed",
+                        slog.String("channel_id", channelID),
+                        slog.String("video_id", resourceVideoID),
+                        slog.Any("error", resolveErr))
+                }
+            }
             thumbnails := convertThumbnails(short.Thumbnail)
```

#### 3-4. community poller에도 같은 fallback을 넣는다

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/poller/pollers.go b/hololive/hololive-shared/pkg/service/youtube/poller/pollers.go
index bbbbbbb..ccccccc 100644
--- a/hololive/hololive-shared/pkg/service/youtube/poller/pollers.go
+++ b/hololive/hololive-shared/pkg/service/youtube/poller/pollers.go
@@
 type CommunityPoller struct {
     client       *scraper.Client
     db           *gorm.DB
     repo         batchRepository
     maxResults   int
     keywords     []string
     routeDecider NotificationRouteDecider
+    inlineResolveMissingPublishedAt bool
 }

-func NewCommunityPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int, keywords []string, routeDecider NotificationRouteDecider) *CommunityPoller {
+func NewCommunityPoller(
+    scraperClient *scraper.Client,
+    db *gorm.DB,
+    maxResults int,
+    keywords []string,
+    routeDecider NotificationRouteDecider,
+    inlineResolveMissingPublishedAt bool,
+) *CommunityPoller {
@@
     return &CommunityPoller{
         client:       scraperClient,
         db:           db,
         repo:         newBatchRepository(db),
         maxResults:   maxResults,
         keywords:     keywords,
         routeDecider: routeDecider,
+        inlineResolveMissingPublishedAt: inlineResolveMissingPublishedAt,
     }
 }
@@
             matchesKeywords := p.matchesKeywords(post.ContentText)
             publishedAt := post.PublishedAt
             publishedAt = yttimestamp.NormalizePtr(publishedAt)
+            if isInitialized && matchesKeywords && publishedAt == nil && p.inlineResolveMissingPublishedAt {
+                resolvedPublishedAt, resolveErr := p.client.ResolveCommunityPostPublishedAt(ctx, canonicalPostID)
+                switch {
+                case resolveErr == nil:
+                    publishedAt = yttimestamp.NormalizePtr(resolvedPublishedAt)
+                case errors.Is(resolveErr, scraper.ErrCommunityPublishedAtNotFound):
+                default:
+                    slog.Warn("inline community published_at resolve failed",
+                        slog.String("channel_id", channelID),
+                        slog.String("post_id", canonicalPostID),
+                        slog.Any("error", resolveErr))
+                }
+            }
             logCommunityPostDetected(ctx, channelID, canonicalPostID, publishedAt, detectedAt)
```

> 참고: `ResolveCommunityPostPublishedAt` 호출 인자는 canonical ID를 그대로 쓰기보다 `normalizeCommunityResourceID(canonicalPostID)`로 정규화해서 넘기는 편이 더 안전합니다. 실제 코드에서는 helper를 하나 두고 `post.ResourceID()`처럼 감싸는 것이 가장 좋습니다.

### 테스트 추가

#### constructor signature 변경으로 반드시 고쳐야 할 테스트 파일

- `hololive-shared/pkg/service/youtube/poller/shorts_poller_test.go`
- `hololive-shared/pkg/service/youtube/poller/community_poller_test.go`
- `hololive-shared/pkg/service/youtube/poller/duplicate_poll_delivery_integration_test.go`
- `hololive-stream-ingester/internal/runtime/stream_ingester_builders_test.go`

#### 반드시 추가할 테스트

1. `shorts_poller_test.go`
   - `TestShortsPoller_WhenResolverDisabled_InlinePublishedAtResolveEnqueuesNotification`
   - `TestShortsPoller_WhenResolverDisabled_InlinePublishedAtResolveNotFound_PersistsTrackingWithoutNotification`

2. `community_poller_test.go`
   - `TestCommunityPoller_WhenResolverDisabled_InlinePublishedAtResolveEnqueuesNotification`
   - `TestCommunityPoller_WhenResolverDisabled_InlinePublishedAtResolveNotFound_PersistsTrackingWithoutNotification`

3. `published_at_resolver_schema_test.go`
   - `TestValidatePublishedAtResolverSchema_SkippedWhenResolverDisabled`  
     이 테스트는 bootstrap helper 쪽으로 두는 편이 더 자연스럽습니다.

### 이 수정의 기대 효과

이 변경으로 `PublishedAtResolver.Enabled`는 진짜 기능 플래그가 됩니다.

- 켜면 현재 비동기 구조 사용
- 끄면 inline fallback으로 의미 보존
- schema 검증도 enabled일 때만 수행

즉 “플래그는 있는데 끄면 시스템 의미가 깨지는 상태”가 사라집니다.

---

## 4. 남은 P1: poller budget + resolver budget의 합산 경고가 없다

### 문제 요약

현재 scheduler budget 경고는 `ProvideScraperScheduler(...)`에서 poller registration만 보고 계산합니다.

`hololive-shared/pkg/providers/youtube_providers.go`

```go
if totalRPM > budgetRPM {
    logger.Warn("scraper_poll_budget_exceeds_rate_limit", ...)
}
```

반면 resolver는 `buildPendingPublishedAtResolver(...)`에서 자기 설정만 별도 로그로 남깁니다.

```go
logger.Info("published_at_resolver_configured",
    slog.Float64("estimated_max_rpm", estimatedPublishedAtResolverMaxRPM(resolverCfg)))
```

그런데 실제 런타임에서는 poller와 resolver가 **같은 `sharedScraperClient` / 같은 `shared rate limiter`**를 공유합니다.  
즉 둘은 서로 다른 예산이 아니라 **같은 예산을 나눠 먹는 경쟁자**입니다.

### 왜 문제인가

현재 기본값은 비교적 안전합니다.

- resolver default: `1 request / 15s ≈ 4 RPM`
- poller default: 실제 target 수가 적절하면 대개 예산 이내

하지만 운영자가 아래를 건드리면 다시 지연이 커질 수 있습니다.

- `SCRAPER_PUBLISHED_AT_RESOLVER_MAX_RESOLVE_PER_RUN`
- `SCRAPER_PUBLISHED_AT_RESOLVER_INTERVAL_SECONDS`
- notification target 수 증가
- poll interval 축소

지금은 이 상태를 조합해서 경고하지 않으므로, 설정은 “합법”인데 실제로는 detection poller를 resolver가 밀어내는 일이 생길 수 있습니다.

### 왜 AI 냄새인가

기능을 둘로 분리했지만, **리소스는 공유하는데 관측과 가드는 분리되어 있는 상태**입니다.  
이건 기능 단위로 코드를 나누다가 운영 관점의 합산 budget을 놓친 전형적인 패턴입니다.

### 수정 원칙

- poller RPM과 resolver RPM을 **같은 자리에서 합산**해서 보여줘야 합니다.
- 적어도 warning은 내야 합니다.
- 더 강하게 하려면 startup에서 fail-fast 또는 auto-clamp를 걸 수 있습니다.
- 운영상 더 무난한 기본안은 “warn + 수치 가시화”입니다.

### 수정 diff

`hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go`

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go b/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go
index ddddddd..eeeeeee 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go
@@
 import (
     "fmt"
     "log/slog"
     "strings"

+    "github.com/kapu/hololive-shared/pkg/constants"
     "github.com/kapu/hololive-shared/pkg/config"
     providers "github.com/kapu/hololive-shared/pkg/providers"
@@
 func buildStreamIngesterYouTubeComponents(
@@
 ) (*poller.Scheduler, *outbox.Dispatcher, []providers.ChannelPollerRegistration, error) {
     pollerRegistrations := buildStreamIngesterChannelPollerRegistrationsWithClient(
@@
     if err := validateExplicitPollerRegistrations(pollerRegistrations); err != nil {
         return nil, nil, nil, err
     }
+    logCombinedYouTubeScraperBudget(scraperCfg, pollerRegistrations, logger)

     scraperScheduler := providers.ProvideScraperScheduler(
         nil,
@@
 }
+
+func logCombinedYouTubeScraperBudget(
+    scraperCfg config.ScraperConfig,
+    registrations []providers.ChannelPollerRegistration,
+    logger *slog.Logger,
+) {
+    if logger == nil {
+        return
+    }
+
+    pollerRPM := estimateResolvedPollerRPM(registrations)
+    resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
+    resolverRPM := 0.0
+    if resolverCfg.Enabled {
+        resolverRPM = estimatedPublishedAtResolverMaxRPM(resolverCfg)
+    }
+    combinedRPM := pollerRPM + resolverRPM
+    budgetRPM := 60.0 / constants.YouTubeScraperRateLimitConfig.RequestInterval.Seconds()
+
+    logger.Info("youtube_scraper_combined_budget_summary",
+        slog.Float64("expected_poller_rpm", pollerRPM),
+        slog.Float64("expected_resolver_rpm", resolverRPM),
+        slog.Float64("expected_combined_rpm", combinedRPM),
+        slog.Float64("budget_rpm", budgetRPM),
+    )
+    if combinedRPM > budgetRPM {
+        logger.Warn("youtube_scraper_combined_budget_exceeds_rate_limit",
+            slog.Float64("expected_poller_rpm", pollerRPM),
+            slog.Float64("expected_resolver_rpm", resolverRPM),
+            slog.Float64("expected_combined_rpm", combinedRPM),
+            slog.Float64("budget_rpm", budgetRPM),
+        )
+    }
+}
+
+func estimateResolvedPollerRPM(registrations []providers.ChannelPollerRegistration) float64 {
+    var rpm float64
+    for _, registration := range registrations {
+        if registration.Poller == nil || registration.Interval <= 0 {
+            continue
+        }
+        channelCount := len(mergeUniqueChannelIDs(registration.ChannelIDs))
+        if channelCount == 0 {
+            continue
+        }
+        rpm += float64(channelCount) * (60.0 / registration.Interval.Seconds())
+    }
+    return rpm
+}
```

### 테스트 추가

- `hololive-stream-ingester/internal/runtime/stream_ingester_builders_test.go`
  - `TestLogCombinedYouTubeScraperBudget_ReportsPollerAndResolverRPM`
  - `TestEstimateResolvedPollerRPM_UsesExplicitChannelCounts`

> 이건 기능 correctness보다 운영 가시성 테스트입니다. log capture helper가 이미 있으면 그걸 재사용하고, 없으면 pure function인 `estimateResolvedPollerRPM`만 테스트해도 충분합니다.

### 이 수정의 기대 효과

운영자가 “resolver를 켰는데 왜 감지 poll이 느려졌는지”를 로그만 보고도 바로 이해할 수 있게 됩니다.  
즉 지금 남아 있는 유튜브 알림 지연의 재발 가능성을 운영 레벨에서 조기에 잡을 수 있습니다.

---

## 5. 남은 P1: resolver schema 검증이 컬럼만 보고 끝난다

### 문제 요약

현재 `validatePublishedAtResolverSchema(...)`는 아래 하나만 검사합니다.

```go
if !db.WithContext(ctx).Migrator().HasColumn(&domain.YouTubeCommunityShortsAlarmState{}, "published_at_retry_after") {
    return fmt.Errorf("missing migration 057: ...")
}
```

하지만 실제 resolver 성능에 중요한 것은 컬럼 존재만이 아니라 아래 index입니다.

- `056_add_ycsas_pending_published_at_resolution_index.sql`
  - `idx_ycsas_pending_published_at_resolution`
- `057_add_ycsas_published_at_retry_after.sql`
  - `idx_ycsas_pending_published_at_retry_after`

이 둘이 빠지면 resolver는 기능상 돌 수는 있어도 backlog가 늘어날수록 scan 비용이 커집니다.

### 왜 중요한가

`ListPendingPublishedAtResolutionsPage(...)` 쿼리는 다음 조건을 사용합니다.

- `actual_published_at IS NULL`
- `alarm_sent_at IS NULL`
- `authorized_at IS NULL`
- `detected_at < ?`
- `(published_at_retry_after IS NULL OR published_at_retry_after <= ?)`
- `ORDER BY detected_at ASC, post_id ASC`

즉 index가 없으면 backlog가 쌓일수록 resolver가 점점 비싸집니다.  
이건 직접적인 I/O 병목으로 이어질 수 있습니다.

### 수정 diff

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/published_at_resolver_schema.go b/hololive/hololive-stream-ingester/internal/runtime/published_at_resolver_schema.go
index fffffff..1212121 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/published_at_resolver_schema.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/published_at_resolver_schema.go
@@
 func validatePublishedAtResolverSchema(ctx context.Context, postgresService database.Client) error {
@@
-    if !db.WithContext(ctx).Migrator().HasColumn(&domain.YouTubeCommunityShortsAlarmState{}, "published_at_retry_after") {
+    migrator := db.WithContext(ctx).Migrator()
+    if !migrator.HasColumn(&domain.YouTubeCommunityShortsAlarmState{}, "published_at_retry_after") {
         return fmt.Errorf("missing migration 057: youtube_community_shorts_alarm_states.published_at_retry_after")
     }
+    if !migrator.HasIndex(&domain.YouTubeCommunityShortsAlarmState{}, "idx_ycsas_pending_published_at_resolution") {
+        return fmt.Errorf("missing migration 056 index: idx_ycsas_pending_published_at_resolution")
+    }
+    if !migrator.HasIndex(&domain.YouTubeCommunityShortsAlarmState{}, "idx_ycsas_pending_published_at_retry_after") {
+        return fmt.Errorf("missing migration 057 index: idx_ycsas_pending_published_at_retry_after")
+    }
     return nil
 }
```

### 테스트 수정

현재 `published_at_resolver_schema_test.go`의 성공 케이스는 `AutoMigrate(...)`만 호출합니다.  
하지만 named partial index는 `AutoMigrate`만으로는 보장되지 않습니다. 성공 케이스는 raw SQL로 index를 만들어 주는 방식으로 바꾸는 것이 맞습니다.

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/published_at_resolver_schema_test.go b/hololive/hololive-stream-ingester/internal/runtime/published_at_resolver_schema_test.go
index 1313131..1414141 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/published_at_resolver_schema_test.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/published_at_resolver_schema_test.go
@@
 func TestValidatePublishedAtResolverSchema_PassesWhenColumnExists(t *testing.T) {
     db := newPublishedAtResolverSchemaTestDB(t)
     require.NoError(t, db.AutoMigrate(&domain.YouTubeCommunityShortsAlarmState{}))
+    require.NoError(t, db.Exec(`
+        CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_resolution
+        ON youtube_community_shorts_alarm_states (detected_at ASC, post_id ASC)
+        WHERE actual_published_at IS NULL
+          AND alarm_sent_at IS NULL
+          AND authorized_at IS NULL
+          AND kind IN ('COMMUNITY_POST', 'NEW_SHORT')
+    `).Error)
+    require.NoError(t, db.Exec(`
+        CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_retry_after
+        ON youtube_community_shorts_alarm_states (published_at_retry_after ASC, detected_at ASC, post_id ASC)
+        WHERE actual_published_at IS NULL
+          AND alarm_sent_at IS NULL
+          AND authorized_at IS NULL
+          AND kind IN ('COMMUNITY_POST', 'NEW_SHORT')
+    `).Error)

     err := validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
         GetGormDBFunc: func() *gorm.DB { return db },
     })
     require.NoError(t, err)
 }
+
+func TestValidatePublishedAtResolverSchema_FailsWhenPendingResolutionIndexMissing(t *testing.T) {
+    db := newPublishedAtResolverSchemaTestDB(t)
+    require.NoError(t, db.AutoMigrate(&domain.YouTubeCommunityShortsAlarmState{}))
+    require.NoError(t, db.Exec(`
+        CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_retry_after
+        ON youtube_community_shorts_alarm_states (published_at_retry_after ASC, detected_at ASC, post_id ASC)
+        WHERE actual_published_at IS NULL
+          AND alarm_sent_at IS NULL
+          AND authorized_at IS NULL
+          AND kind IN ('COMMUNITY_POST', 'NEW_SHORT')
+    `).Error)
+
+    err := validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
+        GetGormDBFunc: func() *gorm.DB { return db },
+    })
+    require.ErrorContains(t, err, "missing migration 056 index")
+}
```

### 이 수정의 기대 효과

기능은 되는데 backlog가 커지면 갑자기 느려지는 상태를 startup에서 차단할 수 있습니다.  
즉 “resolver는 켰는데 왜 프로덕션에서만 늦어지는가” 같은 운영 리스크를 초기에 줄일 수 있습니다.

---

## 6. 남은 P2: empty-cache grace가 stats-only 운영 채널 변화를 최대 30초까지 얼린다

### 문제 요약

`youtube_poll_target_refresh.go`에서 cache alarm registry가 일시적으로 비었고 grace 안이면, 현재 로직은 아래처럼 그냥 `return`합니다.

```go
if !r.lastNonEmptyCacheAt.IsZero() && now.Sub(r.lastNonEmptyCacheAt) < youtubePollTargetEmptyCacheGracePeriod {
    if hasYouTubePollTargets(r.lastResolvedTargets) {
        return
    }
}
```

이 동작은 notification target 보호에는 좋습니다.  
하지만 `stats` target은 alarm cache와 무관하게 **operational channel** 변화만으로 바뀔 수 있습니다.

즉 grace 기간 동안에는 다음이 묶입니다.

- notification target 보호: 의도된 동작
- stats-only 운영 채널 변경 반영: 의도되지 않은 동작

### 영향

치명적이진 않지만, 멤버 roster가 바뀐 직후 stats poller 반영이 최대 30초 지연될 수 있습니다.

### 수정 원칙

- notification target은 last known 값을 유지
- stats target은 fresh operational channel로 재계산
- 둘을 분리해서 sync

### 수정 diff

```diff
diff --git a/hololive/hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh.go b/hololive/hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh.go
index 1515151..1616161 100644
--- a/hololive/hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh.go
+++ b/hololive/hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh.go
@@
     case cacheErr == nil && len(cacheAlarmChannelIDs) == 0:
         if !r.lastNonEmptyCacheAt.IsZero() && now.Sub(r.lastNonEmptyCacheAt) < youtubePollTargetEmptyCacheGracePeriod {
             if hasYouTubePollTargets(r.lastResolvedTargets) {
-                return
+                targets := r.lastResolvedTargets
+                targets.StatsChannelIDs = communityShortsEnabledChannelIDs(operationalChannels)
+                if !equalYouTubePollTargets(r.lastResolvedTargets, targets) {
+                    for _, registration := range r.registrations {
+                        if registration.Poller == nil || registration.Interval <= 0 {
+                            continue
+                        }
+                        if registration.TargetGroup != providers.ChannelTargetGroupStats {
+                            continue
+                        }
+                        updated := registration
+                        updated.ChannelIDs = append([]string(nil), targets.StatsChannelIDs...)
+                        r.scheduler.SyncPollerTargets(updated.ToTargetSync())
+                    }
+                    r.lastResolvedTargets = targets
+                }
+                return
             }
             alarmChannelIDs = cacheAlarmChannelIDs
             candidateFromCache = true
```

### 주의

이 diff는 중복이 많으므로 실제 구현은 `applyTargets(targets, forceImmediateNotification bool)` 같은 helper를 뽑는 편이 낫습니다.  
이건 P2라서 급하지는 않지만, 지금 코드 품질을 보면 helper 추출까지 같이 하는 편이 유지보수성은 더 좋습니다.

---

## 7. 추가로 남아 있는 AI 냄새 정리

이 부분은 기능/성능 자체보다는 유지보수성과 회귀 방지 관점입니다.

### 7-1. control-plane과 data-plane의 source of truth가 아직 완전히 분리되지 않았다

좋아진 점은 많지만, 아직도 몇 군데는 runtime control-plane이 “편의 adapter”를 경유합니다.  
이번 문서 2장에서 제안한 member repository 경로 분리는 반드시 해야 합니다.

### 7-2. feature flag semantics가 아직 한 번 더 명시돼야 한다

`PublishedAtResolver.Enabled`는 이번 문서 3장의 수정 전까지는 “진짜 disable”이 아닙니다.  
이런 half-rollback flag는 나중에 운영자가 설정을 건드릴 때 큰 사고를 만듭니다.

### 7-3. shared resource는 합산해서 봐야 한다

poller와 resolver를 코드 구조상 분리한 것은 맞지만, scraper request budget은 공유 자원입니다.  
지금처럼 각각 따로 로그를 남기면, 운영자는 두 로그를 머릿속으로 합산해야만 실제 병목을 이해할 수 있습니다.

---

## 8. 파일별 수정 체크리스트

여기부터는 실제 작업 순서입니다. 이 순서대로 커밋을 나누는 것이 가장 안전합니다.

### 커밋 1: 운영 채널 로더 authoritative path 수정

수정 파일:

- `hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder.go`
- `hololive-stream-ingester/internal/runtime/channel_target_validation.go`
- `hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go`
- 관련 테스트 파일

목표:

- `memberRepo`를 infra에 실어주기
- startup / refresher loader 모두 repo 기반으로 바꾸기
- repository 에러를 empty roster로 삼키지 않기

### 커밋 2: resolver disable 의미 정리

수정 파일:

- `hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go`
- `hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go`
- `hololive-shared/pkg/service/youtube/poller/pollers.go`
- 관련 poller 테스트

목표:

- schema 검증 conditional
- inline fallback 추가
- `Enabled=false` semantics를 실제로 완결

### 커밋 3: combined budget guard

수정 파일:

- `hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go`
- builder test

목표:

- poller RPM + resolver RPM 합산 로그/경고
- 운영에서 combined budget 바로 관측 가능하게 만들기

### 커밋 4: resolver schema 강화

수정 파일:

- `hololive-stream-ingester/internal/runtime/published_at_resolver_schema.go`
- `hololive-stream-ingester/internal/runtime/published_at_resolver_schema_test.go`

목표:

- migration 056/057 index 존재 검증
- startup fail-fast 강화

### 커밋 5: refresher P2 개선

수정 파일:

- `hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh.go`
- refresher test

목표:

- empty-cache grace 중에도 stats-only 변경 반영

---

## 9. 테스트 계획

이번 수정은 기능보다 “장애 semantics”를 고치는 작업이므로 테스트가 특히 중요합니다.

### 9-1. 반드시 green이어야 하는 테스트 축

1. **운영 채널 로더**
   - repository success
   - repository failure
   - duplicate channel
   - graduated skip

2. **refresher**
   - cache hit + no change
   - cache empty + grace
   - cache error + DB fallback
   - member repository failure + last known fallback

3. **resolver enable/disable**
   - enabled -> async resolver path 유지
   - disabled -> inline fallback으로 enqueue 유지
   - disabled -> schema validation skip

4. **schema validator**
   - column missing
   - 056 index missing
   - 057 index missing
   - 모두 존재

### 9-2. 추천 테스트 이름

- `TestResolveCommunityShortsOperationalChannelsFromRepository_ReturnsErrorOnRepositoryFailure`
- `TestBuildStreamIngesterRuntime_FailsWhenOperationalChannelRepositoryLoadFails`
- `TestYouTubePollTargetRefresher_DoesNotShrinkTargetsToZeroOnMemberRepositoryFailure`
- `TestBuildIngestionRuntime_SkipsResolverSchemaValidationWhenResolverDisabled`
- `TestShortsPoller_WhenResolverDisabled_InlinePublishedAtResolveEnqueuesNotification`
- `TestCommunityPoller_WhenResolverDisabled_InlinePublishedAtResolveEnqueuesNotification`
- `TestValidatePublishedAtResolverSchema_FailsWhenPendingResolutionIndexMissing`
- `TestValidatePublishedAtResolverSchema_FailsWhenRetryAfterIndexMissing`

---

## 10. 배포 순서와 운영 확인 포인트

### 배포 순서

1. **커밋 1과 2를 먼저 배포**  
   이 둘은 correctness 성격이 강합니다.  
   운영 채널 0개 오해와 resolver disable semantics를 먼저 닫아야 합니다.

2. **커밋 3과 4를 바로 뒤따라 배포**  
   운영 가시성과 fail-fast를 붙이는 단계입니다.

3. **커밋 5는 여유 있을 때 배포**  
   성능/정합성 미세 개선입니다.

### 배포 후 반드시 볼 로그

#### 정상이어야 하는 로그

- `Resolved YouTube poll targets`
  - `notification_target_channels > 0`
  - `stats_target_channels > 0`
- `published_at_resolver_schema_validated`
  - resolver enabled일 때만 출력
- `published_at_resolver_configured`
- `youtube_scraper_combined_budget_summary`
- `Scraper scheduler initialized`

#### 있으면 위험한 로그

- `repository lookup failed in GetAllMembers`
  - 이 로그가 계속 뜨면 운영 채널 authoritative path 수정 전에는 특히 위험
- `youtube_scraper_combined_budget_exceeds_rate_limit`
- `Failed to refresh operational channels for YouTube poll targets`
- `Pending published_at resolver iteration failed`

### 추천 메트릭/로그 필드

기존 메트릭이 충분하지 않다면 아래를 추가하는 것이 좋습니다.

- `expected_poller_rpm`
- `expected_resolver_rpm`
- `expected_combined_rpm`
- `budget_rpm`
- `operational_channel_count`
- `notification_target_channels`
- `stats_target_channels`
- `fallback_used`

---

## 11. 최종 우선순위

이번 버전에서 실제로 먼저 고쳐야 할 순서를 한 줄로 정리하면 아래와 같습니다.

### P0
1. 운영 채널 로더를 `member repository` 기반으로 바꾸기  
2. resolver disable semantics를 완결하기

### P1
3. poller + resolver 합산 budget 경고 넣기  
4. resolver schema에서 056/057 index까지 검증하기

### P2
5. empty-cache grace 중 stats-only 갱신 허용  
6. 코드 중복 helper 정리

---

## 12. 한 문장 결론

이번 추가 수정본은 예전의 큰 병목은 많이 해결했지만, **지금 남은 가장 큰 리스크는 “운영 채널 authoritative load가 empty roster로 무음 붕괴하는 경로”와 “resolver disable이 실제로는 disable이 아닌 반쪽 플래그”**입니다.  
이번 문서의 diff를 그대로 적용하면, 남은 AI 냄새와 I/O/성능/지연 회귀 가능성을 상당 부분 구조적으로 닫을 수 있습니다.

---

## 13. 검토 방식에 대한 메모

이번 문서는 최신 번들의 **정적 코드 리뷰** 기준으로 작성했습니다.  
현재 환경에서는 저장소가 요구하는 Go toolchain(`go 1.26.2`)을 직접 내려받아 실행할 수 없어, `go test ./...` 기준의 실행 검증은 하지 못했습니다.  
대신 실제 최신 소스, migration, 테스트 의도, 호출 경로, 상태 저장 경계를 기준으로 가장 재현 가능성이 높은 병목과 회귀 포인트를 정리했습니다.
