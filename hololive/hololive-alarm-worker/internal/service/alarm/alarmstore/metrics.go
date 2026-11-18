package alarmstore

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	subscriberDBFallbackHit   = "hit"
	subscriberDBFallbackMiss  = "miss"
	subscriberDBFallbackError = "error"
)

var (
	alarmSubscriberDBFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_alarm_subscriber_db_fallback_total",
		Help: "subscriber lookups that read the DB because the cache could not resolve them",
	}, []string{"result"})
	alarmSubscriberDBSingleflightSharedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hololive_alarm_subscriber_db_singleflight_shared_total",
		Help: "subscriber DB fallback lookups served from a shared singleflight query",
	})
	alarmSubscriberCacheErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_alarm_subscriber_cache_error_total",
		Help: "subscriber lookup best-effort cache write errors",
	}, []string{"operation"})
)

func init() {
	for _, result := range []string{subscriberDBFallbackHit, subscriberDBFallbackMiss, subscriberDBFallbackError} {
		alarmSubscriberDBFallbackTotal.WithLabelValues(result)
	}
}

func observeAlarmSubscriberDBFallback(result string) {
	alarmSubscriberDBFallbackTotal.WithLabelValues(result).Inc()
}

func observeAlarmSubscriberDBSingleflightShared() {
	alarmSubscriberDBSingleflightSharedTotal.Inc()
}

func observeAlarmSubscriberCacheError(operation string) {
	alarmSubscriberCacheErrorTotal.WithLabelValues(operation).Inc()
}
