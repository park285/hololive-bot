package alarmdispatch

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type alarmDispatchRunnerMetricSet struct {
	emptyPollsTotal               prometheus.Counter
	idleWaitSeconds               *prometheus.HistogramVec
	wakeupConsumedTotal           prometheus.Counter
	wakeupTimeoutTotal            prometheus.Counter
	wakeupErrorTotal              prometheus.Counter
	postSendQuarantinedTotal      prometheus.Counter
	retryAfterClampedTotal        prometheus.Counter
	retentionDeletedRowsTotal     *prometheus.CounterVec
	retentionFailedTotal          prometheus.Counter
	backlogObservationFailedTotal prometheus.Counter
	backlogRows                   *prometheus.GaugeVec
	oldestPendingAgeSeconds       prometheus.Gauge
	oldestRetryAgeSeconds         prometheus.Gauge
	oldestSendingAgeSeconds       prometheus.Gauge
	quarantinedRows               prometheus.Gauge
	oldestQuarantinedAgeSeconds   prometheus.Gauge
	unreviewedQuarantinedRows     prometheus.Gauge
	oldestUnreviewedAgeSeconds    prometheus.Gauge
	backlogSnapshotSuccess        prometheus.Gauge
	backlogSnapshotLastSuccess    prometheus.Gauge
}

// alarmDispatchRunnerMetrics는 runner·retention·backlog 지표를 첫 관측 때 기본 registerer에 한꺼번에 등록한다.
var alarmDispatchRunnerMetrics = sync.OnceValue(func() *alarmDispatchRunnerMetricSet {
	return &alarmDispatchRunnerMetricSet{
		emptyPollsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_runner_empty_polls_total",
			Help: "Alarm dispatch runner empty batch observations.",
		}),
		idleWaitSeconds: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "alarm_dispatch_runner_idle_wait_seconds",
				Help:    "Alarm dispatch runner idle wait duration by wait mode.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"wait_mode"},
		),
		wakeupConsumedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_runner_wakeup_consumed_total",
			Help: "Alarm dispatch runner wakeup tokens consumed.",
		}),
		wakeupTimeoutTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_runner_wakeup_timeout_total",
			Help: "Alarm dispatch runner wakeup waits that timed out.",
		}),
		wakeupErrorTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_runner_wakeup_error_total",
			Help: "Alarm dispatch runner wakeup waits that failed.",
		}),
		postSendQuarantinedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_runner_post_send_quarantined_total",
			Help: "Alarm dispatch rows quarantined after ambiguous post-send failures.",
		}),
		retryAfterClampedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_retry_after_clamped_total",
			Help: "Alarm dispatch HTTP Retry-After hints clamped to the maximum bound.",
		}),
		retentionDeletedRowsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "alarm_dispatch_pg_retention_deleted_rows_total",
				Help: "Alarm dispatch PG rows deleted by retention.",
			},
			[]string{"status"},
		),
		retentionFailedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_retention_failed_total",
			Help: "Alarm dispatch PG retention failures.",
		}),
		backlogObservationFailedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_backlog_observation_failed_total",
			Help: "Alarm dispatch PG backlog observation failures.",
		}),
		quarantinedRows: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_quarantined_rows",
			Help: "Retained quarantined alarm dispatch rows, including rows already closed by a matching closeout receipt; retention is unchanged.",
		}),
		oldestQuarantinedAgeSeconds: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_oldest_quarantined_age_seconds",
			Help: "Age of the oldest retained quarantined alarm dispatch row.",
		}),
		unreviewedQuarantinedRows: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_unreviewed_quarantined_rows",
			Help: "Retained quarantined alarm dispatch rows requiring disposition review: no closeout receipt matches the row's current revision and status metadata.",
		}),
		oldestUnreviewedAgeSeconds: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_oldest_unreviewed_quarantined_age_seconds",
			Help: "Age of the oldest retained quarantined alarm dispatch row requiring disposition review.",
		}),
		backlogSnapshotSuccess: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_backlog_snapshot_success",
			Help: "Whether the latest alarm dispatch backlog observation succeeded.",
		}),
		backlogSnapshotLastSuccess: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_backlog_snapshot_last_success_timestamp_seconds",
			Help: "Unix time of the last successful alarm dispatch backlog observation.",
		}),
		backlogRows: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "alarm_dispatch_pg_backlog_rows",
				Help: "Alarm dispatch PG active backlog rows by status.",
			},
			[]string{"status"},
		),
		oldestPendingAgeSeconds: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_oldest_pending_age_seconds",
			Help: "Oldest due pending alarm dispatch row age in seconds.",
		}),
		oldestRetryAgeSeconds: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_oldest_retry_age_seconds",
			Help: "Oldest due retry alarm dispatch row age in seconds.",
		}),
		oldestSendingAgeSeconds: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "alarm_dispatch_pg_oldest_sending_age_seconds",
			Help: "Oldest sending alarm dispatch row age in seconds.",
		}),
	}
})

