# Hololive bot 최종 코드 레벨 수정안

## 목적

이번 수정본은 가장 큰 구조 병목(알림 대상 12채널 vs 실제 스크래핑 111채널 전체)을 이미 해소했다. 남은 과제는 다음 네 가지다.

1. **subscriber cache를 authoritative하게 재구축**해서 stale registry/typed set 때문에 스케줄 타깃이 다시 오염되지 않게 할 것.
2. **poll target refresher가 cache shrink를 너무 쉽게 신뢰하지 않도록** 해서 partial cache loss가 감지 공백으로 이어지지 않게 할 것.
3. **outbox subscriber lookup의 cold-cache DB fallback 중복 조회를 제거**해서 재기동 직후 tail latency와 DB 부하를 줄일 것.
4. **community/shorts의 published_at 해석을 hot path에서 분리**해서 동일 게시물을 poll마다 재시도하는 구조를 끊을 것.

아래 순서는 실제 적용 우선순위 기준이다.

---

## Patch Set 1 — subscriber cache를 additive warm이 아니라 authoritative rebuild로 변경

### 현재 문제

`hololive/hololive-shared/pkg/service/alarm/cache_warm.go`

- `WarmSubscriberCacheFromRepository()`는 DB를 읽어서 cache에 `SAdd/HSet`만 수행한다.
- 기존 stale key를 지우지 않기 때문에 다음 key들이 남을 수 있다.
  - `alarm:channel_registry`
  - `alarm:registry`
  - `alarm:{roomID}`
  - `alarm:channel_subscribers:*`
  - `alarm:channel_subscribers_empty:*`
- 그 결과 startup 이후에도 오래된 채널이 refresher에 보일 수 있고, typed set drift가 남을 수 있다.

### 수정 방향

startup warm-up은 **rebuild semantics**로 바꿔야 한다.

즉 다음 순서로 처리한다.

1. cache에 남아 있는 alarm 관련 key를 식별한다.
2. 그 key들만 정리한다.
3. DB 기준으로 전체를 다시 채운다.

### 구현 파일

- `hololive/hololive-shared/pkg/service/alarm/cache_warm.go`
- `hololive/hololive-shared/pkg/service/alarm/cache_warm_test.go`
- `hololive/hololive-stream-ingester/internal/app/stream_ingester_alarm_cache.go`

### 권장 코드 구조

```go
func RebuildSubscriberCacheFromRepository(
    ctx context.Context,
    cacheSvc cache.Client,
    repo *Repository,
) (CacheWarmSummary, error)
```

내부 순서는 이렇게 가져간다.

```go
func RebuildSubscriberCacheFromRepository(ctx context.Context, cacheSvc cache.Client, repo *Repository) (CacheWarmSummary, error) {
    if repo == nil {
        return CacheWarmSummary{}, errors.New("rebuild subscriber cache from repository: repository is nil")
    }
    if cacheSvc == nil {
        return CacheWarmSummary{}, errors.New("rebuild subscriber cache from repository: cache service is nil")
    }

    alarms, err := repo.LoadAll(ctx)
    if err != nil {
        return CacheWarmSummary{}, fmt.Errorf("rebuild subscriber cache from repository: load alarms: %w", err)
    }

    if err := resetSubscriberCacheKeys(ctx, cacheSvc); err != nil {
        return CacheWarmSummary{}, fmt.Errorf("rebuild subscriber cache from repository: reset cache keys: %w", err)
    }

    return WarmSubscriberCacheFromAlarms(ctx, cacheSvc, alarms)
}
```

핵심은 `resetSubscriberCacheKeys()`다.

