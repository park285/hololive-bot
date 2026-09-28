package scraping

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	scraperFetchMetricsOnce sync.Once

	scraperFetchRequestsTotal *prometheus.CounterVec
	scraperFetchDuration      *prometheus.HistogramVec
)

// scraperFetchEngineLabel은 fetch metric의 engine 라벨 값이다. 대체 엔진(browser snapshot)과 fetcher fallback을 지워
// net/http 하나만 남았지만, 기존 series와 dashboard 질의를 끊지 않도록 라벨 이름과 값을 그대로 둔다.
const scraperFetchEngineLabel = "nethttp"

func ensureScraperFetchMetrics() {
	scraperFetchMetricsOnce.Do(func() {
		scraperFetchRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_scraper_fetch_requests_total",
			Help: "YouTube scraper fetch request outcomes by fetcher engine",
		}, []string{"engine", "outcome", "reason", "status_code"})
		scraperFetchDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hololive_youtube_scraper_fetch_duration_seconds",
			Help:    "YouTube scraper fetch request duration by fetcher engine",
			Buckets: prometheus.DefBuckets,
		}, []string{"engine", "outcome", "reason"})
	})
}

func init() {
	ensureScraperFetchMetrics()
}

func observeScraperFetch(statusCode int, err error, elapsed time.Duration) {
	ensureScraperFetchMetrics()

	outcome, reason := fetchMetricOutcome(err)
	scraperFetchRequestsTotal.WithLabelValues(scraperFetchEngineLabel, outcome, reason, fetchStatusCodeLabel(statusCode)).Inc()
	scraperFetchDuration.WithLabelValues(scraperFetchEngineLabel, outcome, reason).Observe(elapsed.Seconds())
}

func fetchMetricOutcome(err error) (outcome, reason string) {
	if err == nil {
		return "success", string(FailureReasonNone)
	}

	detail := ClassifyFailure(err, FailureSourceHTML)

	return "error", string(detail.Reason)
}

func fetchStatusCodeLabel(statusCode int) string {
	if statusCode <= 0 {
		return "none"
	}

	return strconv.Itoa(statusCode)
}
