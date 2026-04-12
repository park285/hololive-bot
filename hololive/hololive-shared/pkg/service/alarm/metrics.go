package alarm

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	alarmMetricsOnce sync.Once

	alarmSubscriberDBFallbackTotal           *prometheus.CounterVec
	alarmSubscriberDBSingleflightSharedTotal prometheus.Counter
)

func ensureAlarmMetrics() {
	alarmMetricsOnce.Do(func() {
		alarmSubscriberDBFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_alarm_subscriber_db_fallback_total",
			Help: "subscriber lookup DB fallback execution outcomes",
		}, []string{"result"})
		alarmSubscriberDBSingleflightSharedTotal = promauto.NewCounter(prometheus.CounterOpts{
			Name: "hololive_alarm_subscriber_db_singleflight_shared_total",
			Help: "subscriber DB fallback lookups served from a shared singleflight query",
		})
	})
}

func init() {
	ensureAlarmMetrics()
}

func observeAlarmSubscriberDBFallback(result string) {
	ensureAlarmMetrics()
	alarmSubscriberDBFallbackTotal.WithLabelValues(result).Inc()
}

func observeAlarmSubscriberDBSingleflightShared() {
	ensureAlarmMetrics()
	alarmSubscriberDBSingleflightSharedTotal.Inc()
}