```go
func resetSubscriberCacheKeys(ctx context.Context, cacheSvc cache.Client) error {
    if cacheSvc == nil {
        return errors.New("reset subscriber cache keys: cache service is nil")
    }

    keysToDelete := make(map[string]struct{})
    keysToDelete[sharedalarmkeys.AlarmRegistryKey] = struct{}{}
    keysToDelete[sharedalarmkeys.AlarmChannelRegistryKey] = struct{}{}
    keysToDelete[sharedalarmkeys.MemberNameKey] = struct{}{}
    keysToDelete[sharedalarmkeys.RoomNamesCacheKey] = struct{}{}
    keysToDelete[sharedalarmkeys.UserNamesCacheKey] = struct{}{}

    registryRooms, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
    if err != nil {
        return fmt.Errorf("load alarm registry rooms: %w", err)
    }
    for _, roomID := range registryRooms {
        roomID = strings.TrimSpace(roomID)
        if roomID == "" {
            continue
        }
        keysToDelete[sharedalarmkeys.BuildRoomAlarmKey(roomID)] = struct{}{}
    }

    channelIDs, err := cacheSvc.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
    if err != nil {
        return fmt.Errorf("load alarm channel registry: %w", err)
    }
    for _, channelID := range channelIDs {
        channelID = strings.TrimSpace(channelID)
        if channelID == "" {
            continue
        }
        for _, alarmType := range domain.AllAlarmTypes {
            keysToDelete[sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)] = struct{}{}
            keysToDelete[sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType)] = struct{}{}
        }
    }

    emptyKeys, err := cacheSvc.ScanKeys(ctx, sharedalarmkeys.ChannelSubscribersEmptyKeyPrefix+"*", 500)
    if err != nil {
        return fmt.Errorf("scan empty subscriber keys: %w", err)
    }
    for _, key := range emptyKeys {
        if strings.TrimSpace(key) != "" {
            keysToDelete[key] = struct{}{}
        }
    }

    deleteList := make([]string, 0, len(keysToDelete))
    for key := range keysToDelete {
        deleteList = append(deleteList, key)
    }
    if len(deleteList) == 0 {
        return nil
    }

    _, err = cacheSvc.DelMany(ctx, deleteList)
    if err != nil {
        return fmt.Errorf("delete subscriber cache keys: %w", err)
    }
    return nil
}
```

### stream-ingester startup 호출 변경

`stream_ingester_alarm_cache.go`

```diff
- summary, err := sharedalarm.WarmSubscriberCacheFromRepository(ctx, cacheService, repo)
+ summary, err := sharedalarm.RebuildSubscriberCacheFromRepository(ctx, cacheService, repo)
```

### 테스트

추가해야 할 테스트는 최소 3개다.

1. `TestRebuildSubscriberCacheFromRepository_RemovesStaleRegistryAndTypedSets`
   - 기존 cache에 `UC_STALE`, `room-stale`, empty key를 미리 넣는다.
   - DB에는 `UC_REAL`만 넣는다.
   - rebuild 후 stale key가 모두 사라졌는지 확인한다.

2. `TestRebuildSubscriberCacheFromRepository_ClearsNegativeCacheKeys`
   - `alarm:channel_subscribers_empty:*`가 남아 있어도 rebuild 이후 없어지는지 검증한다.

3. `TestWarmSubscriberCacheFromRepository_RemainsAdditiveByContract`
   - 기존 함수는 additive semantics로 그대로 두고, 새 함수만 rebuild semantics를 갖는지 분리 검증한다.

### 기대 효과

- stale channel 때문에 poll target refresher가 잘못된 타깃을 잡는 회귀를 막는다.
- cache cold/warm 상태에 따른 scheduler target drift를 줄인다.

---

## Patch Set 2 — poll target refresher의 shrink 검증 추가

### 현재 문제

`hololive/hololive-stream-ingester/internal/app/youtube_poll_target_refresh.go`

현재 로직은 다음 두 경우만 DB fallback을 사용한다.

- cache read error
- cache result가 완전히 empty

하지만 cache가 **부분적으로만 비는 경우**에는 그대로 신뢰한다.
즉 12개 중 3개가 빠진 partial shrink가 오면, scheduler가 그 3개 채널의 poller job을 제거해버릴 수 있다.

### 수정 방향

cache 결과가 이전 resolved target보다 **작아지는 방향의 변경**이면, 즉시 반영하지 말고 DB로 한 번 cross-check 한 뒤에만 shrink를 허용한다.

