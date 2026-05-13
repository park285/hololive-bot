package youtube

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	youtubeScraperMetricsOnce sync.Once
	youtubeScraperFailures    *prometheus.CounterVec
	youtubeScraperRecoveries  *prometheus.CounterVec
)

func initYouTubeScraperMetrics() {
	youtubeScraperMetricsOnce.Do(func() {
		youtubeScraperFailures = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "youtube_scraper_channel_failures_total",
			Help: "YouTube scraper channel failures by operation, source, and reason.",
		}, []string{"operation", "source", "reason"})
		youtubeScraperRecoveries = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "youtube_scraper_channel_recoveries_total",
			Help: "YouTube scraper fallback recoveries by operation, failed_source, failed_reason, recovery_source.",
		}, []string{"operation", "failed_source", "failed_reason", "recovery_source"})
	})
}

func observeYouTubeScraperFailure(operation, source, reason string) {
	initYouTubeScraperMetrics()
	youtubeScraperFailures.WithLabelValues(operation, source, reason).Inc()
}

func observeYouTubeScraperRecovery(operation, failedSource, failedReason, recoverySource string) {
	initYouTubeScraperMetrics()
	youtubeScraperRecoveries.WithLabelValues(operation, failedSource, failedReason, recoverySource).Inc()
}
