# Phase 03. Upcoming scraper/API fallback 관찰성 강화

## 목표

현재 upcoming 흐름은 scraper 우선, 실패 채널만 API fallback입니다. 구조는 맞습니다. 문제는 실패 reason/source/recovery가 결과에 남지 않는다는 점입니다.

이 phase에서는 다음을 남깁니다.

- 어떤 채널이 어떤 source에서 실패했는지
- 실패 reason은 무엇인지
- API fallback이 어떤 실패를 복구했는지
- API fallback도 실패한 채널은 무엇인지

## 코드 레벨 의사결정

1. `fallback.RunPrimary` 자체는 건드리지 않습니다.
   - 공통 executor를 크게 바꾸면 다른 서비스에 영향이 큽니다.
   - `scrapeUpcomingStreams` 내부에서 failure slice를 별도 수집합니다.

2. metric label에는 `channel_id`를 넣지 않습니다.
   - channel별 상세는 log/state store에 남깁니다.

3. `events == 0`은 실패가 아닙니다.
   - upcoming/live가 없는 채널은 정상입니다.
   - 이 경우 API fallback을 호출하지 않습니다.

## 변경 대상

- `service_upcoming.go`
- `service_upcoming_scrape.go`
- `service_upcoming_fallback.go`
- `service_upcoming_failure.go` 신규
- `metrics_scraper.go` 신규

## Diff

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/service_upcoming.go b/hololive/hololive-shared/pkg/service/youtube/service_upcoming.go
index f395778..111aaaa 100644
--- a/hololive/hololive-shared/pkg/service/youtube/service_upcoming.go
+++ b/hololive/hololive-shared/pkg/service/youtube/service_upcoming.go
@@
 import (
 	"context"
 	"log/slog"
+	"time"

 	"github.com/kapu/hololive-shared/pkg/constants"
 	"github.com/kapu/hololive-shared/pkg/domain"
 )
@@
 type upcomingAPIFallbackResult struct {
 	streams            []*domain.Stream
 	quotaCost          int
 	successfulChannels int
+	successfulIDs      []string
+	failedIDs          []string
+	failures           []upcomingScrapeFailure
 }

 type upcomingScrapeResult struct {
 	streams   []*domain.Stream
 	failedIDs []string
 	scraped   int
+	failures  []upcomingScrapeFailure
 }