func observeAlarmDispatchRunnerEmptyPoll() {
	alarmDispatchRunnerMetrics().emptyPollsTotal.Inc()
}

func observeAlarmDispatchRunnerPostSendQuarantined(rows int) {
	if rows <= 0 {
		return
	}

	alarmDispatchRunnerMetrics().postSendQuarantinedTotal.Add(float64(rows))
}

func observeAlarmDispatchRetryAfterClamped() {
	alarmDispatchRunnerMetrics().retryAfterClampedTotal.Inc()
}

func observeAlarmDispatchRunnerIdleWait(waitMode string, duration time.Duration) {
	alarmDispatchRunnerMetrics().idleWaitSeconds.WithLabelValues(waitMode).Observe(duration.Seconds())
}

func observeAlarmDispatchRunnerWakeupConsumed() {
	alarmDispatchRunnerMetrics().wakeupConsumedTotal.Inc()
}

func observeAlarmDispatchRunnerWakeupTimeout() {
	alarmDispatchRunnerMetrics().wakeupTimeoutTotal.Inc()
}

func observeAlarmDispatchRunnerWakeupError() {
	alarmDispatchRunnerMetrics().wakeupErrorTotal.Inc()
}

func observeAlarmDispatchRetentionDeletedRows(status string, rows int64) {
	if rows <= 0 {
		return
	}

	alarmDispatchRunnerMetrics().retentionDeletedRowsTotal.WithLabelValues(status).Add(float64(rows))
}

func observeAlarmDispatchRetentionFailure() {
	alarmDispatchRunnerMetrics().retentionFailedTotal.Inc()
}

func observeAlarmDispatchBacklogObservationFailure() {
	alarmDispatchRunnerMetrics().backlogObservationFailedTotal.Inc()
}

func observeAlarmDispatchBacklogStatus(status string, rows int64) {
	alarmDispatchRunnerMetrics().backlogRows.WithLabelValues(status).Set(float64(rows))
}

func observeAlarmDispatchQuarantine(snapshot *alarmDispatchBacklogSnapshot) {
	metrics := alarmDispatchRunnerMetrics()
	metrics.quarantinedRows.Set(float64(snapshot.QuarantinedRows))
	metrics.oldestQuarantinedAgeSeconds.Set(snapshot.OldestQuarantinedAgeSeconds)
	metrics.unreviewedQuarantinedRows.Set(float64(snapshot.UnreviewedQuarantinedRows))
	metrics.oldestUnreviewedAgeSeconds.Set(snapshot.OldestUnreviewedQuarantinedAgeSeconds)
}

func observeAlarmDispatchBacklogSnapshotSucceeded(observedAt time.Time) {
	metrics := alarmDispatchRunnerMetrics()
	metrics.backlogSnapshotLastSuccess.Set(float64(observedAt.Unix()))
	metrics.backlogSnapshotSuccess.Set(1)
}

func observeAlarmDispatchBacklogSnapshotFailed() {
	alarmDispatchRunnerMetrics().backlogSnapshotSuccess.Set(0)
}

func observeAlarmDispatchOldestAges(pending, retry, sending float64) {
	metrics := alarmDispatchRunnerMetrics()
	metrics.oldestPendingAgeSeconds.Set(pending)
	metrics.oldestRetryAgeSeconds.Set(retry)
	metrics.oldestSendingAgeSeconds.Set(sending)
}

// members에 한국어 표시명이 없어 misc/vtuber_fallback 문구로 방송 알림을 만든 횟수다.
var alarmDispatchMemberNameMissingTotal = sync.OnceValue(func() prometheus.Counter {
	return promauto.NewCounter(prometheus.CounterOpts{
		Name: "hololive_alarm_dispatch_member_name_missing_total",
		Help: "Alarm dispatch notifications rendered with misc/vtuber_fallback because the member has no Korean display name.",
	})
})
