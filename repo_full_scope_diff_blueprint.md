
# 전체 범위 diff 설계서

이 문서는 이번 저장소 아카이브 전체를 기준으로, 이전에 지적했던 모든 잔존 이슈를 **축소 없이** 한 번에 닫기 위한 patch 청사진이다.  
범위는 다음을 모두 포함한다.

1. `hololive-kakao-bot-go` 알람 의미론과 runtime loop
2. `hololive-stream-ingester` / `youtube-scraper` cadence, rate-limit budget, scheduler semantics
3. `hololive-shared` provider 정리, runtime/server helper 정리, settings/provider 중복 제거
4. `shared-go` lifecycle 중복 정리
5. `build-all.sh`, `docker-compose.prod.yml`, deploy script, Dockerfile, architecture script를 포함한 self-contained build chain
6. `admin-dashboard` backend typed contract, OpenAPI export, frontend generated client 전환, 대형 page 컴포넌트 분리
7. 관련 테스트와 문서/거버넌스 정리

이 문서는 "어디를 어떻게 고칠지"가 아니라, **바로 작업을 시작할 수 있는 diff 단위 설계서**다.  
다만 이 문서 초안 시점에는 `go.work`가 `go 1.26.1`을 요구하고 로컬 toolchain은 그보다 낮아 전체 `go test ./...`를 끝까지 검증하지는 못했다. 현재 저장소 기준 toolchain target은 `1.26.2`다. 따라서 아래 내용은 **현재 업로드된 코드 구조를 기준으로 맞춘 repo-wide patch blueprint**다.

---

## 0. 적용 순서

이 순서대로 넣는 것이 가장 안전하다.

1. 알람 의미론 보정 (`CrossedTarget` late-backfill 제거)
2. scraper default cadence / env alias / budget warning
3. self-contained build chain 정리 (`IRIS_CLIENT_GO_PATH`, 외부 `../llm/shared-go` 기본값 제거)
4. shared runtime/server helper 추가 후 bot/stream/llm router 중복 제거
5. lifecycle `Close()` wrapper 제거
6. runtime-owned provider/module 중복 제거
7. admin-dashboard backend typed holo contract + OpenAPI export
8. frontend generated client 전환 + page decomposition
9. docs / governance / tests 마감

---

## 1. 알람 의미론 수정: stale-window backfill 제거

현재 문제는 `sharedchecker.CrossedTarget(...)`가 `prevCheckedAt ~ now` 전체를 그대로 신뢰한다는 점이다.  
tier scheduler가 5분마다 채널을 다시 볼 수 있기 때문에, **처음 발견한 시점이 시작 4분 전인데도 5분 알람을 지금 보내는** 잘못된 backfill이 가능하다.

해결 원칙은 단순하다.

- "이전 체크 시각"을 그대로 쓰지 말고
- **현재 runtime cadence 기준으로 lookback을 상한**으로 제한하고
- 그 bounded window 안에서만 가장 큰 target minute를 선택한다.

### 1-1. `hololive/hololive-shared/pkg/service/alarm/checker/helpers.go`

```diff
--- a/hololive/hololive-shared/pkg/service/alarm/checker/helpers.go
+++ b/hololive/hololive-shared/pkg/service/alarm/checker/helpers.go
@@
 func IsTargetMinute(targetMinutes []int, minutesUntil int) bool {
 	return slices.Contains(targetMinutes, minutesUntil)
 }

-func CrossedTarget(targetMinutes []int, start, prev, now time.Time) (int, bool) {
-	current := MinutesUntilFloor(start, now)
-	if IsTargetMinute(targetMinutes, current) {
-		return current, true
-	}
-
-	if prev.IsZero() || !prev.Before(now) {
-		return 0, false
-	}
-
-	previous := MinutesUntilFloor(start, prev)
-	if previous <= current {
-		return 0, false
-	}
-
-	for _, target := range targetMinutes {
-		if current < target && target <= previous {
-			return target, true
-		}
-	}
-
-	return 0, false
-}
+type EvaluationWindow struct {
+	Start time.Time
+	End   time.Time
+}
+
+// ResolveEvaluationWindow는 이전 체크 시각을 그대로 신뢰하지 않고,
+// runtime cadence 기준 maxLookback 범위 안에서만 평가 윈도를 만든다.
+func ResolveEvaluationWindow(prevCheckedAt, now time.Time, maxLookback time.Duration) EvaluationWindow {
+	if now.IsZero() {
+		now = time.Now().UTC()
+	} else {
+		now = now.UTC()
+	}
+
+	if maxLookback <= 0 {
+		maxLookback = time.Minute
+	}
+
+	windowStart := now.Add(-maxLookback)
+	if !prevCheckedAt.IsZero() {
+		prevUTC := prevCheckedAt.UTC()
+		if prevUTC.After(windowStart) && prevUTC.Before(now) {
+			windowStart = prevUTC
+		}
+	}
+
+	// lower bound와 upper bound가 같아지면 crossed target 계산이 무의미해지므로
+	// 최소 1초 윈도는 유지한다.
+	if !windowStart.Before(now) {
+		windowStart = now.Add(-time.Second)
+	}
+
+	return EvaluationWindow{
+		Start: windowStart,
+		End:   now,
+	}
+}
+
+// HighestCrossedTarget는 bounded window 안에서 실제로 지난 가장 큰 target minute를 반환한다.
+func HighestCrossedTarget(targetMinutes []int, startScheduled time.Time, window EvaluationWindow) (int, bool) {
+	if startScheduled.IsZero() || !window.Start.Before(window.End) {
+		return 0, false
+	}
+
+	normalized := NormalizeTargetMinutes(targetMinutes)
+	for _, target := range normalized {
+		boundary := startScheduled.Add(-time.Duration(target) * time.Minute)
+		if boundary.After(window.Start) && !boundary.After(window.End) {
+			return target, true
+		}
+	}
+
+	return 0, false
+}
```

### 1-2. `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/youtube_checker.go`

`YouTubeChecker`가 평가 윈도 상한을 알 수 있어야 한다.

```diff
--- a/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/youtube_checker.go
+++ b/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/youtube_checker.go
@@
 type YouTubeChecker struct {
 	cacheSvc        cache.Client
 	holodexSvc      *holodex.Service
 	tierScheduler   *tier.TieredScheduler
 	dedupSvc        *dedup.Service
 	targetMinutes   []int
 	targetMinutesMu sync.RWMutex
+	evaluationWindowCap time.Duration
 	logger          *slog.Logger
 }
@@
 func NewYouTubeChecker(
 	cacheSvc cache.Client,
 	holodexSvc *holodex.Service,
 	tierScheduler *tier.TieredScheduler,
 	dedupSvc *dedup.Service,
 	targetMinutes []int,
+	evaluationWindowCap time.Duration,
 	logger *slog.Logger,
 ) (*YouTubeChecker, error) {
@@
+	if evaluationWindowCap <= 0 {
+		evaluationWindowCap = 75 * time.Second
+	}
+
 	return &YouTubeChecker{
 		cacheSvc:      cacheSvc,
 		holodexSvc:    holodexSvc,
 		tierScheduler: tierScheduler,
 		dedupSvc:      dedupSvc,
-		targetMinutes: normalizeTargetMinutes(targetMinutes),
+		targetMinutes: sharedchecker.NormalizeTargetMinutes(targetMinutes),
+		evaluationWindowCap: evaluationWindowCap,
 		logger:        safeLogger(logger),
 	}, nil
 }
@@
 func (c *YouTubeChecker) UpdateTargetMinutes(targetMinutes []int) {
 	c.targetMinutesMu.Lock()
 	defer c.targetMinutesMu.Unlock()

-	c.targetMinutes = normalizeTargetMinutes(targetMinutes)
+	c.targetMinutes = sharedchecker.NormalizeTargetMinutes(targetMinutes)
 }
@@
 	prevCheckedAt time.Time,
 	now time.Time,
 ) ([]*domain.AlarmNotification, error) {
 	notifications := make([]*domain.AlarmNotification, 0, len(streams)*len(subscriberRooms))
+	window := sharedchecker.ResolveEvaluationWindow(prevCheckedAt, now, c.evaluationWindowCap)
 	for _, stream := range streams {
@@
-		upcomingNotifications, err := c.buildUpcomingNotifications(ctx, stream, subscriberRooms, prevCheckedAt, now)
+		upcomingNotifications, err := c.buildUpcomingNotifications(ctx, stream, subscriberRooms, window)
@@
 func (c *YouTubeChecker) buildUpcomingNotifications(
 	ctx context.Context,
 	stream *domain.Stream,
 	subscriberRooms []string,
-	prevCheckedAt time.Time,
-	now time.Time,
+	window sharedchecker.EvaluationWindow,
 ) ([]*domain.AlarmNotification, error) {
 	if stream == nil || !stream.IsUpcoming() || stream.StartScheduled == nil {
 		return nil, nil
 	}

-	if !stream.StartScheduled.After(now) {
+	if !stream.StartScheduled.After(window.End) {
 		return nil, nil
 	}

-	minutesUntil, ok := sharedchecker.CrossedTarget(c.targetMinutesSnapshot(), *stream.StartScheduled, prevCheckedAt, now)
+	minutesUntil, ok := sharedchecker.HighestCrossedTarget(c.targetMinutesSnapshot(), *stream.StartScheduled, window)
 	if !ok {
 		return nil, nil
 	}
```

### 1-3. `hololive-kakao-bot-go/internal/service/alarm/checker/common.go`

local wrapper를 삭제한다.

```diff
--- a/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/common.go
+++ b/hololive/hololive-kakao-bot-go/internal/service/alarm/checker/common.go
@@
-import (
+import (
 	"context"
 	"fmt"
 	"log/slog"
 	"sync"
 	"time"

 	"github.com/kapu/hololive-shared/pkg/domain"
-	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
 	"github.com/kapu/hololive-shared/pkg/service/cache"
 	"golang.org/x/sync/errgroup"
@@
-func normalizeTargetMinutes(targetMinutes []int) []int {
-	return sharedchecker.NormalizeTargetMinutes(targetMinutes)
-}
-
 func safeLogger(logger *slog.Logger) *slog.Logger {
```

### 1-4. `hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go`

`YouTubeChecker` constructor에 cadence-based evaluation cap을 넘긴다.  
동시에 local normalize wrapper도 제거한다.

```diff
--- a/hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go
+++ b/hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go
@@
-	targetMinutes := normalizeTargetMinutes(alarmSvc.GetTargetMinutes())
+	youtubeInterval := notifCfg.CheckInterval
+	if youtubeInterval <= 0 {
+		youtubeInterval = defaultYouTubeInterval
+	}
+
+	targetMinutes := sharedchecker.NormalizeTargetMinutes(alarmSvc.GetTargetMinutes())
@@
-	youtubeChecker, err := checker.NewYouTubeChecker(cacheSvc, holodexSvc, tierScheduler, dedupSvc, targetMinutes, logger)
+	youtubeChecker, err := checker.NewYouTubeChecker(
+		cacheSvc,
+		holodexSvc,
+		tierScheduler,
+		dedupSvc,
+		targetMinutes,
+		youtubeEvaluationWindowCap(youtubeInterval),
+		logger,
+	)
@@
-	youtubeInterval := notifCfg.CheckInterval
-	if youtubeInterval <= 0 {
-		youtubeInterval = defaultYouTubeInterval
-	}
-
 	return &RuntimeScheduler{
@@
-func normalizeTargetMinutes(targetMinutes []int) []int {
-	return sharedchecker.NormalizeTargetMinutes(targetMinutes)
+func youtubeEvaluationWindowCap(interval time.Duration) time.Duration {
+	if interval <= 0 {
+		interval = defaultYouTubeInterval
+	}
+	if interval < time.Minute {
+		return time.Minute + 15*time.Second
+	}
+	return interval + 15*time.Second
 }
```

### 1-5. 테스트 추가

`hololive/hololive-shared/pkg/service/alarm/checker/helpers_test.go`

