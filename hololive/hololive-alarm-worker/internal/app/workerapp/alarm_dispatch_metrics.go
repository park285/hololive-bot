package workerapp

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	alarmDispatchRunnerMetricsOnce sync.Once

	alarmDispatchRunnerEmptyPollsTotal          *prometheus.CounterVec
	alarmDispatchRunnerIdleWaitSeconds          *prometheus.HistogramVec
	alarmDispatchRunnerWakeupConsumedTotal      prometheus.Counter
	alarmDispatchRunnerWakeupTimeoutTotal       prometheus.Counter
	alarmDispatchRunnerWakeupErrorTotal         prometheus.Counter
	alarmDispatchRunnerPostSendQuarantinedTotal prometheus.Counter
	alarmDispatchRetryAfterClampedTotal         prometheus.Counter
	alarmDispatchPGRetentionDeletedRowsTotal    *prometheus.CounterVec
	alarmDispatchPGRetentionFailedTotal         prometheus.Counter
	alarmDispatchPGBacklogRows                  *prometheus.GaugeVec
	alarmDispatchPGOldestPendingAgeSeconds      prometheus.Gauge
	alarmDispatchPGOldestRetryAgeSeconds        prometheus.Gauge
	alarmDispatchPGOldestSendingAgeSeconds      prometheus.Gauge
)

func initAlarmDispatchRunnerMetrics() {
	alarmDispatchRunnerMetricsOnce.Do(func() {
		initAlarmDispatchRunnerLoopMetrics()
		initAlarmDispatchRetentionMetrics()
		alarmDispatchPGBacklogRows = promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "alarm_dispatch_pg_backlog_rows",
				Help: "Alarm dispatch PG active backlog rows by status.",
			},
			[]string{"status"},
		)
		alarmDispatchPGOldestPendingAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_oldest_pending_age_seconds",
			Help: "Oldest due pending alarm dispatch row age in seconds.",
		})
		alarmDispatchPGOldestRetryAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_oldest_retry_age_seconds",
			Help: "Oldest due retry alarm dispatch row age in seconds.",
		})
		alarmDispatchPGOldestSendingAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_oldest_sending_age_seconds",
			Help: "Oldest sending alarm dispatch row age in seconds.",
		})
	})
}

func initAlarmDispatchRunnerLoopMetrics() {
	alarmDispatchRunnerEmptyPollsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_runner_empty_polls_total",
			Help: "Alarm dispatch runner empty batch observations.",
		},
		[]string{"consumer_mode"},
	)
	alarmDispatchRunnerIdleWaitSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "alarm_dispatch_runner_idle_wait_seconds",
			Help:    "Alarm dispatch runner idle wait duration by consumer mode and wait mode.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"consumer_mode", "wait_mode"},
	)
	alarmDispatchRunnerWakeupConsumedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_runner_wakeup_consumed_total",
		Help: "Alarm dispatch runner wakeup tokens consumed.",
	})
	alarmDispatchRunnerWakeupTimeoutTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_runner_wakeup_timeout_total",
		Help: "Alarm dispatch runner wakeup waits that timed out.",
	})
	alarmDispatchRunnerWakeupErrorTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_runner_wakeup_error_total",
		Help: "Alarm dispatch runner wakeup waits that failed.",
	})
	alarmDispatchRunnerPostSendQuarantinedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_runner_post_send_quarantined_total",
		Help: "Alarm dispatch rows quarantined after ambiguous post-send failures.",
	})
	alarmDispatchRetryAfterClampedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_retry_after_clamped_total",
		Help: "Alarm dispatch HTTP Retry-After hints clamped to the maximum bound.",
	})
}

func initAlarmDispatchRetentionMetrics() {
	alarmDispatchPGRetentionDeletedRowsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_retention_deleted_rows_total",
			Help: "Alarm dispatch PG rows deleted by retention.",
		},
		[]string{"status"},
	)
	alarmDispatchPGRetentionFailedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "alarm_dispatch_pg_retention_failed_total",
		Help: "Alarm dispatch PG retention failures.",
	})
}

func observeAlarmDispatchRunnerEmptyPoll(consumerMode string) {
	initAlarmDispatchRunnerMetrics()
	alarmDispatchRunnerEmptyPollsTotal.WithLabelValues(consumerMode).Inc()
}

func observeAlarmDispatchRunnerPostSendQuarantined(rows int) {
	if rows <= 0 {
		return
	}
	initAlarmDispatchRunnerMetrics()
	if alarmDispatchRunnerPostSendQuarantinedTotal == nil {
		return
	}
	alarmDispatchRunnerPostSendQuarantinedTotal.Add(float64(rows))
}

func observeAlarmDispatchRetryAfterClamped() {
	initAlarmDispatchRunnerMetrics()
	if alarmDispatchRetryAfterClampedTotal == nil {
		return
	}
	alarmDispatchRetryAfterClampedTotal.Inc()
}

func observeAlarmDispatchRunnerIdleWait(consumerMode, waitMode string, duration time.Duration) {
	initAlarmDispatchRunnerMetrics()
	alarmDispatchRunnerIdleWaitSeconds.WithLabelValues(consumerMode, waitMode).Observe(duration.Seconds())
}

func observeAlarmDispatchRunnerWakeupConsumed() {
	initAlarmDispatchRunnerMetrics()
	if alarmDispatchRunnerWakeupConsumedTotal == nil {
		return
	}
	alarmDispatchRunnerWakeupConsumedTotal.Inc()
}

func observeAlarmDispatchRunnerWakeupTimeout() {
	initAlarmDispatchRunnerMetrics()
	if alarmDispatchRunnerWakeupTimeoutTotal == nil {
		return
	}
	alarmDispatchRunnerWakeupTimeoutTotal.Inc()
}

func observeAlarmDispatchRunnerWakeupError() {
	initAlarmDispatchRunnerMetrics()
	if alarmDispatchRunnerWakeupErrorTotal == nil {
		return
	}
	alarmDispatchRunnerWakeupErrorTotal.Inc()
}

func observeAlarmDispatchRetentionDeletedRows(status string, rows int64) {
	if rows <= 0 {
		return
	}
	initAlarmDispatchRunnerMetrics()
	alarmDispatchPGRetentionDeletedRowsTotal.WithLabelValues(status).Add(float64(rows))
}

func observeAlarmDispatchRetentionFailure() {
	initAlarmDispatchRunnerMetrics()
	if alarmDispatchPGRetentionFailedTotal == nil {
		return
	}
	alarmDispatchPGRetentionFailedTotal.Inc()
}

func observeAlarmDispatchBacklogStatus(status string, rows int64) {
	initAlarmDispatchRunnerMetrics()
	alarmDispatchPGBacklogRows.WithLabelValues(status).Set(float64(rows))
}

func observeAlarmDispatchOldestAges(pending, retry, sending float64) {
	initAlarmDispatchRunnerMetrics()
	if alarmDispatchPGOldestPendingAgeSeconds == nil ||
		alarmDispatchPGOldestRetryAgeSeconds == nil ||
		alarmDispatchPGOldestSendingAgeSeconds == nil {
		return
	}
	alarmDispatchPGOldestPendingAgeSeconds.Set(pending)
	alarmDispatchPGOldestRetryAgeSeconds.Set(retry)
	alarmDispatchPGOldestSendingAgeSeconds.Set(sending)
}