이렇게 하면 cache corruption, race, partial flush가 바로 감지 공백으로 이어지지 않는다.

### 구현 파일

- `hololive/hololive-stream-ingester/internal/app/youtube_poll_target_refresh.go`
- `hololive/hololive-stream-ingester/internal/app/youtube_poll_target_refresh_test.go`

### 권장 함수 추가

```go
func shouldValidateTargetShrink(prev youtubePollTargets, next youtubePollTargets) bool {
    return len(next.NotificationChannelIDs) < len(prev.NotificationChannelIDs)
}
```

`refresh()` 내부는 다음 순서가 맞다.

```go
candidateTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(alarmChannelIDs, r.operationalChannels)
if shouldValidateTargetShrink(r.lastResolvedTargets, candidateTargets) {
    dbAlarmChannelIDs, dbErr := r.loadAlarmChannelIDs(ctx)
    if dbErr != nil {
        r.logger.Warn("Failed to validate YouTube poll target shrink from DB", slog.Any("error", dbErr))
        return
    }
    validatedTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(dbAlarmChannelIDs, r.operationalChannels)
    candidateTargets = validatedTargets
}

targets := candidateTargets
if equalYouTubePollTargets(r.lastResolvedTargets, targets) {
    return
}
```

### 추가 로그

cache shrink가 발생했을 때는 반드시 다음 로그를 남긴다.

```go
r.logger.Warn("YouTube poll targets shrinking; validating against DB",
    slog.Int("previous_notification_channels", len(r.lastResolvedTargets.NotificationChannelIDs)),
    slog.Int("candidate_notification_channels", len(candidateTargets.NotificationChannelIDs)),
)
```

### 테스트

1. `TestYouTubePollTargetRefresher_PartialCacheShrinkUsesDBValidation`
   - 이전 resolved target은 3개.
   - cache는 1개만 반환.
   - DB는 3개 반환.
   - 결과적으로 scheduler sync가 shrink되지 않는지 확인.

2. `TestYouTubePollTargetRefresher_ValidatedShrinkAppliesRemoval`
   - cache 1개, DB도 1개.
   - 실제 shrink가 반영되는지 확인.

3. `TestYouTubePollTargetRefresher_DBValidationFailureKeepsPreviousTargets`
   - cache가 줄었는데 DB 검증 실패.
   - 기존 target을 유지하는지 확인.

### 기대 효과

- partial cache loss가 즉시 감지 공백으로 번지는 것을 막는다.
- 운영에서 cache consistency가 흔들려도 notification poller는 더 보수적으로 동작한다.

---

## Patch Set 3 — outbox subscriber DB fallback에 channel-level singleflight 적용

### 현재 문제

`hololive/hololive-shared/pkg/service/alarm/targets.go`

현재 `ResolveChannelSubscribersByType()`는 type별로 독립 조회한다.
같은 batch에 `LIVE`, `SHORTS`, `COMMUNITY`가 같은 채널에 동시에 있으면 cold-cache 상태에서 DB fallback이 type 수만큼 중복 실행될 수 있다.

즉 channel 하나에 대해 사실상 같은 `WHERE channel_id = ?` 쿼리가 여러 번 날아간다.

### 수정 방향

`loadChannelSubscriberAlarms()`를 channel ID 기준 `singleflight`로 감싼다.

### 구현 파일

- `hololive/hololive-shared/pkg/service/alarm/targets.go`
- `hololive/hololive-shared/pkg/service/alarm/targets_test.go`

### 권장 diff

```diff
+ import "golang.org/x/sync/singleflight"
+
+ var channelSubscriberLoadGroup singleflight.Group
```

