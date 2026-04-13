# hololive-bot-full-20260412T162736Z diff-level review and fix guide

## 0. 목적과 전제

이 문서는 **“항상 아직 버그가 남아 있다”**는 전제로 최신 번들을 다시 본 결과다.  
목표는 단순 리뷰가 아니라, 이 문서만 보고도 실제 수정 작업을 끝낼 수 있도록 **원인 분석 + 코드 경로 + unified diff 수준 수정안 + 테스트 추가 목록 + 배포 후 확인 포인트**까지 제공하는 것이다.

이번 번들에서 이미 정리된 것과 아직 남은 것을 구분해서 본다.

---

## 1. 이번 번들에서 이미 좋아진 점

이번 번들은 이전 번들 대비 다음은 확실히 좋아졌다.

1. **root/worktree split-brain 문제는 사실상 해소**됐다. 번들 안에 `.worktrees/`가 없고, 이전처럼 inert copy만 고치는 상태가 아니다.
2. **migration manifest 불일치 문제는 해소**됐다. 번들 루트에서 `./scripts/architecture/check-migration-manifest.sh`를 실행하면 `OK: migration manifest matches SQL files`가 나온다.
3. **startup poll target은 DB-authoritative**다.  
   `hololive-stream-ingester/internal/runtime/bootstrap_stream_ingester.go`에서
   `resolveCommunityShortsOperationalChannelsFromRepository(...)`와
   `resolveYouTubePollTargets(...)`를 사용한다.
4. **poller target group 분리**는 유지되고 있다.  
   `videos/shorts/community/live`는 notification target만, `stats`는 stats target만 탄다.
5. **runtime poll target refresher**는 set 비교와 DB validation을 갖췄다.  
   order-sensitive equality bug는 사라졌다.
6. **published_at resolver schema 검증**은 column뿐 아니라 index까지 검사한다.
7. **subscriber lookup cache miss → DB fallback + singleflight**는 들어가 있다.
8. **outbox 2초 poll / startup immediate process**, **scheduler due-time timer** 개선은 유지된다.

즉, 예전의 “111채널 전체를 5 poller로 계속 긁는 구조”와 “30초 outbox floor” 같은 주병목은 이미 많이 해결됐다.

이제 남은 것은 더 교묘한 버그다.

---

## 2. 이번 번들에서 남은 핵심 결론

### P0-1. shorts/community 알림이 아직도 resolver를 **불필요하게** critical path로 탄다

현재 코드에서는 `published_at`이 비어 있으면 shorts/community 알림이 바로 enqueue되지 않는다.  
문제는 이 동작이 **route decider가 없는 경우에도** 그대로 적용된다는 점이다.

즉, community/shorts big-bang이 꺼져 있어 `routeDecider == nil`이어도, resolver가 enabled인 상태에서는 poller가 알림을 바로 보내지 않고 resolver가 나중에 `published_at`을 채우길 기다린다.

이건 현재 남은 **유튜브 알림 지연의 가장 큰 원인**이다.

### P0-2. resolver가 shutdown cancel과 real timeout을 같은 것으로 취급한다

`context.Canceled`를 실제 per-candidate timeout처럼 처리해서 `published_at_retry_after`를 적는다.  
서비스 종료/재기동 시점에 이 경로가 타면, 재기동 후에도 같은 candidate가 **최대 5분** 미뤄질 수 있다.

이건 지연보다 더 안 좋은 **blind spot**이다.

### P0-3. retry_after write 실패를 무시한다

resolver는 실패/empty/finalize error 때 `published_at_retry_after`를 적으려 하지만, 이 쓰기 실패를 전부 `_ = ...`로 버린다.  
DB 쓰기 실패가 나면 resolver는 같은 candidate를 다음 run에서 다시 긁는다.  
즉, **실패가 실패를 증폭하는 구조**다.

### P0-4. 현재 repo 기본 poll interval이 scraper budget을 이미 초과한다

`hololive-shared/pkg/config/config_types.go`의 현재 기본값은 다음이다.

- Videos: 15m
- Shorts: 1m
- Community: 1m
- Stats: 6h
- Live: 10m

notification target 12개, stats target 111개라는 지금 구조를 그대로 대입하면 poller RPM은 약 **26.31 RPM**이다.  
scraper budget은 `3초/request` 기준 **20 RPM**이다.

즉, **resolver를 빼도 poller 기본값만으로 budget을 초과**한다.  
resolver 기본값(`15s interval`, `maxResolvePerRun=1`)까지 더하면 theoretical combined RPM은 **30.31 RPM**까지 올라간다.

현재 budget warning log는 있지만 **fail-fast가 아니다**.  
즉, 설정이 잘못돼도 서비스는 뜬다.

### P1-1. resolver는 여전히 scheduler 밖 별도 loop라서 budget fairness가 구조적으로 약하다

shared rate limiter를 공유하더라도, scheduler priority/queue와 분리된 별도 goroutine loop이기 때문에 **poller vs resolver 공정성**이 강제되지 않는다.  
지금은 advisory log만 있고 강제 제어는 없다.

### P2-1. additive cache warm와 service adapter empty-slice fallback은 아직 latent smell이다

이번 번들에서 critical path는 많이 정리됐지만,