```diff
--- a/hololive/hololive-shared/pkg/service/alarm/checker/helpers_test.go
+++ b/hololive/hololive-shared/pkg/service/alarm/checker/helpers_test.go
@@
 func TestNormalizeTargetMinutes(t *testing.T) {
@@
 }

-func TestCrossedTarget(t *testing.T) {
+func TestHighestCrossedTarget(t *testing.T) {
 	start := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

 	tests := []struct {
 		name    string
-		prev    time.Time
-		now     time.Time
+		window  EvaluationWindow
 		targets []int
 		want    int
 		wantOK  bool
 	}{
 		{
-			name:    "exact current target",
-			prev:    time.Date(2026, 4, 9, 11, 54, 10, 0, time.UTC),
-			now:     time.Date(2026, 4, 9, 11, 55, 0, 0, time.UTC),
+			name: "exact current target",
+			window: EvaluationWindow{
+				Start: time.Date(2026, 4, 9, 11, 54, 10, 0, time.UTC),
+				End:   time.Date(2026, 4, 9, 11, 55, 0, 0, time.UTC),
+			},
 			targets: []int{5, 3, 1},
 			want:    5,
 			wantOK:  true,
 		},
 		{
-			name:    "crossed 3 minute target",
-			prev:    time.Date(2026, 4, 9, 11, 56, 5, 0, time.UTC),
-			now:     time.Date(2026, 4, 9, 11, 57, 2, 0, time.UTC),
+			name: "crossed 3 minute target",
+			window: EvaluationWindow{
+				Start: time.Date(2026, 4, 9, 11, 56, 5, 0, time.UTC),
+				End:   time.Date(2026, 4, 9, 11, 57, 2, 0, time.UTC),
+			},
 			targets: []int{5, 3, 1},
 			want:    3,
 			wantOK:  true,
 		},
 		{
-			name:    "late discovery must not backfill 5-minute alarm",
-			prev:    time.Date(2026, 4, 9, 11, 51, 0, 0, time.UTC),
-			now:     time.Date(2026, 4, 9, 11, 56, 0, 0, time.UTC),
+			name: "late discovery must not backfill 5-minute alarm",
+			window: EvaluationWindow{
+				Start: time.Date(2026, 4, 9, 11, 54, 45, 0, time.UTC),
+				End:   time.Date(2026, 4, 9, 11, 56, 0, 0, time.UTC),
+			},
 			targets: []int{5, 3, 1},
 			want:    0,
 			wantOK:  false,
 		},
 	}

 	for _, tt := range tests {
 		t.Run(tt.name, func(t *testing.T) {
-			got, ok := CrossedTarget(tt.targets, start, tt.prev, tt.now)
+			got, ok := HighestCrossedTarget(tt.targets, start, tt.window)
 			if ok != tt.wantOK {
-				t.Fatalf("CrossedTarget() ok = %t, want %t", ok, tt.wantOK)
+				t.Fatalf("HighestCrossedTarget() ok = %t, want %t", ok, tt.wantOK)
 			}
 			if got != tt.want {
-				t.Fatalf("CrossedTarget() minute = %d, want %d", got, tt.want)
+				t.Fatalf("HighestCrossedTarget() minute = %d, want %d", got, tt.want)
 			}
 		})
 	}
 }
```

`hololive-kakao-bot-go/internal/service/alarm/checker/youtube_checker_test.go`에도 아래 케이스를 추가한다.

```go
func TestYouTubeChecker_DoesNotBackfillLateFiveMinuteAlarm(t *testing.T) {
	// given:
	// - stream start = 12:00
	// - prev checked at = 11:51
	// - current check at = 11:56
	// - evaluation cap = 75s
	// expect:
	// - 5분 알람은 나오면 안 됨
}
```

---

## 2. scraper cadence / env / budget 정리

현재 코드는 env wiring은 들어갔지만 기본 cadence가 여전히 rate-limit budget보다 공격적이다.  
`3초당 1회` limiter 기준 총 예산은 분당 약 20회다. 현재 기본 cadence는 채널당 약 `0.603 req/min` 수준이라 33채널 부근부터 backlog가 쌓인다.  
따라서 기본값을 **보수적으로 다시 잡고**, 새 env 이름으로 명확히 승격하며, 부팅 시 budget warning을 남겨야 한다.

### 2-1. `hololive/hololive-shared/pkg/config/config_types.go`

```diff
--- a/hololive/hololive-shared/pkg/config/config_types.go
+++ b/hololive/hololive-shared/pkg/config/config_types.go
@@
 func DefaultScraperWorkerCount() int {
-	return 2
+	return 4
 }
@@
 func DefaultScraperPoll() ScraperPoll {
 	return ScraperPoll{
-		Videos:    5 * time.Minute,
-		Shorts:    10 * time.Minute,
-		Community: 10 * time.Minute,
+		Videos:    15 * time.Minute,
+		Shorts:    30 * time.Minute,
+		Community: 30 * time.Minute,
 		Stats:     6 * time.Hour,
-		Live:      5 * time.Minute,
+		Live:      10 * time.Minute,
 	}
 }
+
+func (p ScraperPoll) EstimatedRequestsPerMinute() float64 {
+	var rpm float64
+	if p.Videos > 0 {
+		rpm += 60.0 / p.Videos.Seconds()
+	}
+	if p.Shorts > 0 {
+		rpm += 60.0 / p.Shorts.Seconds()
+	}
+	if p.Community > 0 {
+		rpm += 60.0 / p.Community.Seconds()
+	}
+	if p.Stats > 0 {
+		rpm += 60.0 / p.Stats.Seconds()
+	}
+	if p.Live > 0 {
+		rpm += 60.0 / p.Live.Seconds()
+	}
+	return rpm
+}
```

### 2-2. `hololive/hololive-shared/pkg/config/config.go`

새 env 이름을 primary로 승격하고, 구 env 이름은 alias fallback으로 유지한다.

```diff
--- a/hololive/hololive-shared/pkg/config/config.go
+++ b/hololive/hololive-shared/pkg/config/config.go
@@
 		Scraper: ScraperConfig{
 			ProxyEnabled: envutil.Bool("SCRAPER_PROXY_ENABLED", false),
 			ProxyURL:     envutil.String("SCRAPER_PROXY_URL", ""),
-			WorkerCount:  intEnv("SCRAPER_WORKER_COUNT", DefaultScraperWorkerCount()),
+			WorkerCount:  intAliasEnv([]string{
+				"SCRAPER_SCHEDULER_WORKER_COUNT",
+				"SCRAPER_WORKER_COUNT",
+			}, DefaultScraperWorkerCount()),
 			Poll:         loadScraperPoll(),
 		},
@@
 func loadScraperPoll() ScraperPoll {
 	defaults := DefaultScraperPoll()

 	return ScraperPoll{
-		Videos:    secondsEnv("SCRAPER_VIDEOS_SECONDS", defaults.Videos),
-		Shorts:    secondsEnv("SCRAPER_SHORTS_SECONDS", defaults.Shorts),
-		Community: secondsEnv("SCRAPER_COMMUNITY_SECONDS", defaults.Community),
-		Stats:     secondsEnv("SCRAPER_STATS_SECONDS", defaults.Stats),
-		Live:      secondsEnv("SCRAPER_LIVE_SECONDS", defaults.Live),
+		Videos: secondsAliasEnv([]string{
+			"SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS",
+			"SCRAPER_VIDEOS_SECONDS",
+		}, defaults.Videos),
+		Shorts: secondsAliasEnv([]string{
+			"SCRAPER_POLL_SHORTS_INTERVAL_SECONDS",
+			"SCRAPER_SHORTS_SECONDS",
+		}, defaults.Shorts),
+		Community: secondsAliasEnv([]string{
+			"SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS",
+			"SCRAPER_COMMUNITY_SECONDS",
+		}, defaults.Community),
+		Stats: secondsAliasEnv([]string{
+			"SCRAPER_POLL_STATS_INTERVAL_SECONDS",
+			"SCRAPER_STATS_SECONDS",
+		}, defaults.Stats),
+		Live: secondsAliasEnv([]string{
+			"SCRAPER_POLL_LIVE_INTERVAL_SECONDS",
+			"SCRAPER_LIVE_SECONDS",
+		}, defaults.Live),
 	}
 }

-func secondsEnv(key string, fallback time.Duration) time.Duration {
-	seconds := envutil.Int(key, int(fallback/time.Second))
-	if seconds <= 0 {
-		return fallback
-	}
-
-	return time.Duration(seconds) * time.Second
+func secondsAliasEnv(keys []string, fallback time.Duration) time.Duration {
+	for _, key := range keys {
+		seconds := envutil.Int(key, 0)
+		if seconds > 0 {
+			return time.Duration(seconds) * time.Second
+		}
+	}
+	return fallback
 }

-func intEnv(key string, fallback int) int {
-	value := envutil.Int(key, fallback)
-	if value <= 0 {
-		return fallback
+func intAliasEnv(keys []string, fallback int) int {
+	for _, key := range keys {
+		value := envutil.Int(key, 0)
+		if value > 0 {
+			return value
+		}
 	}
-
-	return value
+	return fallback
 }
```

### 2-3. `hololive/hololive-shared/pkg/providers/youtube_providers.go`

poll budget를 startup 시점에 계산해서 warning을 남긴다.

```diff
--- a/hololive/hololive-shared/pkg/providers/youtube_providers.go
+++ b/hololive/hololive-shared/pkg/providers/youtube_providers.go
@@
 import (
 	"fmt"
 	"log/slog"

 	"github.com/kapu/hololive-shared/pkg/config"
+	"github.com/kapu/hololive-shared/pkg/constants"
 	"github.com/kapu/hololive-shared/pkg/domain"
@@
 	// 모든 멤버 채널에 대해 폴러 등록
 	members := membersData.GetAllMembers()
+	activeMembers := 0
 	for _, m := range members {
 		if m.IsGraduated {
 			continue // 졸업 멤버 제외
 		}
+		activeMembers++
@@
 	logger.Info("Scraper scheduler initialized",
 		slog.Int("members", len(members)),
+		slog.Int("active_members", activeMembers),
 		slog.Int("poller_templates", len(channelPollerRegistrations)),
-		slog.Int("total_jobs", len(members)*len(channelPollerRegistrations)))
+		slog.Int("total_jobs", len(members)*len(channelPollerRegistrations)))
+
+	perChannelRPM := estimatedRequestsPerMinute(channelPollerRegistrations)
+	totalRPM := perChannelRPM * float64(activeMembers)
+	budgetRPM := 60.0 / constants.YouTubeScraperRateLimitConfig.RequestInterval.Seconds()
+	if totalRPM > budgetRPM {
+		logger.Warn("scraper_poll_budget_exceeds_rate_limit",
+			slog.Float64("per_channel_rpm", perChannelRPM),
+			slog.Float64("expected_total_rpm", totalRPM),
+			slog.Float64("budget_rpm", budgetRPM),
+			slog.Int("active_members", activeMembers),
+		)
+	}

 	return scheduler
 }
+
+func estimatedRequestsPerMinute(registrations []ChannelPollerRegistration) float64 {
+	var rpm float64
+	for _, registration := range registrations {
+		if registration.Interval <= 0 {
+			continue
+		}
+		rpm += 60.0 / registration.Interval.Seconds()
+	}
+	return rpm
+}
```

### 2-4. `hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go`

default worker count도 config 기본값과 맞춘다.

```diff
--- a/hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go
+++ b/hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go
@@
 type SchedulerConfig struct {
-	WorkerCount     int           // 동시 워커 수 (기본: 2)
+	WorkerCount     int           // 동시 워커 수 (기본: 4)
 	RequestInterval time.Duration // 요청 간 최소 간격 (기본: 4초)
 }
@@
 func DefaultSchedulerConfig() SchedulerConfig {
 	return SchedulerConfig{
-		WorkerCount:     2,
+		WorkerCount:     4,
 		RequestInterval: 4 * time.Second,
 	}
 }
```

### 2-5. `hololive/hololive-shared/pkg/constants/api.go`

stale sizing comment를 없앤다.

```diff
--- a/hololive/hololive-shared/pkg/constants/api.go
+++ b/hololive/hololive-shared/pkg/constants/api.go
@@
-	ScraperPhaseTimeout:   45 * time.Second, // 69채널 × 세마포어5 = 14 batch + 안전마진
+	ScraperPhaseTimeout:   45 * time.Second, // 느린 프록시 / 대형 배치에서도 한 phase가 영구 정지하지 않도록 제한
```

### 2-6. `docker-compose.prod.yml`

compose에는 새 env 이름만 노출한다. 구 이름은 code alias로만 남긴다.

```diff
--- a/docker-compose.prod.yml
+++ b/docker-compose.prod.yml
@@
-      SCRAPER_WORKER_COUNT: "${SCRAPER_WORKER_COUNT:-2}"
-      SCRAPER_VIDEOS_SECONDS: "${SCRAPER_VIDEOS_SECONDS:-300}"
-      SCRAPER_SHORTS_SECONDS: "${SCRAPER_SHORTS_SECONDS:-600}"
-      SCRAPER_COMMUNITY_SECONDS: "${SCRAPER_COMMUNITY_SECONDS:-600}"
-      SCRAPER_STATS_SECONDS: "${SCRAPER_STATS_SECONDS:-21600}"
-      SCRAPER_LIVE_SECONDS: "${SCRAPER_LIVE_SECONDS:-300}"
+      SCRAPER_SCHEDULER_WORKER_COUNT: "${SCRAPER_SCHEDULER_WORKER_COUNT:-4}"
+      SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS: "${SCRAPER_POLL_VIDEOS_INTERVAL_SECONDS:-900}"
+      SCRAPER_POLL_SHORTS_INTERVAL_SECONDS: "${SCRAPER_POLL_SHORTS_INTERVAL_SECONDS:-1800}"
+      SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS: "${SCRAPER_POLL_COMMUNITY_INTERVAL_SECONDS:-1800}"
+      SCRAPER_POLL_STATS_INTERVAL_SECONDS: "${SCRAPER_POLL_STATS_INTERVAL_SECONDS:-21600}"
+      SCRAPER_POLL_LIVE_INTERVAL_SECONDS: "${SCRAPER_POLL_LIVE_INTERVAL_SECONDS:-600}"
```