```diff
func loadChannelSubscriberAlarms(ctx context.Context, db *gorm.DB, channelID string) ([]*domain.Alarm, error) {
    if db == nil {
        return nil, errors.New("load channel subscriber alarms: database is nil")
    }
-
-   var records []domain.Alarm
-   if err := db.WithContext(ctx).
-       Where("channel_id = ?", channelID).
-       Order("created_at ASC").
-       Find(&records).Error; err != nil {
-       return nil, fmt.Errorf("load channel subscriber alarms: %w", err)
-   }
-
-   alarms := make([]*domain.Alarm, 0, len(records))
-   for i := range records {
-       alarms = append(alarms, &records[i])
-   }
-
-   return alarms, nil
+
+   result, err, _ := channelSubscriberLoadGroup.Do(strings.TrimSpace(channelID), func() (any, error) {
+       var records []domain.Alarm
+       if err := db.WithContext(ctx).
+           Where("channel_id = ?", channelID).
+           Order("created_at ASC").
+           Find(&records).Error; err != nil {
+           return nil, fmt.Errorf("load channel subscriber alarms: %w", err)
+       }
+
+       alarms := make([]*domain.Alarm, 0, len(records))
+       for i := range records {
+           record := records[i]
+           alarms = append(alarms, &record)
+       }
+       return alarms, nil
+   })
+   if err != nil {
+       return nil, err
+   }
+
+   shared := result.([]*domain.Alarm)
+   copied := make([]*domain.Alarm, 0, len(shared))
+   for i := range shared {
+       if shared[i] == nil {
+           continue
+       }
+       record := *shared[i]
+       copied = append(copied, &record)
+   }
+   return copied, nil
}
```

복사본을 돌려주는 이유는 상위 로직에서 slice/record를 건드려도 singleflight 공유 객체를 오염시키지 않기 위해서다.

### 테스트

1. `TestResolveChannelSubscribersByType_SingleflightDeduplicatesConcurrentDBFallback`
   - 같은 channel에 대해 서로 다른 alarmType 3개를 동시에 호출한다.
   - 실제 DB query counter가 1회만 증가하는지 확인.

2. `TestResolveChannelSubscribersByType_SingleflightDoesNotShareMutablePointers`
   - 첫 번째 호출 결과를 수정해도 두 번째 호출 결과가 오염되지 않는지 확인.

### 기대 효과

- cold-cache 재기동 직후 outbox fan-out tail latency 감소
- 같은 배치에서 channel 단위 중복 DB 조회 제거

---

## Patch Set 4 — scheduler explicit-target guardrail 추가

### 현재 문제

`hololive/hololive-shared/pkg/providers/youtube_providers.go`

현재는 default channel IDs가 있고, registration에 explicit IDs가 없으면 default를 사용한다.
지금 런타임은 우연히 `WithSchedulerChannelIDs(statsChannelIDs)`를 넘겨서 정상 동작하지만, 미래에 새 poller를 추가하면서 `.WithChannelIDs()`를 빼먹으면 조용히 stats 전체를 먹는다.

즉 회귀 포인트가 아직 남아 있다.

### 수정 방향

1. **모든 stream-ingester registration은 explicit target을 강제**한다.
2. `ProvideScraperScheduler()`는 “모든 registration이 explicit이면 defaultChannelIDs가 비어 있어도 정상 등록”하도록 바꾼다.
3. 반대로 explicit과 default가 섞이면 startup에서 명시적으로 에러/패닉을 내거나 최소한 경고를 강하게 남긴다.

### 구현 파일

- `hololive/hololive-shared/pkg/providers/youtube_providers.go`
- `hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go`
- `hololive/hololive-stream-ingester/internal/app/stream_ingester_poller_registrations_test.go`

### 권장 변경

#### 4-1. provider 쪽에서 explicit-only 모드 지원

```go
func allRegistrationsExplicit(registrations []ChannelPollerRegistration) bool {
    for _, reg := range registrations {
        if reg.Poller == nil || reg.Interval <= 0 {
            continue
        }
        if !reg.HasExplicitChannelIDs {
            return false
        }
    }
    return true
}
```

`ProvideScraperScheduler()` 초반부를 이렇게 바꾼다.