- `WarmSubscriberCacheFromRepository()`는 여전히 additive warm이다.
- `member.ServiceAdapter.GetAllMembers()`는 repo error를 warn만 찍고 empty slice를 돌려준다.

현재 stream-ingester poll target path는 repo-authoritative로 바뀌어서 예전만큼 위험하지는 않다.  
그래도 **장애 시 조용히 빈 값으로 degrade**하는 냄새는 여전히 남아 있다.

---

## 3. P0-1 상세: routeDecider가 nil이어도 published_at resolver가 알림의 필수 경로가 되는 문제

### 3.1 문제 코드 경로

`hololive-stream-ingester/internal/runtime/community_shorts_route_policy.go`

```go
func buildCommunityShortsRouteDecider(policy communityShortsBigBangPolicy) poller.NotificationRouteDecider {
    if !policy.Enabled() {
        return nil
    }
    ...
}
```

즉, big-bang이 꺼져 있으면 `routeDecider == nil`이다.

그런데 `hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go`는 현재 이렇게 되어 있다.

```go
resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
inlineResolveMissingPublishedAt := !resolverCfg.Enabled
shortsPoller := poller.NewShortsPoller(scraperClient, db, 10, routeDecider, inlineResolveMissingPublishedAt)
communityPoller := poller.NewCommunityPoller(scraperClient, db, 10, communityKeywords, routeDecider, inlineResolveMissingPublishedAt)
```

resolver가 기본적으로 enabled이므로 `inlineResolveMissingPublishedAt`는 false가 된다.

이 상태에서 `hololive-shared/pkg/service/youtube/poller/pollers.go`의 shorts/community는 공통적으로 다음 패턴을 가진다.

```go
if routePublishedAt.IsZero() {
    if p.inlinePublishedAtFallbackEnabled {
        keepExistingWatermark = true
    }
    continue
}
if shouldEnqueueRoutedNotification(p.routeDecider, ..., routePublishedAt) {
    notifications = append(...)
}
```

즉, `routeDecider == nil`이어도 `routePublishedAt.IsZero()`면 enqueue를 건너뛴다.

### 3.2 왜 이게 버그인가

`routeDecider == nil`이라는 것은 라우팅 판단에 `published_at`이 **필수값이 아니라는 뜻**이다.  
그런데 현재 구현은 “라우팅이 필요 없는 경우”와 “published_at이 필요한 경우”를 같은 분기로 처리한다.

이건 전형적인 AI 냄새다.

- 제어 플래그가 `resolver enabled` 하나에 과도하게 결합돼 있다.
- 실제 비즈니스 의미는 “`published_at`이 라우팅에 필요한가?”인데, 구현은 “resolver가 켜져 있나?”로 대체되어 있다.
- 결과적으로 **필요 없는 비동기 후처리**가 사용자-facing latency path에 들어와 있다.

### 3.3 실제 지연 규모

resolver 기본값:

- Interval = 15s
- MaxResolvePerRun = 1
- ResolveTimeout = 10s
- MinDetectedAge = 30s
- Outbox poll = 2s

따라서 resolver가 critical path일 때 첫 알림 worst-case는 대략 다음이다.

- `MinDetectedAge` 최대 30초
- 다음 resolver run 대기 최대 15초
- resolve timeout 최대 10초
- outbox 최대 2초

합계 약 **57초**

평균적으로도 첫 알림은 **40초대**가 된다.  
backlog에 item이 1개씩 더 쌓이면 `MaxResolvePerRun=1` 때문에 대체로 **15초씩** 더 밀린다.

즉, 지금 남은 유튜브 알림 지연의 본질은 이 경로다.

---

## 4. P0-1 수정안: “routeDecider가 없으면 즉시 enqueue, resolver는 metadata backfill만 담당”

이 문제는 **반드시 두 단계**로 고쳐야 한다.

### 4.1 1단계: poller에서 routeDecider가 nil이면 published_at 없이도 즉시 enqueue

#### 수정 파일 1
`hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go`

현재:

```go
resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
inlineResolveMissingPublishedAt := !resolverCfg.Enabled
```

권장 변경:

```diff
diff --git a/hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go b/hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go
@@
-    resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
-    inlineResolveMissingPublishedAt := !resolverCfg.Enabled
+    resolverCfg := effectivePublishedAtResolverConfig(scraperCfg)
+    requiresPublishedAtForRouting := routeDecider != nil
+    inlineResolveMissingPublishedAt := !resolverCfg.Enabled && requiresPublishedAtForRouting
@@
-    shortsPoller := poller.NewShortsPoller(scraperClient, db, 10, routeDecider, inlineResolveMissingPublishedAt)
-    communityPoller := poller.NewCommunityPoller(scraperClient, db, 10, communityKeywords, routeDecider, inlineResolveMissingPublishedAt)
+    shortsPoller := poller.NewShortsPoller(scraperClient, db, 10, routeDecider, inlineResolveMissingPublishedAt)
+    communityPoller := poller.NewCommunityPoller(scraperClient, db, 10, communityKeywords, routeDecider, inlineResolveMissingPublishedAt)
```

핵심은 `inlineResolveMissingPublishedAt`의 의미를 “resolver가 켜져 있냐”가 아니라  
“resolver가 꺼져 있고, 동시에 published_at이 라우팅에 필요한가”로 바꾸는 것이다.