---

## 3. self-contained build chain: 외부 workspace 기본값 제거

현재 build/deploy/scripts는 `shared-go`는 거의 정리됐지만 `iris-client-go`와 일부 architecture script가 아직 외부 canonical workspace를 전제로 한다.  
최종 방향은 **기본 빌드 체인이 저장소 바깥 경로를 절대 요구하지 않도록** 만드는 것이다.

### 3-1. `build-all.sh`

```diff
--- a/build-all.sh
+++ b/build-all.sh
@@
 export SHARED_GO_WORKSPACE_PATH="$(resolve_shared_go_workspace_path)"
-
-resolve_iris_client_go_path() {
-    local candidate="${IRIS_CLIENT_GO_PATH:-${REPO_CANONICAL_ROOT}/../iris-client-go}"
-    if [ ! -d "$candidate" ]; then
-        echo "[ERROR] Active iris-client-go workspace not found: $candidate"
-        exit 1
-    fi
-    printf '%s\n' "$candidate"
-}
-
-export IRIS_CLIENT_GO_PATH="$(resolve_iris_client_go_path)"
@@
 echo "  COMPOSE_MODE=$COMPOSE_MODE"
 echo "  REMOTE_CACHE=$REMOTE_CACHE"
 echo "  SHARED_GO_WORKSPACE_PATH=$SHARED_GO_WORKSPACE_PATH"
```

### 3-2. `scripts/deploy/compose-redeploy-service.sh`

```diff
--- a/scripts/deploy/compose-redeploy-service.sh
+++ b/scripts/deploy/compose-redeploy-service.sh
@@
 export SHARED_GO_WORKSPACE_PATH="$(resolve_shared_go_workspace_path)"
-
-resolve_iris_client_go_path() {
-    local candidate="${IRIS_CLIENT_GO_PATH:-${REPO_CANONICAL_ROOT}/../iris-client-go}"
-    if [ ! -d "$candidate" ]; then
-        echo "[ERROR] Active iris-client-go workspace not found: $candidate"
-        exit 1
-    fi
-    printf '%s\n' "$candidate"
-}
-
-export IRIS_CLIENT_GO_PATH="$(resolve_iris_client_go_path)"
@@
 echo "[INFO] COMPOSE_FILE=$COMPOSE_FILE"
 echo "[INFO] HOLO_BOT_VERSION=$HOLO_BOT_VERSION"
 echo "[INFO] SHARED_GO_WORKSPACE_PATH=$SHARED_GO_WORKSPACE_PATH"
```

### 3-3. `docker-compose.prod.yml`

모든 service build `additional_contexts`에서 `iris_client_go`를 제거한다.

```diff
--- a/docker-compose.prod.yml
+++ b/docker-compose.prod.yml
@@
-      additional_contexts:
-        iris_client_go: ${IRIS_CLIENT_GO_PATH:-../iris-client-go}
-        shared_go_workspace: ${SHARED_GO_WORKSPACE_PATH:-./shared-go}
+      additional_contexts:
+        shared_go_workspace: ${SHARED_GO_WORKSPACE_PATH:-./shared-go}
```

위 hunk를 아래 service들에 모두 적용한다.

- `hololive-kakao-bot-go`
- `hololive-dispatcher-go`
- `hololive-stream-ingester`
- `youtube-scraper`
- `hololive-llm-sched`

### 3-4. Dockerfile 5개 공통 수정

공통 원칙은 두 가지다.

- `COPY --from=iris_client_go . ../iris-client-go` 제거
- `go mod edit -replace github.com/park285/iris-client-go=/workspace/iris-client-go` 제거

#### `hololive/hololive-kakao-bot-go/Dockerfile`

```diff
--- a/hololive/hololive-kakao-bot-go/Dockerfile
+++ b/hololive/hololive-kakao-bot-go/Dockerfile
@@
 COPY go.work go.work.sum ./
-COPY --from=iris_client_go . ../iris-client-go
 COPY --from=shared_go_workspace . ./shared-go
@@
 RUN \
     --mount=type=cache,target=/go/pkg/mod \
     --mount=type=cache,target=/root/.cache/go-build \
-    go mod edit -replace github.com/park285/iris-client-go=/workspace/iris-client-go && \
     CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOEXPERIMENT=greenteagc \
     go build -tags sonic -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.Version=${VERSION}" -o /dist/bin/bot ./cmd/bot && \
```

같은 패턴을 아래 파일에도 그대로 적용한다.

- `hololive/hololive-dispatcher-go/Dockerfile`
- `hololive/hololive-stream-ingester/Dockerfile`
- `hololive/hololive-stream-ingester/Dockerfile.youtube-scraper`
- `hololive/hololive-llm-sched/Dockerfile`

### 3-5. architecture scripts

외부 `../llm/shared-go`를 default source로 보는 로직을 제거한다.  
env override는 남겨도 되지만, 기본값은 항상 `./shared-go`여야 한다.

#### `scripts/architecture/export-go-workspace-import-graph.sh`

```diff
--- a/scripts/architecture/export-go-workspace-import-graph.sh
+++ b/scripts/architecture/export-go-workspace-import-graph.sh
@@
 resolve_shared_go_dir() {
-  local candidate="${SHARED_GO_WORKSPACE_PATH:-${REPO_CANONICAL_ROOT}/../llm/shared-go}"
-  if [[ ! -d "${candidate}" ]]; then
-    candidate="${ROOT_DIR}/shared-go"
-  fi
+  local candidate="${SHARED_GO_WORKSPACE_PATH:-${ROOT_DIR}/shared-go}"
   if [[ ! -d "${candidate}" ]]; then
     echo "error: active shared-go dir not found" >&2
     exit 1
   fi
   printf '%s\n' "${candidate}"
 }
```

같은 패턴을 아래 파일에도 적용한다.

- `scripts/architecture/check-shared-go-boundary.sh`
- `scripts/architecture/check-shared-go-packages.sh`

#### `docs/architecture/shared-go-package-allowlist.txt`

```diff
--- a/docs/architecture/shared-go-package-allowlist.txt
+++ b/docs/architecture/shared-go-package-allowlist.txt
@@
-# Default active source: ../llm/shared-go/pkg/*
+# Default active source: ./shared-go/pkg/*
```

---

## 4. shared runtime/server helper로 HTTP skeleton 중복 제거

현재 bot / stream-ingester / llm-sched에는 다음 중복이 있다.

- `ProvideAPIServer`
- `ProvideHealthOnlyRouter`
- `ProvideTriggerRouter`
- trusted proxy, recovery, base middleware, metrics route를 묶는 boilerplate

이 중복은 **의미가 아니라 문법이 반복되는** 전형적인 AI smell이다.  
해결은 shared helper를 추가하고 runtime별 파일에서는 진짜 runtime-specific route만 남기는 것이다.

### 4-1. 새 파일: `hololive/hololive-shared/pkg/server/runtime_helpers.go`

```diff
+++ b/hololive/hololive-shared/pkg/server/runtime_helpers.go
@@
+package server
+
+import (
+	"context"
+	"fmt"
+	"log/slog"
+	"net/http"
+	"strings"
+
+	"github.com/gin-contrib/gzip"
+	"github.com/gin-gonic/gin"
+	"github.com/prometheus/client_golang/prometheus/promhttp"
+	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
+
+	"github.com/kapu/hololive-shared/pkg/constants"
+	"github.com/kapu/hololive-shared/pkg/health"
+	"github.com/kapu/hololive-shared/pkg/server/middleware"
+)
+
+type RuntimeRouterOptions struct {
+	APIKey          string
+	EnableGzip      bool
+	Operation       string
+	SkipLogPaths    []string
+	RegisterRoutes  func(*gin.Engine) error
+	ReadyResponder  func(*gin.Context)
+}
+
+func NewH2CServer(addr string, handler http.Handler, operation string) *http.Server {
+	if handler == nil {
+		handler = http.NotFoundHandler()
+	}
+	if strings.TrimSpace(operation) != "" {
+		handler = otelhttp.NewHandler(handler, operation)
+	}
+
+	return &http.Server{
+		Addr:              addr,
+		Handler:           WrapH2C(handler),
+		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
+		ReadTimeout:       constants.ServerTimeout.Read,
+		WriteTimeout:      constants.ServerTimeout.Write,
+		IdleTimeout:       constants.ServerTimeout.Idle,
+		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
+	}
+}
+
+func NewRuntimeRouter(ctx context.Context, logger *slog.Logger, opts RuntimeRouterOptions) (*gin.Engine, error) {
+	gin.SetMode(gin.ReleaseMode)
+	router := gin.New()
+	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
+		return nil, fmt.Errorf("set trusted proxies: %w", err)
+	}
+	router.TrustedPlatform = gin.PlatformCloudflare
+
+	router.Use(gin.Recovery())
+	if opts.EnableGzip {
+		router.Use(gzip.Gzip(gzip.DefaultCompression))
+	}
+	ApplyBaseMiddleware(router, ctx, logger, BaseMiddlewareOptions{
+		SkipLogPaths: append([]string{"/health", "/ready", "/metrics"}, opts.SkipLogPaths...),
+	})
+
+	router.GET("/health", func(c *gin.Context) {
+		c.JSON(http.StatusOK, health.Get())
+	})
+	if opts.ReadyResponder != nil {
+		router.GET("/ready", opts.ReadyResponder)
+	} else {
+		router.GET("/ready", func(c *gin.Context) {
+			c.JSON(http.StatusOK, health.Get())
+		})
+	}
+
+	metrics := router.Group("")
+	metrics.Use(middleware.APIKeyAuthMiddleware(opts.APIKey))
+	metrics.GET("/metrics", gin.WrapH(promhttp.Handler()))
+
+	if opts.RegisterRoutes != nil {
+		if err := opts.RegisterRoutes(router); err != nil {
+			return nil, err
+		}
+	}
+
+	return router, nil
+}
```

### 4-2. `hololive-kakao-bot-go/internal/app/api_router.go`

`ProvideAPIServer` wrapper를 제거하고 callsite에서 shared helper를 직접 쓴다.

```diff
--- a/hololive/hololive-kakao-bot-go/internal/app/api_router.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/api_router.go
@@
-import (
+import (
 	"context"
 	"errors"
 	"fmt"
 	"log/slog"
-	"net/http"
-	"strings"
+	"strings"
@@
-// ProvideAPIServer: 관리자용 HTTP 서버 인스턴스를 생성합니다.
-// H2C(HTTP/2 Cleartext)를 기본으로 사용하여 멀티플렉싱과 헤더 압축 이점을 제공한다.
-func ProvideAPIServer(addr string, router *gin.Engine) *http.Server {
-	return &http.Server{
-		Addr:              addr,
-		Handler:           sharedserver.WrapH2C(router),
-		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
-		ReadTimeout:       constants.ServerTimeout.Read,
-		WriteTimeout:      constants.ServerTimeout.Write,
-		IdleTimeout:       constants.ServerTimeout.Idle,
-		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
-	}
-}
-
 // ProvideAPIRouter: hololive-bot 도메인 API를 서빙하는 Gin 라우터를 설정합니다.
```

그리고 `newAPIRouter(...)` 내부는 shared helper를 사용하도록 바꾼다.

```diff
@@
 func newAPIRouter(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*gin.Engine, error) {
-	gin.SetMode(gin.ReleaseMode)
-
-	router := gin.New()
-	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
-		return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
-	}
-
-	router.TrustedPlatform = gin.PlatformCloudflare
-
-	router.Use(gin.Recovery())
-	router.Use(gzip.Gzip(gzip.DefaultCompression)) // 응답 압축 (HTTP/2 호환)
-	sharedserver.ApplyBaseMiddleware(router, ctx, logger, sharedserver.BaseMiddlewareOptions{
-		SkipLogPaths: []string{
-			"/health",
-			"/ready",
-			"/metrics", // Prometheus 메트릭 폴링 (15초 간격)
-		},
-	})
+	router, err := sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
+		APIKey:       cfg.Server.APIKey,
+		EnableGzip:   true,
+		SkipLogPaths: []string{"/metrics"},
+	})
+	if err != nil {
+		return nil, err
+	}
@@
-	registerAPIHealthRoutes(router, cfg.Server.APIKey)
+	registerAPIHealthRoutes(router, cfg.Server.APIKey)
```