```go
explicitOnly := allRegistrationsExplicit(channelPollerRegistrations)

defaultChannelIDs := uniqueChannelIDs(resolvedOpts.channelIDs)
defaultTargetChannels := len(defaultChannelIDs)
if len(defaultChannelIDs) == 0 && !explicitOnly {
    if membersData == nil {
        logger.Warn("Scraper scheduler initialized without members data for non-explicit registrations")
        return scheduler
    }
    ... 기존 membersData fallback 유지 ...
}
```

#### 4-2. stream-ingester는 default channel fallback 제거

`stream_ingester_runtime_builder_helpers.go`

```diff
 scraperScheduler := providers.ProvideScraperScheduler(
     nil,
     logger,
     providers.WithChannelPollerRegistrations(pollerRegistrations),
     providers.WithSchedulerWorkerCount(scraperCfg.WorkerCountOrDefault()),
-    providers.WithSchedulerChannelIDs(statsChannelIDs),
 )
```

즉 이제 provider가 explicit-only registration을 직접 처리해야 한다.

#### 4-3. runtime builder에서 사전 검증

```go
func validateExplicitPollerRegistrations(registrations []providers.ChannelPollerRegistration) error {
    for _, reg := range registrations {
        if reg.Poller == nil || reg.Interval <= 0 {
            continue
        }
        if !reg.HasExplicitChannelIDs {
            return fmt.Errorf("poller %s is missing explicit channel IDs", reg.Poller.Name())
        }
    }
    return nil
}
```

빌더에서 호출:

```go
if err := validateExplicitPollerRegistrations(pollerRegistrations); err != nil {
    panic(err)
}
```

panic이 부담되면 startup error 반환으로 바꿔도 된다. 운영상은 fail-fast가 더 낫다.

### 테스트

1. `TestBuildStreamIngesterChannelPollerRegistrations_AllExplicit`
   - 모든 registration이 `HasExplicitChannelIDs == true`인지 확인.

2. `TestProvideScraperScheduler_ExplicitRegistrationsWorkWithoutDefaultChannelIDs`
   - `WithSchedulerChannelIDs()` 없이도 registration 기반으로 job이 생성되는지 확인.

3. `TestProvideScraperScheduler_NonExplicitRegistrationsRequireDefaultsOrMembers`
   - explicit이 아닌 registration일 때 기존 fallback 규칙이 유지되는지 확인.

### 기대 효과

- 미래 회귀로 다시 stats 전체를 notification poller가 먹는 사고를 구조적으로 막는다.

---

## Patch Set 5 — published_at 해석을 hot path에서 분리

이 부분이 남은 체감 지연의 핵심이다.

### 현재 문제

`hololive/hololive-shared/pkg/service/youtube/poller/pollers.go`

#### shorts

- `PublishedAt == nil`이면 `ResolveVideoPublishedAt()` 호출
- 실패하면 `routePending = true`
- watermark는 이전 값을 유지
- 다음 poll에서 동일 short를 다시 가져와 같은 해석을 반복

#### community

- `PublishedAt == nil`이면 routeDecider 유무와 무관하게 `ResolveCommunityPostPublishedAt()` 호출
- routeDecider가 있으면 0값일 때 `routePending = true`
- watermark가 멈춰 동일 post를 다시 폴링

즉 이건 “추가 fetch 1번” 문제가 아니라, **실패 시 같은 콘텐츠를 poll마다 다시 잡아오는 반복 루프**다.

### 수정 방향

이 부분은 2단계로 가는 것이 가장 안전하다.

---

### Patch Set 5A — 즉시 적용 가능한 hotfix

#### 목표

- 같은 콘텐츠에 대해 published_at 해석 실패를 짧은 시간 내에 반복하지 않기
- routeDecider가 꺼진 경우 community 추가 fetch를 생략하기

#### 구현 파일

- `hololive/hololive-shared/pkg/service/youtube/poller/pollers.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/shorts_poller_test.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/community_poller_test.go` (없으면 신규 추가)
- `hololive/hololive-shared/pkg/service/cache/` 재사용 가능

#### 5A-1. resolution backoff store 추가

`poller` 패키지에 작은 인터페이스를 둔다.