#### 수정 파일 2
`hololive-shared/pkg/service/youtube/poller/pollers.go`

shorts 쪽:

```diff
diff --git a/hololive-shared/pkg/service/youtube/poller/pollers.go b/hololive-shared/pkg/service/youtube/poller/pollers.go
@@
-                var routePublishedAt time.Time
-                if dbVideo.PublishedAt != nil {
-                    routePublishedAt = *dbVideo.PublishedAt
-                }
-                if routePublishedAt.IsZero() {
-                    if p.inlinePublishedAtFallbackEnabled {
-                        keepExistingWatermark = true
-                    }
-                    continue
-                }
-
-                if shouldEnqueueRoutedNotification(p.routeDecider, domain.AlarmTypeShorts, channelID, routePublishedAt) {
+                requiresPublishedAtForRouting := p.routeDecider != nil
+                var routePublishedAt time.Time
+                if dbVideo.PublishedAt != nil {
+                    routePublishedAt = *dbVideo.PublishedAt
+                }
+                if routePublishedAt.IsZero() && requiresPublishedAtForRouting {
+                    if p.inlinePublishedAtFallbackEnabled {
+                        keepExistingWatermark = true
+                    }
+                    continue
+                }
+
+                if !requiresPublishedAtForRouting ||
+                    shouldEnqueueRoutedNotification(p.routeDecider, domain.AlarmTypeShorts, channelID, routePublishedAt) {
                     notifications = append(notifications, &domain.YouTubeNotificationOutbox{
                         Kind:      domain.OutboxKindNewShort,
                         ChannelID: channelID,
                         ContentID: canonicalPostID,
                         Payload:   buildShortNotificationPayload(dbVideo, canonicalPostID),
                         Status:    domain.OutboxStatusPending,
                     })
                 }
```

community 쪽도 동일 패턴으로 바꾼다.

```diff
diff --git a/hololive-shared/pkg/service/youtube/poller/pollers.go b/hololive-shared/pkg/service/youtube/poller/pollers.go
@@
-                var routePublishedAt time.Time
-                if dbPost.PublishedAt != nil {
-                    routePublishedAt = *dbPost.PublishedAt
-                }
-                if routePublishedAt.IsZero() {
-                    if p.inlinePublishedAtFallbackEnabled {
-                        keepExistingWatermark = true
-                    }
-                    continue
-                }
-                if shouldEnqueueRoutedNotification(p.routeDecider, domain.AlarmTypeCommunity, channelID, routePublishedAt) {
+                requiresPublishedAtForRouting := p.routeDecider != nil
+                var routePublishedAt time.Time
+                if dbPost.PublishedAt != nil {
+                    routePublishedAt = *dbPost.PublishedAt
+                }
+                if routePublishedAt.IsZero() && requiresPublishedAtForRouting {
+                    if p.inlinePublishedAtFallbackEnabled {
+                        keepExistingWatermark = true
+                    }
+                    continue
+                }
+                if !requiresPublishedAtForRouting ||
+                    shouldEnqueueRoutedNotification(p.routeDecider, domain.AlarmTypeCommunity, channelID, routePublishedAt) {
                     notifications = append(notifications, &domain.YouTubeNotificationOutbox{
                         Kind:      domain.OutboxKindCommunityPost,
                         ChannelID: channelID,
                         ContentID: canonicalPostID,
                         Payload:   buildCommunityNotificationPayload(dbPost, canonicalPostID),
                         Status:    domain.OutboxStatusPending,
                     })
                 }
```

이 변경의 의미는 단순하다.

- **routeDecider가 nil이면** `published_at`은 알림 발송의 필수값이 아니다.
- 따라서 poller는 **즉시 enqueue**한다.
- resolver는 이후 metadata backfill만 담당한다.

### 4.2 왜 이 변경이 안전한가

`hololive-shared/pkg/service/youtube/poller/repository_batch.go`의 validation은  
payload의 `published_at`이 비어 있어도 DB row `published_at`이 nil이면 허용한다.

즉, 현재 저장/발송 파이프라인은 이미 `published_at` 없는 short/community 알림을 처리할 수 있다.  
지금까지 못 보낸 이유는 validation이 아니라 **poller 분기**였다.

---

## 5. P0-1 보완: 즉시 보낸 뒤에도 resolver가 metadata backfill을 계속 할 수 있게 해야 한다

위 4.1만 적용하면 알림 지연은 크게 줄어든다.  
하지만 현재 resolver query는 `authorized_at IS NULL` / `alarm_sent_at IS NULL` row만 보기 때문에, 이미 즉시 enqueue된 item은 이후 `actual_published_at`을 backfill하지 못한다.

따라서 resolver query와 finalize 로직도 같이 바꿔야 한다.

### 5.1 수정 파일 3
`hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go`

현재 `ListPendingPublishedAtResolutionsPage()`는 다음 필터를 가진다.

```go
.Where("actual_published_at IS NULL")
.Where("alarm_sent_at IS NULL")
.Where("authorized_at IS NULL")
```

권장 변경:

```diff
diff --git a/hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go b/hololive-shared/pkg/service/youtube/tracking/alarm_state_repository.go
@@
-        Where("actual_published_at IS NULL").
-        Where("alarm_sent_at IS NULL").
-        Where("authorized_at IS NULL").
+        Where("actual_published_at IS NULL").
         Where("detected_at < ?", yttimestamp.Normalize(detectedBefore)).
         Select("kind, post_id, content_id, channel_id, detected_at, published_at_retry_after").
         Where("(published_at_retry_after IS NULL OR published_at_retry_after <= ?)", yttimestamp.Normalize(referenceNow))
```

즉, resolver의 대상은 “아직 보내지지 않은 row”가 아니라 **“actual_published_at이 아직 비어 있는 row” 전체**가 되어야 한다.

이렇게 해야 이미 즉시 발송된 short/community도 나중에 metadata backfill이 된다.

### 5.2 수정 파일 4
`hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go`

현재 `FinalizePublishedAtAndMaybeEnqueue()`는 state row나 tracking row에 `AuthorizedAt` / `AlarmSentAt`가 있으면 바로 return한다.  
이렇게 하면 metadata 업데이트 자체가 일어나지 않는다.

이 early return을 제거하고, **메타데이터는 항상 업데이트하되 enqueue만 조건부로** 해야 한다.

권장 diff:

```diff
diff --git a/hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go b/hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go
@@
 type publishedAtFinalizeResult struct {
     enqueued bool
     reason   string
 }
+
+type publishedAtFinalizeMode struct {
+    allowEnqueue bool
+    alreadyAuthorized bool
+    alreadySent bool
+}
@@
-        if stateRow != nil {
-            if stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero() {
-                result.reason = "already_sent"
-                return nil
-            }
-            if stateRow.AuthorizedAt != nil && !stateRow.AuthorizedAt.IsZero() {
-                result.reason = "already_claimed"
-                return nil
-            }
-        }
+        mode := publishedAtFinalizeMode{allowEnqueue: true}
+        if stateRow != nil {
+            if stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero() {
+                mode.alreadySent = true
+                mode.allowEnqueue = false
+            }
+            if stateRow.AuthorizedAt != nil && !stateRow.AuthorizedAt.IsZero() {
+                mode.alreadyAuthorized = true
+                mode.allowEnqueue = false
+            }
+        }
@@
-        if trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero() {
-            result.reason = "already_sent"
-            return nil
-        }
+        if trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero() {
+            mode.alreadySent = true
+            mode.allowEnqueue = false
+        }

-        notification, reason, err := r.finalizeCandidateState(ctx, tx, txRepo, candidate, normalizedPublishedAt, routeDecider)
+        notification, reason, err := r.finalizeCandidateState(ctx, tx, txRepo, candidate, normalizedPublishedAt, routeDecider, mode)
@@
-        if notification == nil {
+        if notification == nil {
             if err := txRepo.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID); err != nil {
                 return fmt.Errorf("clear published_at retry after: %w", err)
             }
             return nil
         }
```

그리고 `finalizeShort()` / `finalizeCommunity()` 시그니처에 `mode publishedAtFinalizeMode`를 추가한다.

핵심 변경은 enqueue 직전이다.

```diff
diff --git a/hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go b/hololive-shared/pkg/service/youtube/poller/published_at_resolver_repository.go
@@
-    if !shouldEnqueueRoutedNotification(routeDecider, domain.AlarmTypeShorts, candidate.ChannelID, publishedAt) {
+    if !mode.allowEnqueue {
+        if mode.alreadySent {
+            return nil, "backfilled_after_send", nil
+        }
+        if mode.alreadyAuthorized {
+            return nil, "backfilled_after_authorize", nil
+        }
+        return nil, "backfilled_metadata_only", nil
+    }
+    if !shouldEnqueueRoutedNotification(routeDecider, domain.AlarmTypeShorts, candidate.ChannelID, publishedAt) {
         return nil, "route_decider_rejected", nil
     }
```

community에도 동일하게 넣는다.

이렇게 하면 resolver는 이제 두 역할을 모두 수행한다.

1. 아직 미발송인 item이면 `published_at`을 채우고 필요 시 enqueue
2. 이미 발송되었거나 authorized인 item이면 **enqueue 없이 metadata만 backfill**

이게 “정확성과 지연”을 동시에 만족하는 구조다.

---

## 6. P0-2 수정안: shutdown cancel과 real timeout을 분리

현재 `published_at_resolver.go`는 다음 코드가 문제다.

```go
isResolveTimeout := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
_ = r.markPublishedAtRetryAfter(..., isResolveTimeout)
```

이건 parent context cancel과 per-candidate timeout을 같은 것으로 본다.

### 수정 파일 5
`hololive-shared/pkg/service/youtube/poller/published_at_resolver.go`

권장 diff:

```diff
diff --git a/hololive-shared/pkg/service/youtube/poller/published_at_resolver.go b/hololive-shared/pkg/service/youtube/poller/published_at_resolver.go
@@
-                if err != nil {
-                    observePublishedAtResolutionFailure(candidate.Kind)
-                    isResolveTimeout := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
-                    _ = r.markPublishedAtRetryAfter(tracking, ctx, candidate, time.Now().Add(failureBackoffTTL), isResolveTimeout)
-                    if isResolveTimeout {
-                        observePublishedAtResolverSkipped(candidate.Kind, "resolve_timeout")
-                    }
-                    r.logger.Warn("Pending published_at resolver failed to resolve candidate",
-                        slog.String("kind", string(candidate.Kind)),
-                        slog.String("post_id", candidate.PostID),
-                        slog.String("content_id", candidate.ContentID),
-                        slog.Duration("resolve_timeout", resolveTimeout),
-                        slog.Any("error", err),
-                    )
-                    continue
-                }
+                if err != nil {
+                    observePublishedAtResolutionFailure(candidate.Kind)
+
+                    if ctx.Err() != nil {
+                        return ctx.Err()
+                    }
+
+                    isResolveTimeout := errors.Is(err, context.DeadlineExceeded)
+                    if retryErr := r.applyRetryAfter(tracking, ctx, candidate, time.Now().Add(failureBackoffTTL), isResolveTimeout); retryErr != nil {
+                        r.logger.Error("published_at_resolver_retry_after_write_failed",
+                            slog.String("kind", string(candidate.Kind)),
+                            slog.String("post_id", candidate.PostID),
+                            slog.Any("error", retryErr),
+                        )
+                    }
+                    if isResolveTimeout {
+                        observePublishedAtResolverSkipped(candidate.Kind, "resolve_timeout")
+                    }
+                    r.logger.Warn("Pending published_at resolver failed to resolve candidate",
+                        slog.String("kind", string(candidate.Kind)),
+                        slog.String("post_id", candidate.PostID),
+                        slog.String("content_id", candidate.ContentID),
+                        slog.Duration("resolve_timeout", resolveTimeout),
+                        slog.Any("error", err),
+                    )
+                    continue
+                }
```

여기서 가장 중요한 점은:

- **parent ctx cancel이면 즉시 return**
- `context.Canceled`를 **retry_after를 남겨야 하는 timeout**으로 취급하지 않음
- retry_after write 실패도 버리지 않음

이 변경은 재기동 직후의 불필요한 5분 blind spot을 막는다.

### 테스트도 같이 바꿔야 한다

현재 테스트 이름:
`TestPendingPublishedAtResolver_CancelDuringCandidateResolutionSetsRetryAfter`

이 테스트는 이제 잘못된 기대값이다.  
새 기대는 “cancel이면 retry_after를 남기지 않고 종료”다.

테스트를 다음으로 교체한다.

```diff
diff --git a/hololive-shared/pkg/service/youtube/poller/published_at_resolver_test.go b/hololive-shared/pkg/service/youtube/poller/published_at_resolver_test.go
@@
-func TestPendingPublishedAtResolver_CancelDuringCandidateResolutionSetsRetryAfter(t *testing.T) {
+func TestPendingPublishedAtResolver_CancelDuringCandidateResolutionDoesNotSetRetryAfter(t *testing.T) {
@@
-    require.NoError(t, resolver.runOnce(ctx, detectedAt.Add(time.Minute)))
+    err := resolver.runOnce(ctx, detectedAt.Add(time.Minute))
+    require.Error(t, err)
+    require.ErrorIs(t, err, context.Canceled)
@@
-    require.NotNil(t, alarmState.PublishedAtRetryAfter)
+    require.Nil(t, alarmState.PublishedAtRetryAfter)
```

---

## 7. P0-3 수정안: retry_after write 실패를 더 이상 무시하지 말 것

현재 resolver는 세 군데에서 retry_after를 쓰면서 결과를 버린다.

- resolve error
- published_at empty
- finalize error

이건 반복 fetch와 budget 낭비를 만든다.

### 수정 파일 5 계속
`hololive-shared/pkg/service/youtube/poller/published_at_resolver.go`

helper를 하나 도입한다.

```diff
diff --git a/hololive-shared/pkg/service/youtube/poller/published_at_resolver.go b/hololive-shared/pkg/service/youtube/poller/published_at_resolver.go
@@
 func (r *PendingPublishedAtResolver) markPublishedAtRetryAfter(
@@
 }
+
+func (r *PendingPublishedAtResolver) applyRetryAfter(
+    tracking *trackingrepo.GormRepository,
+    ctx context.Context,
+    candidate trackingrepo.PublishedAtResolutionCandidate,
+    retryAfter time.Time,
+    forceLive bool,
+) error {
+    if err := r.markPublishedAtRetryAfter(tracking, ctx, candidate, retryAfter, forceLive); err != nil {
+        observePublishedAtResolverSkipped(candidate.Kind, "retry_after_write_failed")
+        return err
+    }
+    return nil
+}
```

그리고 모든 `_ = tracking.MarkPublishedAtRetryAfter(...)` / `_ = r.markPublishedAtRetryAfter(...)`를 `applyRetryAfter(...)`로 교체한다.

예시:

```diff
@@
-                if publishedAt == nil || publishedAt.IsZero() {
-                    _ = tracking.MarkPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID, time.Now().Add(failureBackoffTTL))
+                if publishedAt == nil || publishedAt.IsZero() {
+                    if retryErr := r.applyRetryAfter(tracking, ctx, candidate, time.Now().Add(failureBackoffTTL), false); retryErr != nil {
+                        r.logger.Error("published_at_resolver_retry_after_write_failed",
+                            slog.String("kind", string(candidate.Kind)),
+                            slog.String("post_id", candidate.PostID),
+                            slog.Any("error", retryErr),
+                        )
+                    }
                     observePublishedAtResolverSkipped(candidate.Kind, "published_at_empty")
                     continue
                 }
@@
-                if err != nil {
-                    _ = tracking.MarkPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID, time.Now().Add(failureBackoffTTL))
+                if err != nil {
+                    if retryErr := r.applyRetryAfter(tracking, ctx, candidate, time.Now().Add(failureBackoffTTL), false); retryErr != nil {
+                        r.logger.Error("published_at_resolver_retry_after_write_failed",
+                            slog.String("kind", string(candidate.Kind)),
+                            slog.String("post_id", candidate.PostID),
+                            slog.Any("error", retryErr),
+                        )
+                    }
                     r.logger.Warn("Pending published_at resolver failed to finalize candidate",
                         ...
                     )
                     continue
                 }
```

### 권장 추가 metric

metric 파일이 이미 있다면 다음 카운터를 추가한다.

- `published_at_resolver_retry_after_write_failed_total{kind}`
- `published_at_resolver_backfilled_after_send_total{kind}`
- `published_at_resolver_backfilled_after_authorize_total{kind}`

이건 운영 중 “왜 같은 candidate를 자꾸 다시 긁는가?”를 보기 위해 반드시 필요하다.

---

## 8. P0-4 수정안: repo 기본 poll interval이 budget을 넘는 문제를 fail-fast로 막을 것

### 8.1 현재 숫자

현재 기본값 그대로면:

- notification 12개
- stats 111개
- poller RPM ≈ **26.31**
- resolver max RPM ≈ **4.00**
- combined max RPM ≈ **30.31**
- budget RPM = **20.00**

즉, 현재 repo 기본값은 **“기본값만으로도 과부하”**다.

이건 단순 경고로 끝내면 안 된다.

### 8.2 수정 원칙

1. **critical path인 poller budget 초과는 startup error**로 바꾼다.
2. resolver budget은 metadata backfill이므로 일단 warning 유지 가능하다.
3. 동시에 repo 기본 poll interval도 production-safe한 값으로 조정한다.

### 8.3 수정 파일 6
`hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go`

현재 `logCombinedYouTubeScraperBudget(...)`는 로그만 찍는다.  
이걸 “요약값 반환 + 검증”으로 바꾸는 것이 맞다.

```diff
diff --git a/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go b/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go
@@
-func logCombinedYouTubeScraperBudget(
+type youtubeScraperBudgetSummary struct {
+    PollerRPM                  float64
+    PollerRetryAmplifiedRPM    float64
+    ResolverRPM                float64
+    ResolverRetryAmplifiedRPM  float64
+    CombinedRPM                float64
+    CombinedRetryAmplifiedRPM  float64
+    BudgetRPM                  float64
+}
+
+func summarizeCombinedYouTubeScraperBudget(
     scraperCfg config.ScraperConfig,
     registrations []providers.ChannelPollerRegistration,
     logger *slog.Logger,
-) {
+) youtubeScraperBudgetSummary {
@@
-    if logger == nil {
-        return
-    }
+    summary := youtubeScraperBudgetSummary{
+        PollerRPM:                 pollerRPM,
+        PollerRetryAmplifiedRPM:   pollerRetryAmplifiedRPM,
+        ResolverRPM:               resolverRPM,
+        ResolverRetryAmplifiedRPM: resolverRetryAmplifiedRPM,
+        CombinedRPM:               combinedRPM,
+        CombinedRetryAmplifiedRPM: combinedRetryAmplifiedRPM,
+        BudgetRPM:                 budgetRPM,
+    }
+    if logger == nil {
+        return summary
+    }
@@
-}
+    return summary
+}
+
+func validateCriticalYouTubeScraperBudget(summary youtubeScraperBudgetSummary) error {
+    if summary.PollerRPM > summary.BudgetRPM {
+        return fmt.Errorf(
+            "configured poller RPM %.2f exceeds scraper budget %.2f; adjust SCRAPER_POLL_* intervals or target counts",
+            summary.PollerRPM,
+            summary.BudgetRPM,
+        )
+    }
+    return nil
+}
```

그리고 `buildStreamIngesterYouTubeComponents(...)`에서:

```diff
diff --git a/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go b/hololive-stream-ingester/internal/runtime/stream_ingester_runtime_builder_helpers.go
@@
-    logCombinedYouTubeScraperBudget(scraperCfg, pollerRegistrations, logger)
+    budgetSummary := summarizeCombinedYouTubeScraperBudget(scraperCfg, pollerRegistrations, logger)
+    if err := validateCriticalYouTubeScraperBudget(budgetSummary); err != nil {
+        return nil, nil, nil, err
+    }
```

이렇게 해야 unsafe defaults가 조용히 production에 들어가지 않는다.

### 8.4 수정 파일 7
`hololive-shared/pkg/config/config_types.go`

기본값도 손봐야 한다.  
정확한 숫자는 운영 의도에 맞춰야 하지만, **현재 1분/1분은 명백히 공격적**이다.

현재 구조에서 12 notification target과 resolver 기본 4 RPM까지 감안하면, shorts/community는 각각 최소 **약 105초 이상**이어야 20 RPM budget 안에 들어온다.  
resolver를 critical path에서 제거한다 해도 poller 자체 budget만 맞추려면 각각 **약 81초 이상**은 필요하다.