+
+type upcomingScrapeFailure struct {
+	ChannelID  string
+	Source     string
+	Reason     string
+	StatusCode int
+	RetryAfter time.Duration
+	Message    string
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/service_upcoming_failure.go b/hololive/hololive-shared/pkg/service/youtube/service_upcoming_failure.go
new file mode 100644
index 0000000..222aaaa
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/service_upcoming_failure.go
@@
+package youtube
+
+import (
+	"errors"
+	"net/http"
+
+	"google.golang.org/api/googleapi"
+
+	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
+)
+
+func summarizeUpcomingScrapeFailures(failures []upcomingScrapeFailure) map[string]int {
+	summary := make(map[string]int)
+	for _, failure := range failures {
+		key := failure.Source + ":" + failure.Reason
+		summary[key]++
+	}
+	return summary
+}
+
+func upcomingFailureByChannel(failures []upcomingScrapeFailure) map[string]upcomingScrapeFailure {
+	out := make(map[string]upcomingScrapeFailure, len(failures))
+	for _, failure := range failures {
+		if failure.ChannelID == "" {
+			continue
+		}
+		out[failure.ChannelID] = failure
+	}
+	return out
+}
+
+func classifyYouTubeAPIFailure(err error) scraper.FailureDetail {
+	detail := scraper.FailureDetail{
+		Reason:  scraper.FailureReasonUnknown,
+		Source:  scraper.FailureSourceAPI,
+		Message: errString(err),
+	}
+	var apiErr *googleapi.Error
+	if errors.As(err, &apiErr) {
+		detail.StatusCode = apiErr.Code
+		switch apiErr.Code {
+		case http.StatusForbidden:
+			detail.Reason = scraper.FailureReasonForbidden
+		case http.StatusTooManyRequests:
+			detail.Reason = scraper.FailureReasonRateLimited
+		case http.StatusRequestTimeout:
+			detail.Reason = scraper.FailureReasonTimeout
+		default:
+			detail.Reason = scraper.FailureReasonHTTPStatus
+		}
+		return detail
+	}
+	return scraper.ClassifyFailure(err, scraper.FailureSourceAPI)
+}
+
+func errString(err error) string {
+	if err == nil {
+		return ""
+	}
+	return err.Error()
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/metrics_scraper.go b/hololive/hololive-shared/pkg/service/youtube/metrics_scraper.go
new file mode 100644
index 0000000..333aaaa
--- /dev/null
+++ b/hololive/hololive-shared/pkg/service/youtube/metrics_scraper.go
@@
+package youtube
+
+import (
+	"sync"
+
+	"github.com/prometheus/client_golang/prometheus"
+	"github.com/prometheus/client_golang/prometheus/promauto"
+)
+
+var (
+	youtubeScraperMetricsOnce sync.Once
+	youtubeScraperFailures    *prometheus.CounterVec
+	youtubeScraperRecoveries  *prometheus.CounterVec
+)
+
+func initYouTubeScraperMetrics() {
+	youtubeScraperMetricsOnce.Do(func() {
+		youtubeScraperFailures = promauto.NewCounterVec(prometheus.CounterOpts{
+			Name: "youtube_scraper_channel_failures_total",
+			Help: "YouTube scraper channel failures by operation, source, and reason.",
+		}, []string{"operation", "source", "reason"})
+
+		youtubeScraperRecoveries = promauto.NewCounterVec(prometheus.CounterOpts{
+			Name: "youtube_scraper_channel_recoveries_total",
+			Help: "YouTube scraper fallback recoveries by operation, failed_source, failed_reason, recovery_source.",
+		}, []string{"operation", "failed_source", "failed_reason", "recovery_source"})
+	})
+}
+
+func observeYouTubeScraperFailure(operation, source, reason string) {
+	initYouTubeScraperMetrics()
+	youtubeScraperFailures.WithLabelValues(operation, source, reason).Inc()
+}
+
+func observeYouTubeScraperRecovery(operation, failedSource, failedReason, recoverySource string) {
+	initYouTubeScraperMetrics()
+	youtubeScraperRecoveries.WithLabelValues(operation, failedSource, failedReason, recoverySource).Inc()
+}
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/service_upcoming_scrape.go b/hololive/hololive-shared/pkg/service/youtube/service_upcoming_scrape.go
index fa4f217..444aaaa 100644
--- a/hololive/hololive-shared/pkg/service/youtube/service_upcoming_scrape.go
+++ b/hololive/hololive-shared/pkg/service/youtube/service_upcoming_scrape.go
@@
 	primary := fallback.RunPrimary(ctx, channelIDs, fallback.FetchPlan[string, struct{}]{Parallelism: 5}, func(gctx context.Context, channelID string) error {
 		events, err := ys.scraper.GetUpcomingEvents(gctx, channelID)
 		if err != nil {
+			detail := scraper.ClassifyFailure(err, scraper.FailureSourceHTML)
+			mu.Lock()
+			result.failures = append(result.failures, upcomingScrapeFailure{
+				ChannelID:  channelID,
+				Source:     string(detail.Source),
+				Reason:     string(detail.Reason),
+				StatusCode: detail.StatusCode,
+				RetryAfter: detail.RetryAfter,
+				Message:    detail.Message,
+			})
+			mu.Unlock()
+			observeYouTubeScraperFailure("upcoming_streams", string(detail.Source), string(detail.Reason))
+			ys.logger.Warn("youtube_upcoming_scraper_channel_failed",
+				slog.String("channelID", channelID),
+				slog.String("source", string(detail.Source)),
+				slog.String("reason", string(detail.Reason)),
+				slog.Int("statusCode", detail.StatusCode),
+				slog.Duration("retryAfter", detail.RetryAfter),
+				slog.Any("error", err))
 			return fmt.Errorf("scraper upcoming events for %s: %w", channelID, err)
 		}
-		if len(events) == 0 {
-			return nil
-		}

 		streams := ys.convertScrapedEvents(events, channelID)
 		mu.Lock()
 		result.streams = append(result.streams, streams...)
@@
 	ys.logger.Info("Scraper phase completed (upcoming streams)",
 		slog.Int("total", len(channelIDs)),
 		slog.Int("scraped", result.scraped),
-		slog.Int("failed", len(result.failedIDs)))
+		slog.Int("failed", len(result.failedIDs)),
+		slog.Any("failureSummary", summarizeUpcomingScrapeFailures(result.failures)))
 	return result
 }
```

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/service_upcoming_fallback.go b/hololive/hololive-shared/pkg/service/youtube/service_upcoming_fallback.go
index 51646be..555aaaa 100644
--- a/hololive/hololive-shared/pkg/service/youtube/service_upcoming_fallback.go
+++ b/hololive/hololive-shared/pkg/service/youtube/service_upcoming_fallback.go
@@
 	"golang.org/x/sync/errgroup"
 	"google.golang.org/api/googleapi"
 	"google.golang.org/api/youtube/v3"

 	"github.com/kapu/hololive-shared/pkg/constants"
 	"github.com/kapu/hololive-shared/pkg/domain"
 	"github.com/kapu/hololive-shared/pkg/service/fallback"
+	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
 )
@@
 			apiResult := ys.fetchUpcomingFromAPI(runCtx, scrapeResult.failedIDs)
 			allStreams = append(allStreams, apiResult.streams...)
 			ys.consumeQuota(apiResult.quotaCost)
+			ys.observeUpcomingFallbackRecoveries(scrapeResult, apiResult)

 			return fallback.SecondaryResult{
 				Items:     len(apiResult.streams),
@@
 func (ys *serviceImpl) fetchUpcomingFromAPI(ctx context.Context, channelIDs []string) upcomingAPIFallbackResult {
 	result := upcomingAPIFallbackResult{
 		streams: make([]*domain.Stream, 0, len(channelIDs)),
@@
 	for _, channelID := range channelIDs {
+		channelID := channelID
 		g.Go(func() error {
 			streams, err := ys.getChannelUpcomingStreams(gctx, channelID)
 			if err != nil {
+				detail := classifyYouTubeAPIFailure(err)
 				ys.logger.Warn("Failed to fetch channel from API",
 					slog.String("channelID", channelID),
+					slog.String("source", string(scraper.FailureSourceAPI)),
+					slog.String("reason", string(detail.Reason)),
+					slog.Int("statusCode", detail.StatusCode),
 					slog.Any("error", err))
+				mu.Lock()
+				result.failedIDs = append(result.failedIDs, channelID)
+				result.failures = append(result.failures, upcomingScrapeFailure{
+					ChannelID:  channelID,
+					Source:     string(scraper.FailureSourceAPI),
+					Reason:     string(detail.Reason),
+					StatusCode: detail.StatusCode,
+					RetryAfter: detail.RetryAfter,
+					Message:    detail.Message,
+				})
+				mu.Unlock()
 				return nil
 			}

 			mu.Lock()
 			result.streams = append(result.streams, streams...)
 			result.successfulChannels++
+			result.successfulIDs = append(result.successfulIDs, channelID)
 			mu.Unlock()
@@
 	return result
 }
+
+func (ys *serviceImpl) observeUpcomingFallbackRecoveries(scrapeResult upcomingScrapeResult, apiResult upcomingAPIFallbackResult) {
+	failuresByChannel := upcomingFailureByChannel(scrapeResult.failures)
+	for _, channelID := range apiResult.successfulIDs {
+		failure, ok := failuresByChannel[channelID]
+		if !ok {
+			continue
+		}
+		observeYouTubeScraperRecovery(
+			"upcoming_streams",
+			failure.Source,
+			failure.Reason,
+			string(scraper.FailureSourceAPI),
+		)
+		ys.logger.Info("youtube_upcoming_api_fallback_recovered_channel",
+			slog.String("channelID", channelID),
+			slog.String("failedSource", failure.Source),
+			slog.String("failedReason", failure.Reason),
+			slog.String("recoverySource", string(scraper.FailureSourceAPI)))
+	}
+	for _, failure := range apiResult.failures {
+		ys.logger.Warn("youtube_upcoming_api_fallback_unrecovered_channel",
+			slog.String("channelID", failure.ChannelID),
+			slog.String("source", failure.Source),
+			slog.String("reason", failure.Reason),
+			slog.Int("statusCode", failure.StatusCode))
+	}
+}
```

## 실행

```bash
go test ./hololive/hololive-shared/pkg/service/youtube -run 'Upcoming|Fallback'
go test ./hololive/hololive-shared/pkg/service/fallback
```

## 완료 기준

- scraper 실패가 reason/source별 metric으로 증가합니다.
- API fallback으로 복구된 채널이 recovery metric으로 증가합니다.
- upcoming 없는 정상 채널은 실패로 기록되지 않습니다.
- `failedIDs` 기반 기존 fallback 흐름은 유지됩니다.
