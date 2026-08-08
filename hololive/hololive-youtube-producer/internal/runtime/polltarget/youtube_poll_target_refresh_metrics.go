package polltarget

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	youTubePollTargetMetricsOnce sync.Once

	youtubePollTargetRefreshTotal                *prometheus.CounterVec
	youtubePollTargetRefreshLastSuccessTimestamp *prometheus.GaugeVec
	youtubePollTargetRefreshAcceptedTargetCount  *prometheus.GaugeVec
	youtubePollTargetRefreshDBValidationTotal    *prometheus.CounterVec
)

func ensureYouTubePollTargetMetrics() {
	youTubePollTargetMetricsOnce.Do(func() {
		youtubePollTargetRefreshTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_youtube_poll_target_refresh_total",
			Help: "YouTube poll target refresh outcomes",
		}, []string{"result"})
		youtubePollTargetRefreshLastSuccessTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hololive_youtube_poll_target_refresh_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful YouTube poll target refresh",
		}, nil)
		youtubePollTargetRefreshAcceptedTargetCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hololive_youtube_poll_target_refresh_accepted_target_count",
			Help: "Accepted YouTube poll target counts by target type",
		}, []string{"target_type"})
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

func observeYouTubePollTargetRefreshSuccess(at time.Time, targets Targets) {
	ensureYouTubePollTargetMetrics()
	youtubePollTargetRefreshTotal.WithLabelValues("success").Inc()
	youtubePollTargetRefreshLastSuccessTimestamp.WithLabelValues().Set(float64(at.Unix()))
	youtubePollTargetRefreshAcceptedTargetCount.WithLabelValues("notification").Set(float64(len(targets.NotificationChannelIDs)))
	youtubePollTargetRefreshAcceptedTargetCount.WithLabelValues("operational").Set(float64(len(targets.OperationalChannelIDs)))
}

func observeYouTubePollTargetRefreshError() {
	ensureYouTubePollTargetMetrics()
	youtubePollTargetRefreshTotal.WithLabelValues("error").Inc()
}