따라서 1분/1분은 그대로 두면 안 된다.

권장 예시:

```diff
diff --git a/hololive-shared/pkg/config/config_types.go b/hololive-shared/pkg/config/config_types.go
@@
 func DefaultScraperPoll() ScraperPoll {
     return ScraperPoll{
         Videos:    15 * time.Minute,
-        Shorts:    1 * time.Minute,
-        Community: 1 * time.Minute,
+        Shorts:    2 * time.Minute,
+        Community: 2 * time.Minute,
         Stats:     6 * time.Hour,
         Live:      10 * time.Minute,
     }
 }
```

중요한 점은 “정확히 2분이어야 한다”가 아니다.  
핵심은 **기본값이 budget-safe해야 한다**는 것이다.

운영에서 더 빠른 poll을 원하면 env override를 하되, 그때는 startup budget validation이 그 값을 다시 검증해야 한다.

### 관련 테스트

`hololive-shared/pkg/config/config_test.go`의 기본값 테스트를 같이 바꾼다.

추가로 `stream_ingester_runtime_builder_helpers_test.go`에 다음 테스트를 추가한다.

- `TestBuildStreamIngesterYouTubeComponents_FailsWhenPollerBudgetExceedsRateLimit`
- `TestBuildStreamIngesterYouTubeComponents_AllowsBudgetSafePollerConfig`

---

## 9. P1-1 권장 후속: resolver를 scheduler 안으로 넣어 fairness를 강제

이건 P0는 아니다.  
위 4~8만 해도 사용자-facing 지연은 크게 줄어든다.

하지만 더 완전하게 가려면 resolver를 scheduler 밖 별도 goroutine으로 두지 말고, scheduler 안의 low-priority synthetic poller로 넣는 것이 낫다.

### 왜 필요한가

지금은 resolver와 poller가 shared rate limiter를 공유하지만, 실행 주체는 완전히 별개다.

- poller: scheduler queue
- resolver: 별도 timer loop

즉, “누가 먼저 budget token을 가져갈지”가 scheduler priority로 통제되지 않는다.

### 권장 구조

1. `PendingPublishedAtResolver`에 `RunOnce(ctx)`를 export
2. `PublishedAtResolverPoller`를 새로 만들어 `Poll(ctx, _ string)`에서 `resolver.RunOnce(ctx)` 호출
3. `ChannelTargetGroupGlobal` 추가
4. synthetic channel ID 하나(`"__published_at_resolver__"`)로 registration
5. `startBackgroundServices()`에서 별도 `go r.PublishedAtResolver.Start(ctx)` 제거
6. `youtube_poll_target_refresh.go`는 `TargetGroupGlobal`은 skip

### 수정 파일 후보

- `hololive-shared/pkg/providers/scraper_scheduler_options.go`
- `hololive-shared/pkg/service/youtube/poller/published_at_resolver.go`
- `hololive-shared/pkg/service/youtube/poller/published_at_resolver_poller.go` (신규)
- `hololive-stream-ingester/internal/runtime/stream_ingester_poller_registrations.go`
- `hololive-stream-ingester/internal/runtime/youtube_poll_target_refresh.go`
- `hololive-stream-ingester/internal/runtime/stream_ingester_runtime_lifecycle.go`

이건 변경 폭이 크므로 **P0 적용 후** 진행하는 것이 맞다.

---

## 10. P2 latent smell 정리

### 10.1 additive cache warm

`hololive-shared/pkg/service/alarm/cache_warm.go`의 `WarmSubscriberCacheFromRepository()`는 이름과 달리 authoritative rebuild가 아니라 additive warm이다.

즉, stale positive cache를 자동으로 정리하지 못한다.

현재는 kakao-bot의 add/remove path가 cache를 동기 업데이트하고, stream-ingester startup poll target은 DB-authoritative라서 예전처럼 P0는 아니다.  
그래도 장기적으로는 다음을 추가하는 것이 좋다.

- `RebuildSubscriberCacheFromRepository(ctx, cacheSvc, repo)` 신설
- admin task 또는 startup maintenance에서만 사용
- normal request path는 기존 warm 유지

### 10.2 member.ServiceAdapter empty-slice fallback

`hololive-shared/pkg/service/member/adapter.go`

```go
members, err := a.cache.repo.GetAllMembers(a.ctx)
if err != nil {
    a.logger.Warn(...)
    return []*domain.Member{}
}
```

현재 stream-ingester poll target은 repo-authoritative라 critical path는 아니다.  
하지만 이 패턴은 여전히 “오류를 빈 값으로 위장”하는 냄새다.

권장안은 두 가지 중 하나다.

1. `GetAllMembersE(ctx) ([]*domain.Member, error)` 같은 error-returning API 추가
2. adapter 사용처 중 critical path는 전부 repository interface를 직접 받도록 축소

지금 당장 blocking 이슈는 아니므로 P2로 둔다.

---

## 11. 테스트 추가/교체 체크리스트

### 반드시 추가/수정할 테스트

