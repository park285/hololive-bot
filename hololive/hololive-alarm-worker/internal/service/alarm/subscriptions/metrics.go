package subscriptions

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type alarmMetricSet struct {
	serviceOperationDuration *prometheus.HistogramVec
	cacheRebuildTotal        *prometheus.CounterVec
	cacheRebuildDuration     *prometheus.HistogramVec
	cacheRebuildLoaded       *prometheus.GaugeVec
	mutationLockWait         *prometheus.HistogramVec
}

var alarmMetrics = sync.OnceValue(newAlarmMetricSet)

const metricLabelOperation = "operation"

func newAlarmMetricSet() *alarmMetricSet {
	return &alarmMetricSet{
		serviceOperationDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_service_operation_duration_seconds",
				Help:    "Alarm service operation duration in seconds by operation and result.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{metricLabelOperation, "result"},
		),
		cacheRebuildTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_cache_rebuild_total",
				Help: "Alarm cache rebuild attempts by operation and result.",
			},
			[]string{metricLabelOperation, "result"},
		),
		cacheRebuildDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_cache_rebuild_duration_seconds",
				Help:    "Alarm cache rebuild duration in seconds by operation and result.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{metricLabelOperation, "result"},
		),
		cacheRebuildLoaded: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "hololive_alarm_cache_rebuild_loaded",
				Help: "Last successful alarm cache rebuild loaded counts by operation and resource.",
			},
			[]string{metricLabelOperation, "resource"},
		),
		mutationLockWait: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_service_mutation_lock_wait_seconds",
				Help:    "Time alarm subscription mutations waited for the cache mutation lock, by operation.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{metricLabelOperation},
		),
	}
}

func observeAlarmServiceOperation(operation string, startedAt time.Time, err error) {
	alarmMetrics().serviceOperationDuration.WithLabelValues(operation, alarmOperationResult(err)).Observe(time.Since(startedAt).Seconds())
}

func alarmOperationResult(err error) string {
	if err != nil {
		return "error"
	}

	return "ok"
}

func observeAlarmCacheRebuild(operation string, err error) {
	alarmMetrics().cacheRebuildTotal.WithLabelValues(operation, alarmOperationResult(err)).Inc()
}

func observeAlarmCacheRebuildDuration(operation string, startedAt time.Time, err error) {
	alarmMetrics().cacheRebuildDuration.WithLabelValues(operation, alarmOperationResult(err)).Observe(time.Since(startedAt).Seconds())
}

func observeAlarmCacheRebuildLoaded(operation string, alarmsLoaded, roomsLoaded, channelsLoaded int) {
	loaded := alarmMetrics().cacheRebuildLoaded
	loaded.WithLabelValues(operation, "alarms").Set(float64(alarmsLoaded))
	loaded.WithLabelValues(operation, "rooms").Set(float64(roomsLoaded))
	loaded.WithLabelValues(operation, "channels").Set(float64(channelsLoaded))
}

func observeAlarmMutationLockWait(operation string, startedAt time.Time) {
	alarmMetrics().mutationLockWait.WithLabelValues(operation).Observe(time.Since(startedAt).Seconds())
}