### 4-3. `hololive-kakao-bot-go/internal/app/bootstrap_bot_server.go`

```diff
--- a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_server.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_server.go
@@
 	addr := fmt.Sprintf(":%d", cfg.Server.Port)

-	return ProvideAPIServer(addr, botRouter), nil
+	return sharedserver.NewH2CServer(addr, botRouter, "hololive-bot.http"), nil
 }
```

### 4-4. `hololive-stream-ingester/internal/app/api_router.go`

generic helper 두 개를 지운다.

```diff
--- a/hololive/hololive-stream-ingester/internal/app/api_router.go
+++ b/hololive/hololive-stream-ingester/internal/app/api_router.go
@@
-import (
-	"context"
-	"fmt"
-	"log/slog"
-	"net/http"
-	"strings"
-
-	"github.com/gin-contrib/gzip"
-	"github.com/gin-gonic/gin"
-	"github.com/prometheus/client_golang/prometheus/promhttp"
-	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
-
-	"github.com/kapu/hololive-shared/pkg/constants"
-	"github.com/kapu/hololive-shared/pkg/health"
-	sharedserver "github.com/kapu/hololive-shared/pkg/server"
-	"github.com/kapu/hololive-shared/pkg/server/middleware"
-)
+import (
+	"context"
+	"log/slog"
+
+	"github.com/gin-gonic/gin"
+	sharedserver "github.com/kapu/hololive-shared/pkg/server"
+)
@@
-func ProvideAPIServer(addr string, handler http.Handler, operation string) *http.Server {
-...
-}
-
-func ProvideHealthOnlyRouter(ctx context.Context, logger *slog.Logger, readiness *ingestionReadinessState, apiKey string) (*gin.Engine, error) {
-...
-}
+func ProvideHealthOnlyRouter(ctx context.Context, logger *slog.Logger, readiness *ingestionReadinessState, apiKey string) (*gin.Engine, error) {
+	return sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
+		APIKey:     apiKey,
+		EnableGzip: true,
+		Operation:  "stream-ingester.http",
+		ReadyResponder: func(c *gin.Context) {
+			statusCode, payload := readiness.response()
+			c.JSON(statusCode, payload)
+		},
+	})
+}
```

그리고 `bootstrap_stream_ingester.go`는 shared helper를 직접 쓴다.

```diff
--- a/hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go
+++ b/hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go
@@
-	return ProvideAPIServer(fmt.Sprintf(":%d", cfg.Server.Port), router, runtimeHTTPServerOperationName(runtimeName)), nil
+	return sharedserver.NewH2CServer(
+		fmt.Sprintf(":%d", cfg.Server.Port),
+		router,
+		runtimeHTTPServerOperationName(runtimeName),
+	), nil
```

### 4-5. `hololive-llm-sched/internal/app/api_router.go`

```diff
--- a/hololive/hololive-llm-sched/internal/app/api_router.go
+++ b/hololive/hololive-llm-sched/internal/app/api_router.go
@@
-import (
-	"context"
-	"fmt"
-	"log/slog"
-	"net/http"
-	"strings"
-
-	"github.com/gin-gonic/gin"
-	"github.com/prometheus/client_golang/prometheus/promhttp"
-
-	"github.com/kapu/hololive-shared/pkg/constants"
-	sharedserver "github.com/kapu/hololive-shared/pkg/server"
-	"github.com/kapu/hololive-shared/pkg/server/middleware"
-)
+import (
+	"context"
+	"fmt"
+	"log/slog"
+	"strings"
+
+	"github.com/gin-gonic/gin"
+	sharedserver "github.com/kapu/hololive-shared/pkg/server"
+)
@@
-func ProvideAPIServer(addr string, router *gin.Engine) *http.Server {
-...
-}
-
 func ProvideHealthOnlyRouter(ctx context.Context, logger *slog.Logger, apiKey string) (*gin.Engine, error) {
-	gin.SetMode(gin.ReleaseMode)
-	router := gin.New()
-	...
-	return router, nil
+	return sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
+		APIKey: apiKey,
+	})
 }
@@
 func ProvideTriggerRouter(
 	ctx context.Context,
 	logger *slog.Logger,
 	triggerHandler *sharedserver.TriggerHandler,
 	apiKey string,
 ) (*gin.Engine, error) {
-	router, err := ProvideHealthOnlyRouter(ctx, logger, apiKey)
-	if err != nil {
-		return nil, err
-	}
-
-	if triggerHandler != nil {
-		if strings.TrimSpace(apiKey) == "" {
-			return nil, fmt.Errorf("API_SECRET_KEY required")
-		}
-		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)
-	}
-
-	return router, nil
+	return sharedserver.NewRuntimeRouter(ctx, logger, sharedserver.RuntimeRouterOptions{
+		APIKey: apiKey,
+		RegisterRoutes: func(router *gin.Engine) error {
+			if triggerHandler == nil {
+				return nil
+			}
+			if strings.TrimSpace(apiKey) == "" {
+				return fmt.Errorf("API_SECRET_KEY required")
+			}
+			triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)
+			return nil
+		},
+	})
 }
```

그리고 `bootstrap_llm_scheduler.go`는 shared helper를 직접 쓴다.

```diff
--- a/hololive/hololive-llm-sched/internal/app/bootstrap_llm_scheduler.go
+++ b/hololive/hololive-llm-sched/internal/app/bootstrap_llm_scheduler.go
@@
-	return ProvideAPIServer(addr, router), nil
+	return sharedserver.NewH2CServer(addr, router, "hololive-llm-sched.http"), nil
```

---

## 5. lifecycle `Close()` wrapper 제거

현재 `Container`, `BotRuntime`, `DBIntegrationRuntime`, `FetchProfilesRuntime`, `StreamIngesterRuntime`, `LLMSchedulerRuntime`가 모두 거의 같은 `Close()` wrapper를 가진다.  
이것도 AI smell이다. 공통 abstraction으로 빼면 된다.

### 5-1. 새 파일: `shared-go/pkg/runtime/lifecycle/managed.go`

```diff
+++ b/shared-go/pkg/runtime/lifecycle/managed.go
@@
+package lifecycle
+
+type Managed struct {
+	CleanupCloser
+}
+
+func NewManaged(cleanup func()) Managed {
+	return Managed{
+		CleanupCloser: NewCleanupCloser(cleanup),
+	}
+}
+
+func (m *Managed) Close() {
+	if m == nil {
+		return
+	}
+	m.CleanupCloser.Close()
+}
```

### 5-2. `hololive-kakao-bot-go/internal/app/container.go`

```diff
--- a/hololive/hololive-kakao-bot-go/internal/app/container.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/container.go
@@
 type Container struct {
 	Config *config.Config
 	Logger *slog.Logger

 	botDeps *bot.Dependencies
-	lifecycle.CleanupCloser
-}
-
-func (c *Container) Close() {
-	if c == nil {
-		return
-	}
-
-	c.CleanupCloser.Close()
+	lifecycle.Managed
 }
@@
 	return &Container{
-		Config:        cfg,
-		Logger:        logger,
-		botDeps:       deps,
-		CleanupCloser: lifecycle.NewCleanupCloser(cleanup),
+		Config:  cfg,
+		Logger:  logger,
+		botDeps: deps,
+		Managed: lifecycle.NewManaged(cleanup),
 	}, nil
 }
```

같은 패턴을 아래 파일에도 적용한다.

- `hololive-kakao-bot-go/internal/app/runtime.go`
- `hololive-kakao-bot-go/internal/app/db_integration_runtime.go`
- `hololive-kakao-bot-go/internal/app/fetch_profiles_runtime.go`
- `hololive-stream-ingester/internal/app/stream_ingester_runtime_runner.go`
- `hololive-llm-sched/internal/app/bootstrap_llm_scheduler.go`

### 5-3. `hololive-dispatcher-go/internal/app/runtime.go`

dispatcher는 custom cache close logging이 있으므로 cleanup func로 감싼다.

```diff
--- a/hololive/hololive-dispatcher-go/internal/app/runtime.go
+++ b/hololive/hololive-dispatcher-go/internal/app/runtime.go
@@
 type Runtime struct {
 	cfg        *Config
 	logger     *slog.Logger
 	cacheSvc   cache.Client
 	dispatcher *dispatch.Dispatcher
 	httpServer *http.Server
 	readyState *readinessState
+	lifecycle.Managed
 }
@@
-// Close: 런타임 리소스를 정리한다.
-func (r *Runtime) Close() {
-	if r == nil || r.cacheSvc == nil {
-		return
-	}
-	if err := r.cacheSvc.Close(); err != nil {
-		r.logger.Warn("Close cache service failed", slog.Any("error", err))
-	}
-}
-
 // Run: dispatcher-go 메인 실행 루프.
@@
 	runtime := &Runtime{
 		cfg:        cfg,
 		logger:     logger,
 		cacheSvc:   cacheSvc,
 		dispatcher: dispatcher,
 		readyState: newReadinessState(),
 	}
+	runtime.Managed = lifecycle.NewManaged(func() {
+		if runtime.cacheSvc == nil {
+			return
+		}
+		if err := runtime.cacheSvc.Close(); err != nil {
+			runtime.logger.Warn("Close cache service failed", slog.Any("error", err))
+		}
+	})
```

### 5-4. 테스트 literal 업데이트

`CleanupCloser:`로 직접 runtime struct를 만들던 테스트는 `Managed:`로 바꾼다.

예:

```diff
-CleanupCloser: lifecycle.NewCleanupCloser(func() { calls++ }),
+Managed: lifecycle.NewManaged(func() { calls++ }),
```

적용 대상:

- `hololive-kakao-bot-go/internal/app/runtime_additional_test.go`
- `hololive-kakao-bot-go/internal/app/runtime_wrappers_additional_test.go`
- `hololive-kakao-bot-go/internal/app/container_additional_test.go`
- `hololive-stream-ingester/internal/app/runtime_helpers_test.go`
- `hololive-stream-ingester/internal/app/stream_ingester_runtime_builder_test.go`
- `hololive-llm-sched/internal/app/bootstrap_llm_scheduler_lifecycle_test.go`

---

## 6. provider/module 정리: runtime-owned duplication 제거

DI 라이브러리를 넣지 않는다.  
대신 runtime-owned wrapper를 줄이고, shared module builder로 옮긴다.

핵심 원칙은 이렇다.

- `hololive-shared/pkg/providers`는 "constructor / module builder"만 가진다.
- runtime 앱은 module을 조립하지, `Provide*` 얇은 wrapper를 줄줄이 호출하지 않는다.
- 현재 bot / stream-ingester에 거의 동일하게 있는 `providers/infra_resources.go`, `providers/youtube.go`, `providers/settings.go`는 shared로 이동시킨다.

### 6-1. 새 파일: `hololive/hololive-shared/pkg/providers/modules/infra.go`

```diff
+++ b/hololive/hololive-shared/pkg/providers/modules/infra.go
@@
+package modules
+
+import (
+	"context"
+	"fmt"
+	"log/slog"
+
+	"github.com/kapu/hololive-shared/pkg/config"
+	"github.com/kapu/hololive-shared/pkg/providers"
+	"github.com/kapu/hololive-shared/pkg/service/cache"
+	"github.com/kapu/hololive-shared/pkg/service/database"
+	"github.com/kapu/hololive-shared/pkg/service/member"
+)
+
+type InfraModule struct {
+	Cache       cache.Client
+	Postgres    database.Client
+	MemberRepo  *member.Repository
+	MemberCache *member.Cache
+	Cleanup     func()
+}
+
+func BuildInfraModule(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *InfraModule, retErr error) {
+	if cfg == nil {
+		return nil, fmt.Errorf("build infra module: config is nil")
+	}
+
+	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, cfg.Valkey, logger)
+	if err != nil {
+		return nil, fmt.Errorf("build infra module: cache resources: %w", err)
+	}
+	defer func() {
+		if retErr != nil && cleanupCache != nil {
+			cleanupCache()
+		}
+	}()
+
+	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
+	if err != nil {
+		return nil, fmt.Errorf("build infra module: database resources: %w", err)
+	}
+	defer func() {
+		if retErr != nil && cleanupDB != nil {
+			cleanupDB()
+		}
+	}()
+
+	cacheService := cacheResources.Service
+	postgresService := databaseResources.Service
+	memberRepository := providers.ProvideMemberRepository(postgresService, logger)
+	memberCache, err := providers.ProvideMemberCache(ctx, memberRepository, cacheService, logger)
+	if err != nil {
+		return nil, fmt.Errorf("build infra module: member cache: %w", err)
+	}
+
+	return &InfraModule{
+		Cache:       cacheService,
+		Postgres:    postgresService,
+		MemberRepo:  memberRepository,
+		MemberCache: memberCache,
+		Cleanup: func() {
+			if cleanupDB != nil {
+				cleanupDB()
+			}
+			if cleanupCache != nil {
+				cleanupCache()
+			}
+		},
+	}, nil
+}
```

