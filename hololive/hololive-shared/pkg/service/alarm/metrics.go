package alarm

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// subscriber DB fallback 결과 label. 모든 값을 기동 때 0으로 만들어 첫 관측 전에도 series가 있게 한다.
const (
	subscriberDBFallbackHit   = "hit"
	subscriberDBFallbackMiss  = "miss"
	subscriberDBFallbackError = "error"
)

var (
	alarmMetricsOnce sync.Once

	alarmSubscriberDBFallbackTotal           *prometheus.CounterVec
	alarmSubscriberDBSingleflightSharedTotal prometheus.Counter
	alarmSubscriberCacheErrorTotal           *prometheus.CounterVec
	alarmMemberNameFallbackChannels          prometheus.Gauge
)

func ensureAlarmMetrics() {
	alarmMetricsOnce.Do(func() {
		alarmSubscriberDBFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_alarm_subscriber_db_fallback_total",
			Help: "subscriber lookup DB fallback execution outcomes",
		}, []string{"result"})

		for _, result := range []string{subscriberDBFallbackHit, subscriberDBFallbackMiss, subscriberDBFallbackError} {
			alarmSubscriberDBFallbackTotal.WithLabelValues(result)
		}

		alarmSubscriberDBSingleflightSharedTotal = promauto.NewCounter(prometheus.CounterOpts{
			Name: "hololive_alarm_subscriber_db_singleflight_shared_total",
			Help: "subscriber DB fallback lookups served from a shared singleflight query",
		})
		alarmSubscriberCacheErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_alarm_subscriber_cache_error_total",
			Help: "subscriber lookup best-effort cache write errors",
		}, []string{"operation"})
		alarmMemberNameFallbackChannels = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "hololive_alarm_member_name_fallback_channels",
			Help: "alarm channels whose display name came from the latest alarms.member_name because members has no Korean display name (last member-name load)",
		})
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

func observeAlarmMemberNameFallbackChannels(count int) {
	ensureAlarmMetrics()

	if alarmMemberNameFallbackChannels == nil {
		return
	}

	alarmMemberNameFallbackChannels.Set(float64(count))
}

func observeAlarmSubscriberCacheError(operation string) {
	ensureAlarmMetrics()

	if alarmSubscriberCacheErrorTotal == nil {
		return
	}

	alarmSubscriberCacheErrorTotal.WithLabelValues(operation).Inc()
}