```go
type publishedAtResolveBackoff interface {
    Active(ctx context.Context, kind domain.OutboxKind, contentID string) bool
    Mark(ctx context.Context, kind domain.OutboxKind, contentID string, ttl time.Duration)
    Clear(ctx context.Context, kind domain.OutboxKind, contentID string)
}
```

cacheSvc가 있으면 valkey 기반, 없으면 no-op 구현을 둔다.

key 예시:

```go
const publishedAtResolveBackoffPrefix = "youtube:published_at_backoff:"
func buildPublishedAtResolveBackoffKey(kind domain.OutboxKind, contentID string) string {
    return publishedAtResolveBackoffPrefix + string(kind) + ":" + strings.TrimSpace(contentID)
}
```

#### 5A-2. shorts poller 수정

```diff
 if isInitialized {
     if dbVideo.PublishedAt == nil {
+        if p.resolveBackoff != nil && p.resolveBackoff.Active(ctx, domain.OutboxKindNewShort, canonicalPostID) {
+            if p.routeDecider != nil {
+                routePending = true
+            }
+        } else {
             resolvedPublishedAt, resolveErr := p.client.ResolveVideoPublishedAt(ctx, resourceVideoID)
             if resolveErr != nil {
                 ...
+                if p.resolveBackoff != nil {
+                    p.resolveBackoff.Mark(ctx, domain.OutboxKindNewShort, canonicalPostID, 5*time.Minute)
+                }
                 if p.routeDecider != nil {
                     routePending = true
                 }
             } else if resolvedPublishedAt == nil || resolvedPublishedAt.IsZero() {
+                if p.resolveBackoff != nil {
+                    p.resolveBackoff.Mark(ctx, domain.OutboxKindNewShort, canonicalPostID, 5*time.Minute)
+                }
                 if p.routeDecider != nil {
                     routePending = true
                 }
             } else {
                 dbVideo.PublishedAt = yttimestamp.NormalizePtr(resolvedPublishedAt)
+                if p.resolveBackoff != nil {
+                    p.resolveBackoff.Clear(ctx, domain.OutboxKindNewShort, canonicalPostID)
+                }
             }
+        }
     }
```

#### 5A-3. community poller 수정

community는 더 보수적으로 바꾸는 것이 좋다.

```diff
- if publishedAt == nil {
+ if publishedAt == nil && p.routeDecider != nil {
```

즉 routeDecider가 꺼져 있으면 굳이 hot path에서 published_at을 해석하지 않는다.

그리고 routeDecider가 켜져 있을 때도 shorts와 동일하게 backoff를 둔다.

#### 5A-4. 로그/메트릭 추가

- `published_at_resolution_attempt_total{kind}`
- `published_at_resolution_success_total{kind}`
- `published_at_resolution_backoff_skip_total{kind}`
- `published_at_resolution_failure_total{kind}`

이 메트릭은 나중에 5B로 갈지 판단하는 기준이 된다.

### 5A 테스트

1. `TestShortsPoller_PublishedAtResolutionFailureSetsBackoff`
2. `TestShortsPoller_BackoffSkipsRepeatedResolutionCalls`
3. `TestCommunityPoller_DoesNotResolvePublishedAtWhenRouteDeciderNil`
4. `TestCommunityPoller_PublishedAtResolutionBackoffAppliedWhenRouteDeciderEnabled`

### 5A 기대 효과

- 같은 short/post에 대해 매 poll마다 추가 HTTP 요청이 반복되는 현상을 줄인다.
- routeDecider 비활성 상태에서는 community 추가 fetch를 제거한다.

---

### Patch Set 5B — 근본 해결: published_at resolver를 별도 루프로 분리

이게 최종 해법이다.

#### 목표

poller는 “새 콘텐츠를 빠르게 저장하고 watermark를 전진”하는 역할만 하고,
published_at 해석과 route authorization은 별도 resolver가 담당하게 만든다.

#### 핵심 설계