### 6-2. 새 파일: `hololive/hololive-shared/pkg/providers/modules/settings.go`

bot / stream-ingester duplicated `ProvideSettingsService()`를 shared로 옮긴다.

```diff
+++ b/hololive/hololive-shared/pkg/providers/modules/settings.go
@@
+package modules
+
+import (
+	"log/slog"
+	"os"
+	"path/filepath"
+	"strings"
+
+	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
+	"github.com/kapu/hololive-shared/pkg/service/settings"
+)
+
+func BuildSettingsService(targetMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) settings.ReadWriter {
+	settingsPath := resolveSettingsFilePath()
+	if logger != nil {
+		logger.Info("Using settings file path", slog.String("path", settingsPath))
+	}
+
+	normalized := sharedchecker.NormalizeTargetMinutes(targetMinutes)
+	defaultMinute := 5
+	if len(normalized) > 0 && normalized[0] > 0 {
+		defaultMinute = normalized[0]
+	}
+
+	return settings.NewSettingsService(settingsPath, settings.Settings{
+		AlarmAdvanceMinutes: defaultMinute,
+		ScraperProxyEnabled: scraperProxyEnabled,
+	}, logger)
+}
+
+func ResolvePersistedTargetMinutes(targetMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) []int {
+	settingsPath := resolveSettingsFilePath()
+	if _, err := os.Stat(settingsPath); err != nil {
+		return sharedchecker.NormalizeTargetMinutes(targetMinutes)
+	}
+
+	svc := BuildSettingsService(targetMinutes, scraperProxyEnabled, logger)
+	current := svc.Get().AlarmAdvanceMinutes
+	if current <= 0 {
+		return sharedchecker.NormalizeTargetMinutes(targetMinutes)
+	}
+	return sharedchecker.NormalizeTargetMinutes([]int{current, 3, 1})
+}
+
+func resolveSettingsFilePath() string {
+	dir := strings.TrimSpace(os.Getenv("SETTINGS_DIR"))
+	if dir == "" {
+		dir = "data"
+	}
+	return filepath.Join(dir, "settings.json")
+}
```

### 6-3. 새 파일: `hololive/hololive-shared/pkg/providers/modules/youtube_stack.go`

bot / stream-ingester duplicated `ProvideYouTubeStack()`를 shared로 올린다.

```diff
+++ b/hololive/hololive-shared/pkg/providers/modules/youtube_stack.go
@@
+package modules
+
+import (
+	"context"
+	"log/slog"
+
+	"github.com/kapu/hololive-shared/pkg/config"
+	"github.com/kapu/hololive-shared/pkg/domain"
+	"github.com/kapu/hololive-shared/pkg/providers"
+	"github.com/kapu/hololive-shared/pkg/service/cache"
+	"github.com/kapu/hololive-shared/pkg/service/holodex"
+	"github.com/kapu/hololive-shared/pkg/service/member"
+	"github.com/kapu/hololive-shared/pkg/service/youtube"
+	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
+	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
+	"github.com/park285/iris-client-go/iris"
+)
+
+type YouTubeStackParams struct {
+	YouTubeConfig   config.YouTubeConfig
+	ScraperConfig   config.ScraperConfig
+	CacheService    cache.Client
+	HolodexService  *holodex.Service
+	Members         member.DataProvider
+	StatsRepo       *ytstats.StatsRepository
+	AlarmState      domain.AlarmDispatchState
+	IrisClient      iris.Sender
+	Formatter       youtube.MilestoneMessageFormatter
+	SharedRateLimit *scraper.RateLimiter
+	Logger          *slog.Logger
+}
+
+func BuildYouTubeStack(ctx context.Context, p YouTubeStackParams) *providers.YouTubeStack {
+	if !p.YouTubeConfig.EnableQuotaBuilding || p.YouTubeConfig.APIKey == "" {
+		if p.Logger != nil {
+			p.Logger.Info("YouTube quota building disabled; stats repository only")
+		}
+		return &providers.YouTubeStack{StatsRepo: p.StatsRepo}
+	}
+
+	svc, err := youtube.NewYouTubeService(ctx, p.YouTubeConfig.APIKey, p.CacheService, scraper.ProxyConfig{
+		Enabled: p.ScraperConfig.ProxyEnabled,
+		URL:     p.ScraperConfig.ProxyURL,
+	}, p.SharedRateLimit, p.Logger)
+	if err != nil {
+		if p.Logger != nil {
+			p.Logger.Warn("YouTube service init failed (optional feature)", slog.Any("error", err))
+		}
+		return &providers.YouTubeStack{StatsRepo: p.StatsRepo}
+	}
+
+	scheduler := youtube.NewScheduler(
+		svc,
+		p.HolodexService,
+		p.CacheService,
+		p.StatsRepo,
+		p.Members,
+		p.AlarmState,
+		p.IrisClient,
+		p.Formatter,
+		p.Logger,
+	)
+
+	if p.Logger != nil {
+		p.Logger.Info("YouTube quota building enabled", slog.String("mode", "API Key"), slog.Int("daily_target", 9192))
+	}
+
+	return &providers.YouTubeStack{
+		Service:   svc,
+		Scheduler: scheduler,
+		StatsRepo: p.StatsRepo,
+	}
+}
```

### 6-4. runtime callsite 치환

#### `hololive-kakao-bot-go/internal/app/bootstrap_services_alarm.go`

```diff
--- a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_alarm.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_alarm.go
@@
-	resolved := appproviders.ResolveAlarmAdvanceMinutes(advanceMinutes, scraperProxyEnabled, logger)
+	resolved := modules.ResolvePersistedTargetMinutes(advanceMinutes, scraperProxyEnabled, logger)
```

#### `hololive-kakao-bot-go/internal/app/bootstrap_services_alarm_stack.go`

```diff
--- a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_alarm_stack.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_alarm_stack.go
@@
-	youTubeStatsRepository := sharedproviders.ProvideYouTubeStatsRepository(infra.postgresService, logger)
-	youTubeStack := appproviders.ProvideYouTubeStack(
-		ctx, cfg.YouTube, cfg.Scraper, infra.cacheService, holodexService, memberServiceAdapter,
-		youTubeStatsRepository, alarmSvc, irisClient, formatter, sharedRL, logger,
-	)
+	youTubeStatsRepository := ytstats.NewYouTubeStatsRepository(infra.postgresService, logger)
+	youTubeStack := modules.BuildYouTubeStack(ctx, modules.YouTubeStackParams{
+		YouTubeConfig:   cfg.YouTube,
+		ScraperConfig:   cfg.Scraper,
+		CacheService:    infra.cacheService,
+		HolodexService:  holodexService,
+		Members:         memberServiceAdapter,
+		StatsRepo:       youTubeStatsRepository,
+		AlarmState:      alarmSvc,
+		IrisClient:      irisClient,
+		Formatter:       formatter,
+		SharedRateLimit: sharedRL,
+		Logger:          logger,
+	})
@@
-		settingsService: appproviders.ProvideSettingsService(
+		settingsService: modules.BuildSettingsService(
 			cfg.Notification.AdvanceMinutes,
 			cfg.Scraper.ProxyEnabled,
 			logger,
 		),
```

#### `hololive-stream-ingester/internal/app/stream_ingester_runtime_builder.go`

```diff
--- a/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder.go
+++ b/hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder.go
@@
-	youTubeStatsRepository := sharedproviders.ProvideYouTubeStatsRepository(infra.postgresService, logger)
-	// stream-ingester는 alarm dispatch가 없으므로 alarmSvc=nil로 전달
-	youTubeStack := appproviders.ProvideYouTubeStack(ctx, cfg.YouTube, cfg.Scraper, infra.cacheService, holodexService, memberServiceAdapter, youTubeStatsRepository, nil, irisClient, nil, sharedRL, logger)
-
-	settingsService := appproviders.ProvideSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)
+	youTubeStatsRepository := ytstats.NewYouTubeStatsRepository(infra.postgresService, logger)
+	youTubeStack := modules.BuildYouTubeStack(ctx, modules.YouTubeStackParams{
+		YouTubeConfig:   cfg.YouTube,
+		ScraperConfig:   cfg.Scraper,
+		CacheService:    infra.cacheService,
+		HolodexService:  holodexService,
+		Members:         memberServiceAdapter,
+		StatsRepo:       youTubeStatsRepository,
+		AlarmState:      nil,
+		IrisClient:      irisClient,
+		Formatter:       nil,
+		SharedRateLimit: sharedRL,
+		Logger:          logger,
+	})
+
+	settingsService := modules.BuildSettingsService(cfg.Notification.AdvanceMinutes, cfg.Scraper.ProxyEnabled, logger)
```

#### `hololive-kakao-bot-go/internal/app/bootstrap_core.go` / `hololive-stream-ingester/internal/app/bootstrap.go`

둘 다 runtime-owned `ProvideInfraResources()` 대신 shared `BuildInfraModule()`로 바꾼다.

```diff
--- a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core.go
@@
-	resources, err := appproviders.ProvideInfraResources(ctx, cfg, logger)
+	resources, err := modules.BuildInfraModule(ctx, cfg, logger)
```

`hololive-stream-ingester/internal/app/bootstrap.go`에도 동일 적용.

### 6-5. 삭제 목록

아래 파일은 module migration 후 삭제한다.

- `hololive/hololive-kakao-bot-go/internal/app/providers/infra_resources.go`
- `hololive/hololive-kakao-bot-go/internal/app/providers/youtube.go`
- `hololive/hololive-kakao-bot-go/internal/app/providers/settings.go`
- `hololive/hololive-stream-ingester/internal/app/providers/infra_resources.go`
- `hololive/hololive-stream-ingester/internal/app/providers/youtube.go`
- `hololive/hololive-stream-ingester/internal/app/providers/settings.go`

그리고 아래 shared wrapper는 callsite migration 후 삭제 후보로 둔다.

- `ProvideValkeyConfig`
- `ProvidePostgresConfig`
- `ProvideCacheService`
- `ProvidePostgresService`
- `ProvideHolodexAPIKey`
- `ProvideScraperService`
- `ProvideHolodexService`
- `ProvideYouTubeStatsRepository`

---

## 7. admin-dashboard backend: blind proxy에서 typed admin contract로 전환

현재 backend는 `/admin/api/holo/{*path}`를 blind proxy로 넘긴다.  
이 구조는 프런트가 upstream payload에 직접 종속되기 때문에, admin-dashboard가 자기 contract를 소유하지 못한다.

최종 목표는 다음이다.

- dashboard가 실제로 사용하는 holo endpoint는 backend가 **typed handler + OpenAPI**로 소유
- blind proxy는 websocket / 미이관 endpoint의 compatibility fallback으로만 유지
- OpenAPI export binary를 실제로 추가
- frontend는 generated client만 transport contract로 사용

### 7-1. 새 모듈 추가

#### `admin-dashboard/backend/src/holo/mod.rs`

```diff
+++ b/admin-dashboard/backend/src/holo/mod.rs
@@
+pub mod client;
+pub mod handlers;
+pub mod types;
```

#### `admin-dashboard/backend/src/holo/types.rs`

아래는 핵심 구조체만 실었다. 실제 구현 시 이 파일에 dashboard가 현재 쓰는 모든 DTO를 모은다.

