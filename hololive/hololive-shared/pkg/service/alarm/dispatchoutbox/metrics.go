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
