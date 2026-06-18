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
	alarmSubscriberCacheErrorTotal           *prometheus.CounterVec
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
		alarmSubscriberCacheErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_alarm_subscriber_cache_error_total",
			Help: "subscriber lookup best-effort cache write errors",
		}, []string{"operation"})
	})
}

func init() {
	ensureAlarmMetrics()
}

func observeAlarmSubscriberDBFallback(result string) {
	ensureAlarmMetrics()
	if alarmSubscriberDBFallbackTotal == nil {
		return
	}
	alarmSubscriberDBFallbackTotal.WithLabelValues(result).Inc()
}

func observeAlarmSubscriberDBSingleflightShared() {
	ensureAlarmMetrics()
	if alarmSubscriberDBSingleflightSharedTotal == nil {
		return
	}
	alarmSubscriberDBSingleflightSharedTotal.Inc()
}

func observeAlarmSubscriberCacheError(operation string) {
	ensureAlarmMetrics()
	if alarmSubscriberCacheErrorTotal == nil {
		return
	}
	alarmSubscriberCacheErrorTotal.WithLabelValues(operation).Inc()
}
