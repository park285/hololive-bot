package runtime

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	youTubePollTargetMetricsOnce sync.Once

	youtubePollTargetRefreshDBValidationTotal *prometheus.CounterVec
)

func ensureYouTubePollTargetMetrics() {
	youTubePollTargetMetricsOnce.Do(func() {
		youtubePollTargetRefreshDBValidationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_poll_target_refresh_db_validation_total",
			Help: "YouTube poll target refresh DB validation outcomes",
		}, []string{"result"})
	})
}

func init() {
	ensureYouTubePollTargetMetrics()
}

func observeYouTubePollTargetValidation(result string) {
	ensureYouTubePollTargetMetrics()
	youtubePollTargetRefreshDBValidationTotal.WithLabelValues(result).Inc()
}
