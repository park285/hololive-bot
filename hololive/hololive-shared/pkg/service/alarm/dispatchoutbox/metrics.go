package dispatchoutbox

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	recoveryTypeLeased  = "leased"
	recoveryTypeSending = "sending"
)

var (
	alarmDispatchRecoveryLastSuccessTimestamp prometheus.Gauge
	alarmDispatchRecoveryFailedTotal          *prometheus.CounterVec
	alarmDispatchRecoveryRowsTotal            *prometheus.CounterVec
	alarmDispatchPGClaimedTotal               prometheus.Counter
	alarmDispatchPGMarkSendingFailedTotal     prometheus.Counter
	alarmDispatchPGMarkSentFailedTotal        prometheus.Counter
	alarmDispatchPGQuarantinedTotal           prometheus.Counter
	alarmDispatchPGDLQTotal                   prometheus.Counter
	alarmDispatchPGRetryScheduledTotal        prometheus.Counter
	alarmDispatchPGTransitionPartialTotal     prometheus.Counter
)

func init() {
	alarmDispatchRecoveryLastSuccessTimestamp = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "alarm_dispatch_recovery_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful PG dispatch recovery pass.",
		},
	)
	alarmDispatchRecoveryFailedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_recovery_failed_total",
			Help: "Total failed PG dispatch recovery attempts by recovery type.",
		},
		[]string{"type"},
	)
	alarmDispatchRecoveryRowsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_recovery_rows_total",
			Help: "Total PG dispatch rows touched by recovery by recovery type.",
		},
		[]string{"type"},
	)
	alarmDispatchPGClaimedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_claimed_total",
			Help: "Total PG dispatch rows claimed for delivery.",
		},
	)
	alarmDispatchPGMarkSendingFailedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_mark_sending_failed_total",
			Help: "Total PG dispatch mark-sending operations that failed.",
		},
	)
	alarmDispatchPGMarkSentFailedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_mark_sent_failed_total",
			Help: "Total PG dispatch mark-sent operations that failed.",
		},
	)
	alarmDispatchPGQuarantinedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_quarantined_total",
			Help: "Total PG dispatch rows moved to quarantine.",
		},
	)
	alarmDispatchPGDLQTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_dlq_total",
			Help: "Total PG dispatch rows moved to DLQ.",
		},
	)
	alarmDispatchPGRetryScheduledTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_retry_scheduled_total",
			Help: "Total PG dispatch rows scheduled for retry.",
		},
	)
	alarmDispatchPGTransitionPartialTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_transition_partial_total",
			Help: "Total PG dispatch mark-sending/mark-sent operations where RowsAffected < expected (concurrent worker overlap or quarantine preemption).",
		},
	)
}

func observeRecoveryRows(recoveryType string, rows int) {
	if rows > 0 {
		alarmDispatchRecoveryRowsTotal.WithLabelValues(recoveryType).Add(float64(rows))
	}
}

func observeRecoveryFailure(recoveryType string) {
	alarmDispatchRecoveryFailedTotal.WithLabelValues(recoveryType).Inc()
}

func observeRecoverySuccess(at time.Time) {
	alarmDispatchRecoveryLastSuccessTimestamp.Set(float64(at.Unix()))
}

func observePGClaimed(rows int) {
	if rows > 0 {
		alarmDispatchPGClaimedTotal.Add(float64(rows))
	}
}

func observePGMarkSendingFailure() {
	alarmDispatchPGMarkSendingFailedTotal.Inc()
}

func observePGMarkSentFailure() {
	alarmDispatchPGMarkSentFailedTotal.Inc()
}

func observePGQuarantined(rows int) {
	if rows > 0 {
		alarmDispatchPGQuarantinedTotal.Add(float64(rows))
	}
}

func observePGDLQ(rows int) {
	if rows > 0 {
		alarmDispatchPGDLQTotal.Add(float64(rows))
	}
}

func observePGRetryScheduled(rows int) {
	if rows > 0 {
		alarmDispatchPGRetryScheduledTotal.Add(float64(rows))
	}
}

func observePGTransitionPartial() {
	alarmDispatchPGTransitionPartialTotal.Inc()
}
