package apiservice

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	youtubeProducerScrapeMetricsOnce sync.Once
	youtubeProducerScrapeFailures    *prometheus.CounterVec
	youtubeProducerScrapeRecoveries  *prometheus.CounterVec
)

func initYouTubeProducerScrapeMetrics() {
	youtubeProducerScrapeMetricsOnce.Do(func() {
		youtubeProducerScrapeFailures = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "youtube_producer_channel_failures_total",
			Help: "YouTube producer channel failures by operation, source, and reason.",
		}, []string{"operation", "source", "reason"})
		youtubeProducerScrapeRecoveries = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "youtube_producer_channel_recoveries_total",
			Help: "YouTube producer fallback recoveries by operation, failed_source, failed_reason, recovery_source.",
		}, []string{"operation", "failed_source", "failed_reason", "recovery_source"})
	})
}

func observeYouTubeProducerScrapeFailure(operation, source, reason string) {
	initYouTubeProducerScrapeMetrics()
	youtubeProducerScrapeFailures.WithLabelValues(operation, source, reason).Inc()
}

func observeYouTubeProducerScrapeRecovery(operation, failedSource, failedReason, recoverySource string) {
	initYouTubeProducerScrapeMetrics()
	youtubeProducerScrapeRecoveries.WithLabelValues(operation, failedSource, failedReason, recoverySource).Inc()
}
