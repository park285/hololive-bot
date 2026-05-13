# Phase 02. 채널/source별 adaptive backoff와 operation guard

## 목표

현재 `BackoffState`는 전역 hard/transient cooldown을 관리합니다. 이건 유지해야 합니다. 다만 parser drift나 특정 채널 timeout은 전역 문제로 보면 안 됩니다.

이 phase에서는 channel/source 단위 health를 추가합니다.

예:

- `UCxxxx + html`: parser drift 3회 → 다음 HTML poll 40분 후
- `UCyyyy + rss`: XML parse 실패 2회 → RSS source만 일시 cooldown
- 403/429: 기존 전역 hard backoff 유지

## 코드 레벨 의사결정

1. 403/429는 기존 `BackoffState`의 전역 hard cooldown을 유지합니다.
2. parser drift/timeout/transport/http_status는 channel/source health로 관리합니다.
3. scheduler는 이미 `RetryDelay()`를 이해하므로 `CooldownError`를 재사용합니다.
4. operation 레벨 helper를 둡니다. fetcher 레벨은 operation/stage 정보를 모릅니다.

## 변경 대상

- `scraper/channel_health.go` 신규
- `scraper/client_operation_guard.go` 신규
- `scraper/client_options.go` 수정
- `scraper/state_manager.go` 수정 또는 `initStateManagers()` 확장
- `scraper/videos.go` upcoming 함수에 guard 적용