1. **poller는 절대 published_at 때문에 watermark를 멈추지 않는다.**
2. 새 short/community post를 발견하면 DB row와 tracking/source/alarm_state는 먼저 저장한다.
3. `ActualPublishedAt == nil`이고 아직 `AuthorizedAt == nil`, `AlarmSentAt == nil`인 항목은 resolver가 뒤에서 처리한다.
4. resolver가 published_at을 해석한 뒤 routeDecider를 적용하고 outbox row를 transactionally 생성한다.

#### 구현 파일

- 신규: `hololive/hololive-shared/pkg/service/youtube/poller/published_at_resolver.go`
- 신규: `hololive/hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go`
- 수정: `hololive/hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go`
- 수정: `hololive/hololive-shared/pkg/service/youtube/tracking/repository.go`
- 수정: `hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_runner.go`
- 수정: `hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_lifecycle.go`
- 수정: `hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_helpers.go`
- 필요 시 config 추가: `hololive/hololive-shared/pkg/config/config_types.go`, `config.go`

#### 신규 repository query

`alarm_state_repository.go`

```go
type PublishedAtResolutionCandidate struct {
    Kind      domain.OutboxKind
    PostID    string
    ContentID string
    ChannelID string
    DetectedAt time.Time
}

func (r *GormRepository) ListPendingPublishedAtResolutions(
    ctx context.Context,
    detectedBefore time.Time,
    limit int,
) ([]PublishedAtResolutionCandidate, error)
```

SQL 조건은 이 기준이 맞다.

- `kind IN ('COMMUNITY_POST', 'NEW_SHORT')`
- `actual_published_at IS NULL`
- `alarm_sent_at IS NULL`
- `authorized_at IS NULL`
- `detected_at < ?`
- `ORDER BY detected_at ASC`
- `LIMIT ?`

즉 이미 발송된 것, 이미 authorize된 것은 건드리지 않는다.

#### resolver 서비스 개념 구조

```go
type PendingPublishedAtResolver struct {
    db           *gorm.DB
    client       *scraper.Client
    routeDecider NotificationRouteDecider
    interval     time.Duration
    batchSize    int
    logger       *slog.Logger
}

func (r *PendingPublishedAtResolver) Start(ctx context.Context)
```

루프 순서는 이렇다.

1. `ListPendingPublishedAtResolutions()`로 후보를 가져온다.
2. 각 후보에 대해 kind별로 `ResolveVideoPublishedAt` 또는 `ResolveCommunityPostPublishedAt`를 호출한다.
3. 성공 시 repository 메서드 하나로 transaction 처리한다.

#### transaction 단위 finalize 메서드

신규 repository 메서드는 다음 책임을 한 번에 가져야 한다.

```go
func (r *ResolverRepository) FinalizePublishedAtAndMaybeEnqueue(
    ctx context.Context,
    candidate PublishedAtResolutionCandidate,
    publishedAt time.Time,
    routeDecider NotificationRouteDecider,
) error
```

트랜잭션 내부 단계:

1. `youtube_videos` 또는 `youtube_community_posts`의 `published_at` 업데이트
2. `youtube_content_alarm_tracking.actual_published_at` 업데이트
3. `youtube_community_shorts_source_posts.actual_published_at` 업데이트
4. `youtube_community_shorts_alarm_states.actual_published_at` 업데이트
5. routeDecider 평가
6. route 허용 시 `TryClaimAlarmState()`로 claim
7. claim 성공 시 outbox row insert (`kind, channel_id, content_id, payload, status=PENDING`)

중요한 점은 **outbox insert 전 claim**이다.
그래야 poller/재시도/resolver가 겹쳐도 중복 enqueue가 생기지 않는다.

#### poller 쪽 변경

`pollers.go`에서는 다음 두 가지를 제거한다.

1. published_at 실패 시 `routePending = true`
2. routePending 때문에 watermark를 유지하는 로직

즉 아래가 핵심이다.