#### shorts/community immediate enqueue 관련
- `TestShortsPoller_EnqueuesImmediatelyWhenRouteDeciderNilEvenIfPublishedAtMissing`
- `TestCommunityPoller_EnqueuesImmediatelyWhenRouteDeciderNilEvenIfPublishedAtMissing`
- `TestShortsPoller_KeepsWatermarkWhenRouteDeciderRequiresPublishedAtAndPublishedAtMissing`
- `TestCommunityPoller_KeepsWatermarkWhenRouteDeciderRequiresPublishedAtAndPublishedAtMissing`

#### resolver metadata backfill 관련
- `TestPendingPublishedAtResolver_BackfillsAlreadyAuthorizedRowWithoutReenqueuing`
- `TestPendingPublishedAtResolver_BackfillsAlreadySentRowWithoutReenqueuing`
- `TestPendingPublishedAtResolver_ClearsRetryAfterAfterMetadataOnlyBackfill`

#### resolver cancel / retry_after 관련
- 기존 `TestPendingPublishedAtResolver_CancelDuringCandidateResolutionSetsRetryAfter` 제거 또는 rename
- 새 테스트:
  - `TestPendingPublishedAtResolver_CancelDuringCandidateResolutionDoesNotSetRetryAfter`
  - `TestPendingPublishedAtResolver_LogsRetryAfterWriteFailure`
  - `TestPendingPublishedAtResolver_FinalizeFailureLogsRetryAfterWriteFailure`

#### budget validation 관련
- `TestBuildStreamIngesterYouTubeComponents_FailsWhenPollerBudgetExceedsRateLimit`
- `TestSummarizeCombinedYouTubeScraperBudget_ReportsCurrentResolvedRPM`
- `TestConfigLoad_DefaultScraperPollIsBudgetSafeForReferenceTopology`  
  이 테스트는 topology를 박아도 되고, builder helper test에서 registration 기반으로 검증해도 된다.

---

## 12. 적용 순서

가장 안전한 순서는 다음이다.

### 1단계
P0-1 1차 적용

- `stream_ingester_poller_registrations.go`
- `pollers.go`

즉, `routeDecider == nil`이면 즉시 enqueue.

### 2단계
P0-1 2차 적용

- `alarm_state_repository.go`
- `published_at_resolver_repository.go`

즉, resolver를 metadata backfill capable하게 만들기.

### 3단계
P0-2 / P0-3 적용

- `published_at_resolver.go`
- 관련 테스트 교체

즉, shutdown cancel과 retry_after write failure 정리.

### 4단계
P0-4 적용

- `stream_ingester_runtime_builder_helpers.go`
- `config_types.go`
- 관련 테스트

즉, budget fail-fast와 safer defaults.

### 5단계
P1 진행 여부 판단

- 운영에서 resolver backlog, budget warning, retry_after write failure가 아직 남으면
- resolver를 scheduler 안으로 넣는 구조 변경

---

## 13. 배포 후 반드시 볼 로그/메트릭

### 기대 로그

- `Resolved YouTube poll targets`  
  - `notification_target_channels`
  - `stats_target_channels`
  - `dropped_alarm_targets`
- `youtube_scraper_combined_budget_summary`
- `published_at_resolver_configured`
- `youtube_poll_target_refresh_db_validated`

### 배포 후 반드시 사라져야 하는 것

- `youtube_scraper_combined_budget_exceeds_rate_limit`  
  repo defaults까지 조정했다면 기본 환경에서는 사라져야 한다.
- community/shorts 신규 item의 첫 알림이 40~60초대인 현상  
  routeDecider가 nil이면 이제 거의 poller 감지 시간 + outbox 0~2초 수준으로 줄어야 한다.

### 새로 추가해 볼 메트릭

- `published_at_resolver_retry_after_write_failed_total`
- `published_at_resolver_backfilled_after_send_total`
- `published_at_resolver_backfilled_after_authorize_total`
- `youtube_alert_deferred_to_resolver_total{kind,route_required}`
  - `route_required=false`가 계속 증가하면 다시 버그다.

---

## 14. 최종 결론

이번 번들은 예전처럼 “전체 채널을 다 긁는다”거나 “worktree만 고쳐서 실제 배포는 옛 코드가 돈다”는 식의 거친 문제는 많이 정리됐다.

하지만 아직 가장 중요한 버그가 남아 있다.

**shorts/community에서 `routeDecider == nil`이어도 `published_at` resolver가 알림의 필수 경로가 되는 구조**다.

이건 지금 남은 유튜브 알림 지연의 본질이다.

그리고 그 뒤에

- shutdown cancel을 timeout처럼 취급하는 resolver
- retry_after write 실패 무시
- budget 초과를 warning으로만 남기는 config/runtime
- resolver의 scheduler 밖 실행

이 이어져 있다.

따라서 이번 패치의 최우선 순위는 다음 네 줄로 요약된다.

1. **routeDecider가 없으면 published_at 없이 즉시 enqueue**
2. **resolver는 metadata backfill까지 계속 하도록 query/finalize를 확장**
3. **shutdown cancel과 real timeout을 분리하고 retry_after write failure를 표면화**
4. **budget 초과는 startup error로 바꿔 기본값부터 안전하게 만들 것**

이 네 가지를 적용하면, 이번 번들의 남은 “AI 냄새 / I/O 병목 / 성능 병목 / 유튜브 알림 지연” 중 가장 큰 것들은 실질적으로 정리된다.
