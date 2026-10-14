package alarm

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// subscriber DB fallback 결과 label. 모든 값을 기동 때 0으로 만들어 첫 관측 전에도 series가 있게 한다.
const (
	subscriberDBFallbackHit   = "hit"
	subscriberDBFallbackMiss  = "miss"
	subscriberDBFallbackError = "error"
)

// 패키지를 불러오는 시점에 기본 registerer에 등록한다.
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