```diff
+++ b/admin-dashboard/backend/src/holo/types.rs
@@
+use serde::{Deserialize, Serialize};
+use std::collections::HashMap;
+use utoipa::ToSchema;
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct StatusOnlyResponse {
+    pub status: String,
+    pub message: Option<String>,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct Alarm {
+    pub room_id: String,
+    pub room_name: String,
+    pub user_id: String,
+    pub user_name: String,
+    pub channel_id: String,
+    pub member_name: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct AlarmsResponse {
+    pub status: String,
+    pub alarms: Vec<Alarm>,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct DeleteAlarmRequest {
+    pub room_id: String,
+    pub user_id: String,
+    pub channel_id: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct RoomNameUpdateRequest {
+    pub room_id: String,
+    pub room_name: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct UserNameUpdateRequest {
+    pub user_id: String,
+    pub user_name: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct Aliases {
+    pub ko: Vec<String>,
+    pub ja: Vec<String>,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct Member {
+    pub id: i64,
+    pub channel_id: String,
+    pub name: String,
+    pub aliases: Aliases,
+    pub name_ja: Option<String>,
+    pub name_ko: Option<String>,
+    pub is_graduated: bool,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct MembersResponse {
+    pub status: String,
+    pub members: Vec<Member>,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct AddAliasRequest {
+    #[schema(value_type = String, example = "ko")]
+    pub r#type: String,
+    pub alias: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct RemoveAliasRequest {
+    #[schema(value_type = String, example = "ja")]
+    pub r#type: String,
+    pub alias: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct SetGraduationRequest {
+    pub is_graduated: bool,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct UpdateChannelRequest {
+    pub channel_id: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct UpdateMemberNameRequest {
+    pub name: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct RoomsResponse {
+    pub status: String,
+    pub rooms: Vec<String>,
+    pub acl_enabled: bool,
+    pub acl_mode: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct AddRoomRequest {
+    pub room: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct RemoveRoomRequest {
+    pub room: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct SetAclRequest {
+    pub enabled: Option<bool>,
+    pub mode: Option<String>,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct SetAclResponse {
+    pub status: String,
+    pub enabled: bool,
+    pub mode: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct Settings {
+    pub alarm_advance_minutes: i32,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct SettingsResponse {
+    pub status: String,
+    pub settings: Settings,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct StatsResponse {
+    pub status: String,
+    pub members: i32,
+    pub alarms: i32,
+    pub rooms: i32,
+    pub version: String,
+    pub uptime: String,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct ChannelStat {
+    pub channel_id: String,
+    pub channel_title: String,
+    pub subscriber_count: i64,
+    pub video_count: i64,
+    pub view_count: i64,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct ChannelStatsResponse {
+    pub status: String,
+    pub stats: HashMap<String, ChannelStat>,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct Stream {
+    pub id: String,
+    pub title: String,
+    pub status: String,
+    pub channel_name: Option<String>,
+    pub channel_id: String,
+    pub link: Option<String>,
+    pub thumbnail: Option<String>,
+    pub start_scheduled: Option<String>,
+    pub start_actual: Option<String>,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct StreamsResponse {
+    pub status: String,
+    pub org: Option<String>,
+    pub streams: Vec<Stream>,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct Milestone {
+    pub channel_id: String,
+    pub member_name: String,
+    pub r#type: String,
+    pub value: i64,
+    pub achieved_at: String,
+    pub notified: bool,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct MilestonesResponse {
+    pub status: String,
+    pub milestones: Vec<Milestone>,
+    pub total: i64,
+    pub limit: i64,
+    pub offset: i64,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct NearMilestone {
+    pub channel_id: String,
+    pub member_name: String,
+    pub current_subs: i64,
+    pub next_milestone: i64,
+    pub remaining: i64,
+    pub progress_pct: f64,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct NearMilestonesResponse {
+    pub status: String,
+    pub members: Vec<NearMilestone>,
+    pub count: i64,
+    pub threshold: f64,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct MilestoneStats {
+    pub total_achieved: i64,
+    pub total_near_milestone: i64,
+    pub recent_achievements: i64,
+    pub not_notified_count: i64,
+}
+
+#[derive(Debug, Serialize, Deserialize, ToSchema)]
+pub struct MilestoneStatsResponse {
+    pub status: String,
+    pub stats: MilestoneStats,
+}
```

> 구현 시 `serde(rename_all = "camelCase")`를 구조체별로 넣어서 현재 프런트 payload와 정확히 맞추는 쪽이 더 깔끔하다.  
> 위 예시는 payload shape를 한 번에 보여주기 위한 골격이다.

#### `admin-dashboard/backend/src/holo/client.rs`

```diff
+++ b/admin-dashboard/backend/src/holo/client.rs
@@
+use reqwest::{Method, StatusCode};
+use serde::Serialize;
+use serde::de::DeserializeOwned;
+
+use crate::error::AppError;
+
+#[derive(Debug, Clone)]
+pub struct HoloApiClient {
+    base_url: String,
+    api_key: Option<String>,
+    client: reqwest::Client,
+}
+
+impl HoloApiClient {
+    pub fn new(base_url: String, api_key: Option<String>) -> anyhow::Result<Self> {
+        let client = reqwest::Client::builder()
+            .timeout(std::time::Duration::from_secs(10))
+            .build()?;
+
+        Ok(Self {
+            base_url: base_url.trim_end_matches('/').to_string(),
+            api_key: api_key.filter(|v| !v.trim().is_empty()),
+            client,
+        })
+    }
+
+    pub async fn get<T: DeserializeOwned>(&self, path: &str, query: Option<&[(&str, String)]>) -> Result<(StatusCode, T), AppError> {
+        self.request(Method::GET, path, query, Option::<&()>::None).await
+    }
+
+    pub async fn send<B: Serialize + ?Sized, T: DeserializeOwned>(
+        &self,
+        method: Method,
+        path: &str,
+        body: Option<&B>,
+    ) -> Result<(StatusCode, T), AppError> {
+        self.request(method, path, None, body).await
+    }
+
+    async fn request<B: Serialize + ?Sized, T: DeserializeOwned>(
+        &self,
+        method: Method,
+        path: &str,
+        query: Option<&[(&str, String)]>,
+        body: Option<&B>,
+    ) -> Result<(StatusCode, T), AppError> {
+        let mut req = self.client.request(method, format!("{}{}", self.base_url, path));
+        if let Some(key) = &self.api_key {
+            req = req.header("X-API-Key", key);
+        }
+        if let Some(query) = query {
+            req = req.query(query);
+        }
+        if let Some(body) = body {
+            req = req.json(body);
+        }
+
+        let resp = req.send().await?;
+        let status = resp.status();
+        let bytes = resp.bytes().await?;
+        let parsed = serde_json::from_slice::<T>(&bytes)?;
+        Ok((status, parsed))
+    }
+}
```

### 7-2. `admin-dashboard/backend/src/state.rs`

```diff
--- a/admin-dashboard/backend/src/state.rs
+++ b/admin-dashboard/backend/src/state.rs
@@
 use crate::auth::rate_limiter::LoginRateLimiter;
 use crate::auth::session::ValkeySessionStore;
 use crate::config::Config;
 use crate::docker::DockerService;
+use crate::holo::client::HoloApiClient;
 use crate::proxy::BotProxy;
 use crate::status::{StatusCollector, SystemStats};
@@
     pub rate_limiter: Arc<LoginRateLimiter>,
     pub bot_proxy: Option<BotProxy>,
+    pub holo_api: Arc<HoloApiClient>,
     pub docker_svc: Option<Arc<DockerService>>,
```

### 7-3. `admin-dashboard/backend/src/main.rs`

```diff
--- a/admin-dashboard/backend/src/main.rs
+++ b/admin-dashboard/backend/src/main.rs
@@
 mod handlers;
+mod holo;
 mod logging;
@@
     let bot_proxy = Some(proxy::BotProxy::new(&cfg.holo_bot_url, {
         let key = cfg.holo_bot_api_key.clone();
         if key.is_empty() { None } else { Some(key) }
     }));
+    let holo_api = Arc::new(
+        holo::client::HoloApiClient::new(
+            cfg.holo_bot_url.clone(),
+            if cfg.holo_bot_api_key.is_empty() {
+                None
+            } else {
+                Some(cfg.holo_bot_api_key.clone())
+            },
+        )
+        .expect("holo api client init failed"),
+    );
@@
         rate_limiter: rate_limiter.clone(),
         bot_proxy,
+        holo_api,
         docker_svc,
```

### 7-4. `admin-dashboard/backend/src/lib.rs`

```diff
--- a/admin-dashboard/backend/src/lib.rs
+++ b/admin-dashboard/backend/src/lib.rs
@@
 pub mod handlers;
+pub mod holo;
 pub mod middleware;
```

### 7-5. `admin-dashboard/backend/src/routes.rs`

typed route를 먼저 등록하고, wildcard proxy는 compatibility fallback으로만 남긴다.

```diff
--- a/admin-dashboard/backend/src/routes.rs
+++ b/admin-dashboard/backend/src/routes.rs
@@
 use axum::{
     Json, Router, middleware,
-    routing::{any, get, post},
+    routing::{any, delete, get, patch, post},
 };
@@
-    let proxy_routes = Router::new().route(
-        crate::proxy::HOLO_PROXY_ROUTE,
-        any(crate::proxy::bot_proxy::proxy_holo),
-    );
+    let holo_routes = Router::new()
+        .route("/admin/api/holo/alarms", get(crate::holo::handlers::get_alarms).delete(crate::holo::handlers::delete_alarm))
+        .route("/admin/api/holo/names/room", post(crate::holo::handlers::set_room_name))
+        .route("/admin/api/holo/names/user", post(crate::holo::handlers::set_user_name))
+        .route("/admin/api/holo/members", get(crate::holo::handlers::get_members).post(crate::holo::handlers::add_member))
+        .route("/admin/api/holo/members/{id}/aliases", post(crate::holo::handlers::add_alias).delete(crate::holo::handlers::remove_alias))
+        .route("/admin/api/holo/members/{id}/graduation", patch(crate::holo::handlers::set_graduation))
+        .route("/admin/api/holo/members/{id}/channel", patch(crate::holo::handlers::update_channel))
+        .route("/admin/api/holo/members/{id}/name", patch(crate::holo::handlers::update_member_name))
+        .route("/admin/api/holo/rooms", get(crate::holo::handlers::get_rooms).post(crate::holo::handlers::add_room).delete(crate::holo::handlers::remove_room))
+        .route("/admin/api/holo/rooms/acl", post(crate::holo::handlers::set_acl))
+        .route("/admin/api/holo/settings", get(crate::holo::handlers::get_settings).post(crate::holo::handlers::update_settings))
+        .route("/admin/api/holo/stats", get(crate::holo::handlers::get_stats))
+        .route("/admin/api/holo/stats/channels", get(crate::holo::handlers::get_channel_stats))
+        .route("/admin/api/holo/streams/live", get(crate::holo::handlers::get_live_streams))
+        .route("/admin/api/holo/streams/upcoming", get(crate::holo::handlers::get_upcoming_streams))
+        .route("/admin/api/holo/milestones", get(crate::holo::handlers::get_milestones))
+        .route("/admin/api/holo/milestones/near", get(crate::holo::handlers::get_near_milestones))
+        .route("/admin/api/holo/milestones/stats", get(crate::holo::handlers::get_milestone_stats));
+
+    let proxy_routes = Router::new().route(
+        crate::proxy::HOLO_PROXY_ROUTE,
+        any(crate::proxy::bot_proxy::proxy_holo), // compatibility fallback only
+    );
@@
     let authenticated = Router::new()
         .merge(auth_csrf)
         .merge(auth_get)
+        .merge(holo_routes)
         .merge(proxy_routes)
         .layer(auth_layer);
```

### 7-6. `admin-dashboard/backend/src/openapi.rs`

OpenAPI에 holo endpoint를 넣고, generated client 메서드명을 안정화하기 위해 `operation_id`를 고정한다.

