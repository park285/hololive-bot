package runtime

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	youTubePollTargetMetricsOnce sync.Once

	youtubePollTargetRefreshCacheShrinkValidationTotal *prometheus.CounterVec
)

func ensureYouTubePollTargetMetrics() {
	youTubePollTargetMetricsOnce.Do(func() {
		youtubePollTargetRefreshCacheShrinkValidationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_poll_target_refresh_cache_shrink_validation_total",
			Help: "YouTube poll target refresh shrink validation outcomes",
		}, []string{"result"})
	})
}

func init() {
	ensureYouTubePollTargetMetrics()
}

func observeYouTubePollTargetShrinkValidation(result string) {
	ensureYouTubePollTargetMetrics()
	youtubePollTargetRefreshCacheShrinkValidationTotal.WithLabelValues(result).Inc()
}
