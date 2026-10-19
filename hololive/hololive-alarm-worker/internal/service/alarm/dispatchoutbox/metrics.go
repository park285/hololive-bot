package dispatchoutbox

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
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
	alarmDispatchPGTransitionPartialTotal     *prometheus.CounterVec
	alarmDispatchPGClaimReleasedTotal         prometheus.Counter
)

func init() {
	registerRecoveryMetrics()
	registerPGDispatchMetrics()
}

func registerRecoveryMetrics() {
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
}

func registerPGDispatchMetrics() {
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
	alarmDispatchPGTransitionPartialTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_transition_partial_total",
			Help: "Total PG dispatch transitions that left at least one target unchanged by a send fence miss, by action and observed miss reason.",
		},
		[]string{"action", "reason"},
	)
	alarmDispatchPGClaimReleasedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "alarm_dispatch_pg_claim_released_total",
			Help: "Total dedup claim keys deleted after PG dispatch terminal (DLQ/quarantine/drop) cleanup.",
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
	if alarmDispatchRecoveryLastSuccessTimestamp == nil {
		return
	}

	alarmDispatchRecoveryLastSuccessTimestamp.Set(float64(at.Unix()))
}

func observePGClaimed(rows int) {
	if alarmDispatchPGClaimedTotal == nil || rows <= 0 {
		return
	}

	alarmDispatchPGClaimedTotal.Add(float64(rows))
}

func observePGMarkSendingFailure() {
	if alarmDispatchPGMarkSendingFailedTotal == nil {
		return
	}

	alarmDispatchPGMarkSendingFailedTotal.Inc()
}

func observePGMarkSentFailure() {
	if alarmDispatchPGMarkSentFailedTotal == nil {
		return
	}

	alarmDispatchPGMarkSentFailedTotal.Inc()
}

func observePGQuarantined(rows int) {
	if alarmDispatchPGQuarantinedTotal == nil || rows <= 0 {
		return
	}

	alarmDispatchPGQuarantinedTotal.Add(float64(rows))
}

func observePGDLQ(rows int) {
	if alarmDispatchPGDLQTotal == nil || rows <= 0 {
		return
	}

	alarmDispatchPGDLQTotal.Add(float64(rows))
}

func observePGRetryScheduled(rows int) {
	if alarmDispatchPGRetryScheduledTotal == nil || rows <= 0 {
		return
	}

	alarmDispatchPGRetryScheduledTotal.Add(float64(rows))
}

// observePGFenceMiss는 한 전이 문장의 미스를 원인마다 한 번 센다.
func observePGFenceMiss(miss *sendfence.MissError) {
	if alarmDispatchPGTransitionPartialTotal == nil {
		return
	}

	seen := make(map[sendfence.Reason]struct{}, len(miss.Rows))
	for _, row := range miss.Rows {
		if _, ok := seen[row.Reason]; ok {
			continue
		}

		seen[row.Reason] = struct{}{}
		alarmDispatchPGTransitionPartialTotal.WithLabelValues(miss.Action, string(row.Reason)).Inc()
	}
}

func observePGClaimReleased(keys int) {
	if alarmDispatchPGClaimReleasedTotal == nil || keys <= 0 {
		return
	}

	alarmDispatchPGClaimReleasedTotal.Add(float64(keys))
}