```diff
- routePending := false
+ routePending := false // 제거 가능
...
- if dbVideo.PublishedAt == nil { ResolveVideoPublishedAt(...) }
+ // published_at resolution is deferred to resolver
...
- if p.routeDecider != nil && routePublishedAt.IsZero() {
-     continue
- }
+ if p.routeDecider != nil && routePublishedAt.IsZero() {
+     // tracking/state만 저장하고 enqueue는 resolver가 담당
+     continue
+ }
...
- LastContentID: resolveWatermarkLastContentID(..., lastSeenID, routePending),
+ LastContentID: normalizeContentID(..., shorts[0].VideoID),
```

community도 동일하다.

즉 watermark는 항상 전진한다.

### 5B 테스트

1. `TestShortsPoller_PublishedAtMissingStillAdvancesWatermark`
2. `TestCommunityPoller_PublishedAtMissingStillAdvancesWatermark`
3. `TestPendingPublishedAtResolver_EnqueuesOnceAfterResolution`
4. `TestPendingPublishedAtResolver_DoesNotDuplicateWhenAlarmStateAlreadyClaimed`
5. `TestPendingPublishedAtResolver_UpdatesTrackingSourceStateAtomically`
6. `TestPendingPublishedAtResolver_SkipsAlreadySentContent`

### 5B 기대 효과

- community/shorts tail latency의 반복 루프 제거
- poller가 detection 전용으로 단순화
- published_at 해석 실패가 전체 watermark 진행을 막지 않음

---

## Patch Set 6 — 운영 메트릭과 로그 추가

이건 구현 후 검증을 위해 필요하다.

### 추가해야 할 메트릭

1. `youtube_poll_target_refresh_cache_shrink_validation_total{result=validated|failed|skipped}`
2. `alarm_subscriber_db_fallback_total{result=hit|miss|error}`
3. `alarm_subscriber_db_singleflight_shared_total`
4. `published_at_resolution_attempt_total{kind}`
5. `published_at_resolution_success_total{kind}`
6. `published_at_resolution_failure_total{kind}`
7. `published_at_resolution_backoff_skip_total{kind}`
8. `published_at_resolver_enqueued_total{kind}`
9. `published_at_resolver_pending_candidates`

### 추가 로그

- startup rebuild 결과
  - `subscriber_cache_rebuilt_from_db`
  - `alarms_loaded`, `rooms_loaded`, `channels_loaded`, `keys_deleted`

- poll target shrink DB 검증
  - 이전 채널 수, candidate 수, validated 수

- resolver enqueue 성공/스킵
  - `kind`, `post_id`, `channel_id`, `published_at`, `reason`

---

## 적용 순서

### 1차 배포 (즉시)

- Patch Set 1
- Patch Set 2
- Patch Set 3
- Patch Set 4
- Patch Set 5A
- Patch Set 6 일부 메트릭

이 조합만으로도 현재 남은 재발 포인트와 반복 fetch tail을 크게 줄일 수 있다.

### 2차 배포 (근본 해결)

- Patch Set 5B
- Patch Set 6 나머지 메트릭/로그

---

## 배포 후 확인 기준

1. `scraper_poll_budget_exceeds_rate_limit`가 계속 0이어야 한다.
2. `Scraper scheduler initialized.total_jobs`가 159 안팎이어야 한다.
3. `notification_target_channels`가 실 구독 채널 수와 일치해야 한다.
4. `published_at_resolution_backoff_skip_total`이 올라가더라도 poller RPM은 증가하지 않아야 한다.
5. resolver 도입 후에는 shorts/community의 동일 콘텐츠에 대한 repeated detection 로그가 눈에 띄게 줄어야 한다.
6. `outbox_no_subscribers`가 급증하면 cache rebuild 또는 subscriber fallback 쪽을 다시 봐야 한다.

---

## 한 줄 정리

지금 기준의 최종 수정안은 단순 성능 튜닝이 아니다.  
**cache를 authoritative하게 재구축하고, cache shrink를 보수적으로 검증하고, subscriber DB fallback 중복을 제거하고, published_at 해석을 polling hot path에서 분리하는 구조 수정**이 정답이다.
