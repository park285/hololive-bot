package notification

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	alarmMetricsInitOnce sync.Once

	alarmServiceOperationDuration *prometheus.HistogramVec
)

func initAlarmMetrics() {
	alarmMetricsInitOnce.Do(func() {
		alarmServiceOperationDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_service_operation_duration_seconds",
				Help:    "Alarm service operation duration in seconds by operation and result.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation", "result"},
		)
	})
}

func observeAlarmServiceOperation(operation string, startedAt time.Time, err error) {
	initAlarmMetrics()
	alarmServiceOperationDuration.WithLabelValues(operation, alarmOperationResult(err)).Observe(time.Since(startedAt).Seconds())
}

func alarmOperationResult(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}