```diff
--- a/admin-dashboard/backend/src/openapi.rs
+++ b/admin-dashboard/backend/src/openapi.rs
@@
     paths(
         crate::handlers::auth::handle_login,
         crate::handlers::auth::handle_logout,
         crate::handlers::auth::handle_session_status,
         crate::handlers::auth::handle_heartbeat,
         crate::handlers::docker::handle_docker_health,
         crate::handlers::docker::handle_docker_containers,
         crate::handlers::docker::handle_docker_restart,
         crate::handlers::docker::handle_docker_stop,
         crate::handlers::docker::handle_docker_start,
         crate::handlers::status::handle_aggregated_status,
+        crate::holo::handlers::get_alarms,
+        crate::holo::handlers::delete_alarm,
+        crate::holo::handlers::set_room_name,
+        crate::holo::handlers::set_user_name,
+        crate::holo::handlers::get_members,
+        crate::holo::handlers::add_member,
+        crate::holo::handlers::add_alias,
+        crate::holo::handlers::remove_alias,
+        crate::holo::handlers::set_graduation,
+        crate::holo::handlers::update_channel,
+        crate::holo::handlers::update_member_name,
+        crate::holo::handlers::get_rooms,
+        crate::holo::handlers::add_room,
+        crate::holo::handlers::remove_room,
+        crate::holo::handlers::set_acl,
+        crate::holo::handlers::get_settings,
+        crate::holo::handlers::update_settings,
+        crate::holo::handlers::get_stats,
+        crate::holo::handlers::get_channel_stats,
+        crate::holo::handlers::get_live_streams,
+        crate::holo::handlers::get_upcoming_streams,
+        crate::holo::handlers::get_milestones,
+        crate::holo::handlers::get_near_milestones,
+        crate::holo::handlers::get_milestone_stats,
     ),
     components(schemas(
@@
         crate::status::AggregatedStatus,
         crate::status::ServiceStatus,
+        crate::holo::types::StatusOnlyResponse,
+        crate::holo::types::Alarm,
+        crate::holo::types::AlarmsResponse,
+        crate::holo::types::DeleteAlarmRequest,
+        crate::holo::types::RoomNameUpdateRequest,
+        crate::holo::types::UserNameUpdateRequest,
+        crate::holo::types::Aliases,
+        crate::holo::types::Member,
+        crate::holo::types::MembersResponse,
+        crate::holo::types::AddAliasRequest,
+        crate::holo::types::RemoveAliasRequest,
+        crate::holo::types::SetGraduationRequest,
+        crate::holo::types::UpdateChannelRequest,
+        crate::holo::types::UpdateMemberNameRequest,
+        crate::holo::types::RoomsResponse,
+        crate::holo::types::AddRoomRequest,
+        crate::holo::types::RemoveRoomRequest,
+        crate::holo::types::SetAclRequest,
+        crate::holo::types::SetAclResponse,
+        crate::holo::types::Settings,
+        crate::holo::types::SettingsResponse,
+        crate::holo::types::StatsResponse,
+        crate::holo::types::ChannelStat,
+        crate::holo::types::ChannelStatsResponse,
+        crate::holo::types::Stream,
+        crate::holo::types::StreamsResponse,
+        crate::holo::types::Milestone,
+        crate::holo::types::MilestonesResponse,
+        crate::holo::types::NearMilestone,
+        crate::holo::types::NearMilestonesResponse,
+        crate::holo::types::MilestoneStats,
+        crate::holo::types::MilestoneStatsResponse,
     )),
     tags(
         (name = "auth", description = "Authentication endpoints"),
         (name = "docker", description = "Docker management endpoints"),
         (name = "status", description = "Status and monitoring endpoints"),
+        (name = "holo", description = "Typed admin contract for holo dashboard endpoints"),
     )
 )]
```

### 7-7. 새 파일: `admin-dashboard/backend/src/bin/export-openapi.rs`

현재 README와 `package.json`이 기대하지만 실제 파일이 없다. 이 빈 구멍을 반드시 메운다.

```diff
+++ b/admin-dashboard/backend/src/bin/export-openapi.rs
@@
+use utoipa::OpenApi;
+
+fn main() {
+    let openapi = admin_dashboard::openapi::ApiDoc::openapi();
+    println!(
+        "{}",
+        openapi
+            .to_pretty_json()
+            .expect("failed to serialize OpenAPI document")
+    );
+}
```

---

## 8. admin-dashboard frontend: generated client로 transport contract 일원화

원칙은 이렇다.

- 수동 `src/api/holo.ts` 제거
- feature별 `types.ts`는 generated DTO re-export만 하거나, 진짜 view-model만 남긴다
- feature `api.ts`는 generated client wrapper만 사용한다
- page는 query/mutation/modal/filter/select logic를 hook/selectors로 분리한다

### 8-1. `admin-dashboard/frontend/src/api/holo.ts` 삭제

```diff
--- a/admin-dashboard/frontend/src/api/holo.ts
+++ /dev/null
```

### 8-2. 새 파일: `admin-dashboard/frontend/src/api/holoClient.ts`

> 전제: backend OpenAPI에 `operation_id`를 고정했으므로 generated client method name도 안정적으로 생성된다.  
> `npm run generate:api` 재실행 후 사용한다.

```diff
+++ b/admin-dashboard/frontend/src/api/holoClient.ts
@@
+import { Admin } from '@/api/generated/Admin'
+import type {
+  AddAliasRequest,
+  AddRoomRequest,
+  AlarmsResponse,
+  ChannelStatsResponse,
+  DeleteAlarmRequest,
+  MembersResponse,
+  MilestoneStatsResponse,
+  MilestonesResponse,
+  NearMilestonesResponse,
+  RemoveAliasRequest,
+  RemoveRoomRequest,
+  RoomsResponse,
+  RoomNameUpdateRequest,
+  Settings,
+  SettingsResponse,
+  SetAclRequest,
+  SetAclResponse,
+  SetGraduationRequest,
+  StatusOnlyResponse,
+  StreamsResponse,
+  UpdateChannelRequest,
+  UpdateMemberNameRequest,
+  UserNameUpdateRequest,
+} from '@/api/generated/data-contracts'
+import { createApiClient } from '@/api/client'
+
+const adminClient = new Admin()
+adminClient.instance = createApiClient('')
+
+export const holoClient = {
+  getAlarms: async (): Promise<AlarmsResponse> => (await adminClient.holoGetAlarms()).data,
+  deleteAlarm: async (body: DeleteAlarmRequest): Promise<StatusOnlyResponse> => (await adminClient.holoDeleteAlarm(body)).data,
+  setRoomName: async (body: RoomNameUpdateRequest): Promise<StatusOnlyResponse> => (await adminClient.holoSetRoomName(body)).data,
+  setUserName: async (body: UserNameUpdateRequest): Promise<StatusOnlyResponse> => (await adminClient.holoSetUserName(body)).data,
+
+  getMembers: async (): Promise<MembersResponse> => (await adminClient.holoGetMembers()).data,
+  addMember: async (body: Partial<MembersResponse['members'][number]>): Promise<StatusOnlyResponse> => (await adminClient.holoAddMember(body)).data,
+  addAlias: async (id: number, body: AddAliasRequest): Promise<StatusOnlyResponse> => (await adminClient.holoAddAlias(String(id), body)).data,
+  removeAlias: async (id: number, body: RemoveAliasRequest): Promise<StatusOnlyResponse> => (await adminClient.holoRemoveAlias(String(id), body)).data,
+  setGraduation: async (id: number, body: SetGraduationRequest): Promise<StatusOnlyResponse> => (await adminClient.holoSetGraduation(String(id), body)).data,
+  updateChannel: async (id: number, body: UpdateChannelRequest): Promise<StatusOnlyResponse> => (await adminClient.holoUpdateChannel(String(id), body)).data,
+  updateMemberName: async (id: number, body: UpdateMemberNameRequest): Promise<StatusOnlyResponse> => (await adminClient.holoUpdateMemberName(String(id), body)).data,
+
+  getRooms: async (): Promise<RoomsResponse> => (await adminClient.holoGetRooms()).data,
+  addRoom: async (body: AddRoomRequest): Promise<StatusOnlyResponse> => (await adminClient.holoAddRoom(body)).data,
+  removeRoom: async (body: RemoveRoomRequest): Promise<StatusOnlyResponse> => (await adminClient.holoRemoveRoom(body)).data,
+  setAcl: async (body: SetAclRequest): Promise<SetAclResponse> => (await adminClient.holoSetAcl(body)).data,
+
+  getSettings: async (): Promise<SettingsResponse> => (await adminClient.holoGetSettings()).data,
+  updateSettings: async (body: Settings): Promise<StatusOnlyResponse> => (await adminClient.holoUpdateSettings(body)).data,
+
+  getStats: async (): Promise<import('@/api/generated/data-contracts').StatsResponse> => (await adminClient.holoGetStats()).data,
+  getChannelStats: async (): Promise<ChannelStatsResponse> => (await adminClient.holoGetChannelStats()).data,
+  getLiveStreams: async (org = 'hololive'): Promise<StreamsResponse> => (await adminClient.holoGetLiveStreams({ org })).data,
+  getUpcomingStreams: async (org = 'hololive'): Promise<StreamsResponse> => (await adminClient.holoGetUpcomingStreams({ org })).data,
+
+  getMilestones: async (params?: { limit?: number; offset?: number; channelId?: string; memberName?: string }): Promise<MilestonesResponse> =>
+    (await adminClient.holoGetMilestones(params ?? {})).data,
+  getNearMilestones: async (threshold = 0.9): Promise<NearMilestonesResponse> =>
+    (await adminClient.holoGetNearMilestones({ threshold })).data,
+  getMilestoneStats: async (): Promise<MilestoneStatsResponse> =>
+    (await adminClient.holoGetMilestoneStats()).data,
+}
```

### 8-3. feature `api.ts` 파일 교체

#### `admin-dashboard/frontend/src/features/alarms/api.ts`

```diff
--- a/admin-dashboard/frontend/src/features/alarms/api.ts
+++ b/admin-dashboard/frontend/src/features/alarms/api.ts
@@
-import { holoApi, type HoloApiResponse } from '@/api/holo'
-import type { AlarmsResponse } from './types'
+import { holoClient } from '@/api/holoClient'
+import type { DeleteAlarmRequest } from '@/api/generated/data-contracts'

-interface DeleteAlarmRequest {
-  roomId: string
-  userId: string
-  channelId: string
-}
-
 export const alarmsApi = {
-  getAll: async () => holoApi.get<AlarmsResponse>('/alarms'),
-
-  delete: async (request: DeleteAlarmRequest) => holoApi.delete<HoloApiResponse>('/alarms', {
-    data: request,
-  }),
+  getAll: holoClient.getAlarms,
+  delete: (request: DeleteAlarmRequest) => holoClient.deleteAlarm(request),
 }

 export const namesApi = {
-  setRoomName: async (roomId: string, roomName: string) => holoApi.post<HoloApiResponse>('/names/room', {
-    roomId,
-    roomName,
-  }),
-
-  setUserName: async (userId: string, userName: string) => holoApi.post<HoloApiResponse>('/names/user', {
-    userId,
-    userName,
-  }),
+  setRoomName: (roomId: string, roomName: string) => holoClient.setRoomName({ roomId, roomName }),
+  setUserName: (userId: string, userName: string) => holoClient.setUserName({ userId, userName }),
 }
```

같은 패턴을 아래 파일에도 적용한다.

- `features/members/api.ts`
- `features/rooms/api.ts`
- `features/settings/api.ts`
- `features/stats/api.ts`
- `features/streams/api.ts`
- `features/milestones/api.ts`

### 8-4. feature `types.ts`는 generated DTO re-export로 바꾼다

예: `features/alarms/types.ts`

```diff
--- a/admin-dashboard/frontend/src/features/alarms/types.ts
+++ b/admin-dashboard/frontend/src/features/alarms/types.ts
@@
-export interface Alarm {
-  roomId: string
-  roomName: string
-  userId: string
-  userName: string
-  channelId: string
-  memberName: string
-}
-
-export interface AlarmsResponse {
-  status: string
-  alarms: Alarm[]
-}
+export type {
+  Alarm,
+  AlarmsResponse,
+} from '@/api/generated/data-contracts'
```

같은 패턴을 아래 파일에도 적용한다.

- `features/members/types.ts`
- `features/rooms/types.ts`
- `features/settings/types.ts`
- `features/stats/types.ts`
- `features/streams/types.ts`
- `features/milestones/types.ts`

> 단, local-only union/type helper가 있는 경우에는 DTO re-export와 view-model type을 분리한다.  
> 예: `ACLMode`, `StreamOrg`, modal state union은 feature 로컬에 남겨도 된다.

### 8-5. giant page 분리

#### A. `features/alarms/selectors.ts`

```diff
+++ b/admin-dashboard/frontend/src/features/alarms/selectors.ts
@@
+import type { Alarm } from '@/features/alarms/types'
+import type { AlarmGroup } from '@/features/alarms/components/AlarmGroups'
+
+export function groupAlarms(alarms: Alarm[]): AlarmGroup[] {
+  const groups = new Map<string, AlarmGroup>()
+  alarms.forEach((alarm) => {
+    const key = `${alarm.roomId}:${alarm.userId}`
+    if (!groups.has(key)) {
+      groups.set(key, {
+        roomId: alarm.roomId,
+        roomName: alarm.roomName,
+        userId: alarm.userId,
+        userName: alarm.userName,
+        alarms: [],
+      })
+    }
+    groups.get(key)!.alarms.push(alarm)
+  })
+
+  return Array.from(groups.values()).sort((a, b) => {
+    if (a.roomName !== b.roomName) {
+      return a.roomName.localeCompare(b.roomName, 'ko')
+    }
+    return a.userName.localeCompare(b.userName, 'ko')
+  })
+}
+
+export function filterAlarmGroups(groups: AlarmGroup[], keyword: string): AlarmGroup[] {
+  const normalized = keyword.trim().toLowerCase()
+  if (!normalized) {
+    return groups
+  }
+  return groups.filter((group) =>
+    group.roomName.toLowerCase().includes(normalized) ||
+    group.userName.toLowerCase().includes(normalized) ||
+    group.alarms.some((alarm) => alarm.memberName.toLowerCase().includes(normalized)),
+  )
+}
```

