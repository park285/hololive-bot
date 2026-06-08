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
	scraperFetchFallbackTotal *prometheus.CounterVec
)

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
		scraperFetchFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_scraper_fetch_fallback_total",
			Help: "YouTube scraper fetcher fallback outcomes",
		}, []string{"from_engine", "to_engine", "reason"})
	})
}

func init() {
	ensureScraperFetchMetrics()
}

func observeScraperFetch(engine FetcherEngine, statusCode int, err error, elapsed time.Duration) {
	ensureScraperFetchMetrics()
	outcome, reason := fetchMetricOutcome(err)
	engineLabel := fetcherEngineMetricLabel(engine)
	scraperFetchRequestsTotal.WithLabelValues(engineLabel, outcome, reason, fetchStatusCodeLabel(statusCode)).Inc()
	scraperFetchDuration.WithLabelValues(engineLabel, outcome, reason).Observe(elapsed.Seconds())
}

func observeScraperFetchFallback(fromEngine FetcherEngine, toEngine FetcherEngine, err error) {
	ensureScraperFetchMetrics()
	_, reason := fetchMetricOutcome(err)
	scraperFetchFallbackTotal.WithLabelValues(
		fetcherEngineMetricLabel(fromEngine),
		fetcherEngineMetricLabel(toEngine),
		reason,
	).Inc()
}

func fetchMetricOutcome(err error) (string, string) {
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

func fetcherEngineMetricLabel(engine FetcherEngine) string {
	return string(normalizeFetcherEngine(engine))
}