## Diff

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/channel_health.go b/hololive/hololive-shared/pkg/service/youtube/scraper/channel_health.go
new file mode 100644
index 0000000..4444444
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/channel_health.go
@@
+package scraper
+
+import (
+    "context"
+    "fmt"
+    "log/slog"
+    "strings"
+    "time"
+)
+
+type ChannelSourceHealth struct {
+    ChannelID           string        `json:"channel_id"`
+    Source              FailureSource `json:"source"`
+    ConsecutiveFailures int           `json:"consecutive_failures"`
+    LastFailureReason   FailureReason `json:"last_failure_reason"`
+    LastFailureAt       time.Time     `json:"last_failure_at"`
+    LastSuccessAt       time.Time     `json:"last_success_at"`
+    NextAllowedAt       time.Time     `json:"next_allowed_at"`
+}
+
+type ChannelHealthPolicy struct {
+    TTL               time.Duration
+    ParserDriftBase   time.Duration
+    ParserDriftMax    time.Duration
+    TransportBase     time.Duration
+    TransportMax      time.Duration
+    TimeoutBase       time.Duration
+    TimeoutMax        time.Duration
+    HTTPStatusBase    time.Duration
+    HTTPStatusMax     time.Duration
+    SuccessDecaySteps int
+}
+
+func DefaultChannelHealthPolicy() ChannelHealthPolicy {
+    return ChannelHealthPolicy{
+        TTL:               24 * time.Hour,
+        ParserDriftBase:   10 * time.Minute,
+        ParserDriftMax:    6 * time.Hour,
+        TransportBase:     2 * time.Minute,
+        TransportMax:      30 * time.Minute,
+        TimeoutBase:       2 * time.Minute,
+        TimeoutMax:        30 * time.Minute,
+        HTTPStatusBase:    5 * time.Minute,
+        HTTPStatusMax:     1 * time.Hour,
+        SuccessDecaySteps: 1,
+    }
+}
+
+type ChannelHealthStore struct {
+    store  stateStore
+    policy ChannelHealthPolicy
+}
+
+func NewChannelHealthStore(store stateStore, policy ChannelHealthPolicy) *ChannelHealthStore {
+    if policy.TTL <= 0 {
+        policy = DefaultChannelHealthPolicy()
+    }
+    if policy.SuccessDecaySteps <= 0 {
+        policy.SuccessDecaySteps = 1
+    }
+    return &ChannelHealthStore{store: store, policy: policy}
+}
+
+func (s *ChannelHealthStore) ShouldSkip(ctx context.Context, channelID string, source FailureSource, now time.Time) (time.Duration, bool) {
+    if s == nil || s.store == nil {
+        return 0, false
+    }
+    health, ok := s.Get(ctx, channelID, source)
+    if !ok || health.NextAllowedAt.IsZero() {
+        return 0, false
+    }
+    remaining := health.NextAllowedAt.Sub(now)
+    if remaining <= 0 {
+        return 0, false
+    }
+    return remaining, true
+}
+
+func (s *ChannelHealthStore) RecordSuccess(ctx context.Context, channelID string, source FailureSource, now time.Time) {
+    if s == nil || s.store == nil {
+        return
+    }
+    health, _ := s.Get(ctx, channelID, source)
+    health.ChannelID = strings.TrimSpace(channelID)
+    health.Source = source
+    health.LastSuccessAt = now
+    health.NextAllowedAt = time.Time{}
+
+    if health.ConsecutiveFailures > 0 {
+        health.ConsecutiveFailures -= s.policy.SuccessDecaySteps
+        if health.ConsecutiveFailures < 0 {
+            health.ConsecutiveFailures = 0
+        }
+    }
+    if health.ConsecutiveFailures == 0 {
+        health.LastFailureReason = FailureReasonNone
+    }
+
+    if err := s.store.Set(ctx, channelHealthStateKey(channelID, source), health, s.policy.TTL); err != nil {
+        slog.Warn("failed to persist youtube scraper channel health success",
+            "channel_id", channelID,
+            "source", source,
+            "error", err)
+    }
+}
+
+func (s *ChannelHealthStore) RecordFailure(ctx context.Context, channelID string, detail FailureDetail, now time.Time) time.Duration {
+    if s == nil || s.store == nil {
+        return 0
+    }
+
+    source := detail.Source
+    if source == "" {
+        source = FailureSourceHTML
+    }
+
+    health, _ := s.Get(ctx, channelID, source)
+    health.ChannelID = strings.TrimSpace(channelID)
+    health.Source = source
+    health.ConsecutiveFailures++
+    health.LastFailureReason = detail.Reason
+    health.LastFailureAt = now
+
+    delay := s.delayFor(detail.Reason, health.ConsecutiveFailures)
+    if detail.RetryAfter > delay {
+        delay = detail.RetryAfter
+    }
+    if delay > 0 {
+        health.NextAllowedAt = now.Add(delay)
+    }
+
+    if err := s.store.Set(ctx, channelHealthStateKey(channelID, source), health, s.policy.TTL); err != nil {
+        slog.Warn("failed to persist youtube scraper channel health failure",
+            "channel_id", channelID,
+            "source", source,
+            "reason", detail.Reason,
+            "error", err)
+    }
+    return delay
+}
+
+func (s *ChannelHealthStore) Get(ctx context.Context, channelID string, source FailureSource) (ChannelSourceHealth, bool) {
+    var health ChannelSourceHealth
+    if s == nil || s.store == nil {
+        return health, false
+    }
+    if err := s.store.Get(ctx, channelHealthStateKey(channelID, source), &health); err != nil {
+        return health, false
+    }
+    return health, strings.TrimSpace(health.ChannelID) != ""
+}
+
+func (s *ChannelHealthStore) delayFor(reason FailureReason, failures int) time.Duration {
+    if failures <= 0 {
+        return 0
+    }
+    var base, maxDelay time.Duration
+    switch reason {
+    case FailureReasonParserDrift:
+        base, maxDelay = s.policy.ParserDriftBase, s.policy.ParserDriftMax
+    case FailureReasonTransport:
+        base, maxDelay = s.policy.TransportBase, s.policy.TransportMax
+    case FailureReasonTimeout:
+        base, maxDelay = s.policy.TimeoutBase, s.policy.TimeoutMax
+    case FailureReasonHTTPStatus:
+        base, maxDelay = s.policy.HTTPStatusBase, s.policy.HTTPStatusMax
+    default:
+        return 0
+    }
+    delay := base
+    for i := 1; i < failures; i++ {
+        delay *= 2
+        if delay >= maxDelay {
+            return maxDelay
+        }
+    }
+    return delay
+}
+
+func channelHealthStateKey(channelID string, source FailureSource) string {
+    return fmt.Sprintf(
+        "youtube:scraper:channel-health:%s:%s",
+        strings.TrimSpace(string(source)),
+        strings.TrimSpace(channelID),
+    )
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/client_operation_guard.go b/hololive/hololive-shared/pkg/service/youtube/scraper/client_operation_guard.go
new file mode 100644
index 0000000..5555555
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/client_operation_guard.go
@@
+package scraper
+
+import (
+    "context"
+    "fmt"
+    "strings"
+    "time"
+)
+
+func (c *Client) ensureChannelSourceAllowed(ctx context.Context, channelID string, source FailureSource) error {
+    if c == nil || c.channelHealth == nil {
+        return nil
+    }
+    wait, ok := c.channelHealth.ShouldSkip(ctx, channelID, source, time.Now())
+    if !ok {
+        return nil
+    }
+    return &CooldownError{
+        Kind:  fmt.Sprintf("youtube channel-source %s", source),
+        Delay: wait,
+        Err:   ErrTransientCooldown,
+    }
+}
+
+func (c *Client) fetchChannelSourcePage(
+    ctx context.Context,
+    operation string,
+    channelID string,
+    pageURL string,
+    source FailureSource,
+    policy ...FetchPolicy,
+) (string, error) {
+    if err := c.ensureChannelSourceAllowed(ctx, channelID, source); err != nil {
+        return "", err
+    }
+
+    html, err := c.fetchPage(ctx, pageURL, policy...)
+    if err != nil {
+        detail := ClassifyFailure(err, source)
+        c.recordChannelSourceFailure(ctx, channelID, detail)
+        return "", err
+    }
+
+    if strings.TrimSpace(html) == "" {
+        err := fmt.Errorf("%s empty response from %s", operation, pageURL)
+        detail := ClassifyFailure(err, source)
+        c.recordChannelSourceFailure(ctx, channelID, detail)
+        return "", err
+    }
+
+    return html, nil
+}
+
+func (c *Client) recordChannelSourceSuccess(ctx context.Context, channelID string, source FailureSource) {
+    if c == nil || c.channelHealth == nil {
+        return
+    }
+    c.channelHealth.RecordSuccess(ctx, channelID, source, time.Now())
+}
+
+func (c *Client) recordChannelSourceFailure(ctx context.Context, channelID string, detail FailureDetail) time.Duration {
+    if c == nil || c.channelHealth == nil {
+        return 0
+    }
+    return c.channelHealth.RecordFailure(ctx, channelID, detail, time.Now())
+}
+
+func (c *Client) recordParserDrift(
+    ctx context.Context,
+    operation string,
+    stage string,
+    channelID string,
+    pageURL string,
+    source FailureSource,
+    html string,
+    cause error,
+) error {
+    err := NewParserDriftError(operation, stage, cause)
+    detail := ClassifyFailure(err, source)
+    c.recordChannelSourceFailure(ctx, channelID, detail)
+    return err
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go b/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go
index 8b96f25..ccccccc 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/client_options.go
@@
     stateStore       stateStore
     fetcherEngine    FetcherEngine
+    channelHealthPolicy ChannelHealthPolicy
+    channelHealth    *ChannelHealthStore

     communityMissing *cacheState
     videoRSSBackoff  *cacheState
@@
 func WithFetcherEngine(engine FetcherEngine) ClientOption {
     return func(c *Client) {
         c.fetcherEngine = normalizeFetcherEngine(engine)
     }
 }
+
+func WithChannelHealthPolicy(policy ChannelHealthPolicy) ClientOption {
+    return func(c *Client) {
+        c.channelHealthPolicy = policy
+    }
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/state_manager.go b/hololive/hololive-shared/pkg/service/youtube/scraper/state_manager.go
index ab1a8f7..ddddddd 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/state_manager.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/state_manager.go
@@
 func (c *Client) initStateManagers() {
     if c == nil {
         return
     }
     c.communityMissing = newCacheState(c.stateStore, constants.YouTubeConfig.CommunityMissingTTL, "community missing")
     c.videoRSSBackoff = newCacheState(c.stateStore, constants.YouTubeConfig.VideoRSSBackoffTTL, "video rss backoff")
+    c.channelHealth = NewChannelHealthStore(c.stateStore, c.channelHealthPolicy)
 }
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go b/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
index bbbbbbb..eeeeeee 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
@@
 func (c *Client) GetUpcomingEvents(ctx context.Context, channelID string) ([]*UpcomingEvent, error) {
     url := fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)

-    html, err := c.fetchPage(ctx, url)
+    html, err := c.fetchChannelSourcePage(ctx, "upcoming_events", channelID, url, FailureSourceHTML)
     if err != nil {
         return nil, fmt.Errorf("failed to fetch channel page: %w", err)
     }
-    if strings.TrimSpace(html) == "" {
-        return nil, fmt.Errorf("empty response from channel page")
-    }

     jsonStr, err := extractYtInitialData(html)
     if err != nil {
         logStructureWarning("upcoming_events", channelID, "ytInitialData extraction failed", "error", err)
-        return nil, NewParserDriftError("upcoming_events", "extract_yt_initial_data", err)
+        return nil, c.recordParserDrift(ctx, "upcoming_events", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
     }

     data := gjson.Parse(jsonStr)
     events, err := parseUpcomingEventsFromInitialData(data)
     if err != nil {
         logStructureWarning("upcoming_events", channelID, "failed to parse initial data", "error", err)
-        return nil, NewParserDriftError("upcoming_events", "parse_initial_data", err)
+        return nil, c.recordParserDrift(ctx, "upcoming_events", "parse_initial_data", channelID, url, FailureSourceHTML, html, err)
     }
+    c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
     return events, nil
 }
```

## 테스트 추가

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/channel_health_test.go b/hololive/hololive-shared/pkg/service/youtube/scraper/channel_health_test.go
new file mode 100644
index 0000000..6666666
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/channel_health_test.go
@@
+package scraper
+
+import (
+    "context"
+    "testing"
+    "time"
+
+    "github.com/stretchr/testify/require"
+)
+
+type memoryStateStore struct {
+    values map[string]any
+}
+
+func newMemoryStateStore() *memoryStateStore {
+    return &memoryStateStore{values: map[string]any{}}
+}
+
+func (s *memoryStateStore) Get(_ context.Context, key string, dest any) error {
+    value, ok := s.values[key]
+    if !ok {
+        return context.Canceled // cacheState도 error를 not found처럼 취급하므로 테스트에서는 false 기대용
+    }
+    switch typed := dest.(type) {
+    case *ChannelSourceHealth:
+        *typed = value.(ChannelSourceHealth)
+    case *bool:
+        *typed = value.(bool)
+    }
+    return nil
+}
+
+func (s *memoryStateStore) Set(_ context.Context, key string, value any, _ time.Duration) error {
+    s.values[key] = value
+    return nil
+}
+
+func (s *memoryStateStore) Del(_ context.Context, key string) error {
+    delete(s.values, key)
+    return nil
+}
+
+func TestChannelHealthRecordFailureBackoff(t *testing.T) {
+    store := newMemoryStateStore()
+    health := NewChannelHealthStore(store, ChannelHealthPolicy{
+        TTL:               time.Hour,
+        ParserDriftBase:   10 * time.Minute,
+        ParserDriftMax:    time.Hour,
+        SuccessDecaySteps: 1,
+    })
+    now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
+
+    delay1 := health.RecordFailure(context.Background(), "UCxxx", FailureDetail{
+        Source: FailureSourceHTML,
+        Reason: FailureReasonParserDrift,
+    }, now)
+    delay2 := health.RecordFailure(context.Background(), "UCxxx", FailureDetail{
+        Source: FailureSourceHTML,
+        Reason: FailureReasonParserDrift,
+    }, now)
+
+    require.Equal(t, 10*time.Minute, delay1)
+    require.Equal(t, 20*time.Minute, delay2)
+
+    remaining, skip := health.ShouldSkip(context.Background(), "UCxxx", FailureSourceHTML, now.Add(time.Minute))
+    require.True(t, skip)
+    require.Greater(t, remaining, 0)
+}
+
+func TestChannelHealthSuccessDecaysFailureCount(t *testing.T) {
+    store := newMemoryStateStore()
+    health := NewChannelHealthStore(store, ChannelHealthPolicy{
+        TTL:               time.Hour,
+        ParserDriftBase:   10 * time.Minute,
+        ParserDriftMax:    time.Hour,
+        SuccessDecaySteps: 1,
+    })
+    now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
+
+    _ = health.RecordFailure(context.Background(), "UCxxx", FailureDetail{
+        Source: FailureSourceHTML,
+        Reason: FailureReasonParserDrift,
+    }, now)
+    health.RecordSuccess(context.Background(), "UCxxx", FailureSourceHTML, now.Add(time.Minute))
+
+    got, ok := health.Get(context.Background(), "UCxxx", FailureSourceHTML)
+    require.True(t, ok)
+    require.Equal(t, 0, got.ConsecutiveFailures)
+    require.True(t, got.NextAllowedAt.IsZero())
+}
```

테스트에서 `memoryStateStore.Get`의 not-found error는 실제 cache client의 error 모양과 다를 수 있습니다. 실제 repo의 cache mock이 있다면 그걸 쓰는 편이 더 좋습니다.

## 실행

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper -run 'TestChannelHealth|TestClassifyFailure'
```

## 완료 기준

- parser drift 발생 채널은 같은 source에 대해 다음 poll delay가 적용됩니다.
- 성공하면 failure count가 줄어듭니다.
- 403/429 전역 backoff와 channel health가 서로 섞이지 않습니다.
- scheduler는 `CooldownError.RetryDelay()`로 자연스럽게 next run을 늦춥니다.