#### B. `features/alarms/hooks/useAlarmsPage.ts`

```diff
+++ b/admin-dashboard/frontend/src/features/alarms/hooks/useAlarmsPage.ts
@@
+import { useDeferredValue, useEffect, useMemo, useState } from 'react'
+import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
+import { queryKeys } from '@/api/queryKeys'
+import { alarmsApi, namesApi } from '@/features/alarms/api'
+import { filterAlarmGroups, groupAlarms } from '@/features/alarms/selectors'
+import type { Alarm } from '@/features/alarms/types'
+
+const ALARM_GROUP_PAGE_SIZE = 20
+
+export function useAlarmsPage() {
+  const queryClient = useQueryClient()
+  const [search, setSearch] = useState('')
+  const deferredSearch = useDeferredValue(search)
+  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())
+  const [alarmToDelete, setAlarmToDelete] = useState<Alarm | null>(null)
+  const [visibleGroupCount, setVisibleGroupCount] = useState(ALARM_GROUP_PAGE_SIZE)
+  const [editModal, setEditModal] = useState<{ type: 'room' | 'user'; id: string; currentName: string } | null>(null)
+
+  const query = useQuery({
+    queryKey: queryKeys.alarms.all,
+    queryFn: alarmsApi.getAll,
+  })
+
+  const deleteAlarmMutation = useMutation({
+    mutationFn: alarmsApi.delete,
+    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: queryKeys.alarms.all }) },
+  })
+
+  const setNameMutation = useMutation({
+    mutationFn: async ({ type, id, name }: { type: 'room' | 'user'; id: string; name: string }) =>
+      type === 'room' ? namesApi.setRoomName(id, name) : namesApi.setUserName(id, name),
+    onSuccess: () => { void queryClient.invalidateQueries({ queryKey: queryKeys.alarms.all }) },
+  })
+
+  useEffect(() => {
+    setVisibleGroupCount(ALARM_GROUP_PAGE_SIZE)
+  }, [deferredSearch])
+
+  const groupedAlarms = useMemo(
+    () => groupAlarms(query.data?.alarms ?? []),
+    [query.data?.alarms],
+  )
+  const filteredGroups = useMemo(
+    () => filterAlarmGroups(groupedAlarms, deferredSearch),
+    [groupedAlarms, deferredSearch],
+  )
+  const totalAlarms = filteredGroups.reduce((sum, group) => sum + group.alarms.length, 0)
+
+  return {
+    search,
+    setSearch,
+    expandedGroups,
+    setExpandedGroups,
+    alarmToDelete,
+    setAlarmToDelete,
+    visibleGroupCount,
+    setVisibleGroupCount,
+    editModal,
+    setEditModal,
+    groupedAlarms,
+    filteredGroups,
+    totalAlarms,
+    query,
+    deleteAlarmMutation,
+    setNameMutation,
+  }
+}
```

#### C. `features/alarms/pages/AlarmsPage.tsx`

page는 hook과 selector 결과만 받아 렌더링하도록 줄인다.

```diff
--- a/admin-dashboard/frontend/src/features/alarms/pages/AlarmsPage.tsx
+++ b/admin-dashboard/frontend/src/features/alarms/pages/AlarmsPage.tsx
@@
-import { useDeferredValue, useEffect, useMemo, useState } from 'react'
-import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
 import Bell from 'lucide-react/dist/esm/icons/bell'
-import { queryKeys } from '@/api/queryKeys'
 import { ConfirmModal } from '@/components/ConfirmModal'
 import EditNameModal from '@/components/EditNameModal'
-import { alarmsApi, namesApi } from '@/features/alarms/api'
 import { AlarmGroups, type AlarmGroup } from '@/features/alarms/components/AlarmGroups'
 import { AlarmsToolbar } from '@/features/alarms/components/AlarmsToolbar'
-import type { Alarm } from '@/features/alarms/types'
+import { useAlarmsPage } from '@/features/alarms/hooks/useAlarmsPage'

-const ALARM_GROUP_PAGE_SIZE = 20
-
 export const AlarmsPage = () => {
-  const queryClient = useQueryClient()
-  ...
+  const {
+    search,
+    setSearch,
+    expandedGroups,
+    setExpandedGroups,
+    alarmToDelete,
+    setAlarmToDelete,
+    visibleGroupCount,
+    setVisibleGroupCount,
+    editModal,
+    setEditModal,
+    groupedAlarms,
+    filteredGroups,
+    totalAlarms,
+    query,
+    deleteAlarmMutation,
+    setNameMutation,
+  } = useAlarmsPage()
@@
-  if (isLoading) {
+  if (query.isLoading) {
@@
-  const confirmDelete = () => {
+  const confirmDelete = () => {
     if (!alarmToDelete) return
@@
-  const handleSaveName = (newName: string) => {
+  const handleSaveName = (newName: string) => {
@@
-  if (groupedAlarms.length === 0) {
+  if (groupedAlarms.length === 0) {
```

같은 방식으로 아래 giant page를 분리한다.

- `features/members/pages/MembersPage.tsx`
  - 새 파일:
    - `features/members/selectors.ts`
    - `features/members/hooks/useMembersPage.ts`
- `features/rooms/pages/RoomsPage.tsx`
  - 새 파일:
    - `features/rooms/hooks/useRoomsPage.ts`
- `features/stats/pages/StatsPage.tsx`
  - 새 파일:
    - `features/stats/selectors.ts`
    - `features/stats/hooks/useStatsPage.ts`

아래는 `members/selectors.ts`의 핵심만 제시한다.

```diff
+++ b/admin-dashboard/frontend/src/features/members/selectors.ts
@@
+import type { Member } from '@/features/members/types'
+
+export function cloneMembers(members: Member[]): Member[] {
+  return members.map((member) => ({
+    ...member,
+    aliases: {
+      ko: [...member.aliases.ko],
+      ja: [...member.aliases.ja],
+    },
+  }))
+}
+
+export function filterMembers(members: Member[], keyword: string, hideGraduated: boolean): Member[] {
+  const normalized = keyword.trim().toLowerCase()
+  return members.filter((member) => {
+    if (hideGraduated && member.isGraduated) {
+      return false
+    }
+    if (!normalized) {
+      return true
+    }
+    return (
+      member.name.toLowerCase().includes(normalized) ||
+      member.channelId.toLowerCase().includes(normalized) ||
+      String(member.id).includes(normalized) ||
+      member.aliases.ko.some((alias) => alias.toLowerCase().includes(normalized)) ||
+      member.aliases.ja.some((alias) => alias.toLowerCase().includes(normalized))
+    )
+  })
+}
+
+export function sortMembers(members: Member[]): Member[] {
+  return [...members].sort((first, second) => {
+    if (first.isGraduated !== second.isGraduated) {
+      return first.isGraduated ? 1 : -1
+    }
+    return first.name.localeCompare(second.name)
+  })
+}
```

---

## 9. admin-dashboard 문서 / OpenAPI 파이프라인 마감

### 9-1. `admin-dashboard/README.md`

`export-openapi.rs`가 이제 실제로 존재하므로, README는 그대로 두되 아래 문장을 추가한다.

```diff
--- a/admin-dashboard/README.md
+++ b/admin-dashboard/README.md
@@
 ## OpenAPI generation
@@
 cargo run --bin export-openapi > docs/swagger.json
 cd frontend
 npm run generate:api
+
+주의:
+- `src/api/generated/*`는 수동 수정 금지
+- holo dashboard endpoint는 `/admin/api/holo/*` typed contract로만 추가
+- blind proxy route는 websocket / 미이관 compatibility fallback 용도로만 사용
```

### 9-2. `admin-dashboard/docs/openapi-pipeline.md`

현재 문서가 예전 generator 내용을 섞고 있으면 다음으로 정리한다.

```diff
--- a/admin-dashboard/docs/openapi-pipeline.md
+++ b/admin-dashboard/docs/openapi-pipeline.md
@@
-    "generate:api": "openapi-generator-cli generate -i ../backend/docs/swagger.json -g typescript-fetch -o src/api/generated --additional-properties=supportsES6=true,typescriptThreePlus=true"
+    "generate:api": "mkdir -p ../backend/docs && (cd ../backend && cargo run --quiet --bin export-openapi > docs/swagger.json) && swagger-typescript-api generate -p ../backend/docs/swagger.json -o src/api/generated --axios --modular"
```

---

## 10. 테스트 추가

최소 이 정도는 같이 넣어야 한다.

### 10-1. Go

1. `helpers_test.go`
   - late discovery가 5분 알람을 backfill하지 않는지
   - exact target과 bounded-crossed target이 정상인지

2. `youtube_checker_test.go`
   - evaluation window cap 적용 시 5분 late-backfill 방지
   - 3분/1분 target은 정상 발송

3. `runtime_scheduler_additional_test.go`
   - dynamic target minute sync가 계속 동작하는지
   - aligned loop가 drift를 누적하지 않는지

4. `stream_ingester_poller_registrations_test.go`
   - 새 env alias 이름이 poll registration에 반영되는지

5. `youtube_providers_test.go`
   - poll budget warning 계산 helper가 interval 0을 무시하는지
   - active member 수 기준으로 expected total RPM을 계산하는지

### 10-2. Rust backend

1. `backend/src/holo/client.rs`
   - API key header가 주입되는지
   - query/body serialization이 예상대로 되는지

2. `backend/src/holo/handlers.rs`
   - members/alarms/rooms/settings/stats/streams/milestones 각 핸들러가 typed body를 내리는지
   - upstream 5xx/invalid JSON에서 502로 실패하는지

3. `backend/src/openapi.rs`
   - exported schema에 holo path가 포함되는지

### 10-3. Frontend

1. `features/alarms/selectors.test.ts`
   - room/user grouping, keyword filter
2. `features/members/selectors.test.ts`
   - graduated filtering, alias keyword match, stable sort
3. `features/stats/selectors.test.ts`
   - currentServiceStats fallback
4. smoke test:
   - `npm run generate:api`
   - `npm run build`

---

## 11. 삭제 / 이동 체크리스트

적용 후 정리해야 할 항목이다.

### 삭제

- `admin-dashboard/frontend/src/api/holo.ts`
- `hololive-kakao-bot-go/internal/app/providers/infra_resources.go`
- `hololive-kakao-bot-go/internal/app/providers/youtube.go`
- `hololive-kakao-bot-go/internal/app/providers/settings.go`
- `hololive-stream-ingester/internal/app/providers/infra_resources.go`
- `hololive-stream-ingester/internal/app/providers/youtube.go`
- `hololive-stream-ingester/internal/app/providers/settings.go`

### 축소된 책임

- `admin-dashboard/backend/src/proxy/bot_proxy.rs`
  - 더 이상 dashboard-owned JSON feature endpoint의 primary path가 아님
  - websocket/compat fallback 전용

- `hololive-shared/pkg/providers/infra_providers.go`
  - resource creator만 남기고 config extractor/service extractor wrapper는 단계적 삭제

- `hololive-shared/pkg/providers/youtube_providers.go`
  - `ProvideScraperScheduler`만 유지
  - 나머지 얇은 wrapper는 module builder로 흡수 후 제거

---

## 12. 최종 마감 기준

이 patch set이 끝나면 아래가 만족되어야 한다.

1. `.env`에 scraper poll cadence를 넣으면 실제 scheduler에 반영된다.
2. YouTube upcoming 알람은 늦게 발견된 스트림에 대해 5분 알람을 뒤늦게 backfill하지 않는다.
3. 저장소 기본 빌드/배포 체인은 외부 `../llm/shared-go`, `../iris-client-go`를 요구하지 않는다.
4. bot / stream-ingester / llm-sched의 HTTP server/router boilerplate가 shared helper 기준으로 정리된다.
5. runtime `Close()` wrapper 중복이 없어진다.
6. runtime-owned provider duplication이 shared module builder로 치환된다.
7. admin-dashboard는 blind proxy가 아니라 typed admin contract를 소유하고, OpenAPI export가 실제로 동작한다.
8. frontend는 generated client만 transport DTO source로 사용한다.
9. giant page 컴포넌트는 page/hook/selectors로 분해된다.

---

## 13. 마지막 메모

이번 저장소는 "함수 몇 개 더 고치면 되는 수준"을 이미 지났다.  
핵심은 세 가지였다.

- 시간 의미를 제대로 복원하는 것
- 설정 의미를 실제 runtime까지 전파하는 것
- 조립 문법과 얇은 wrapper를 걷어내고 ownership을 다시 세우는 것

위 patch set은 그 세 가지를 repo 전체 범위에서 동시에 닫는 방향이다.  
운영 버그를 먼저 멈추고, 그 위에서 self-contained build와 admin contract ownership까지 정리하는 순서가 맞다.
