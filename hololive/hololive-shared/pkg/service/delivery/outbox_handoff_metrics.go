package delivery

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
)

var (
	dispatchHandoffMetricsOnce sync.Once
	dispatchHandoffTotal       *prometheus.CounterVec
)

func observeDispatchHandoff(mode handoff.Mode, result string, rows int) {
	if rows <= 0 {
		return
	}

	dispatchHandoffMetricsOnce.Do(func() {
		dispatchHandoffTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_delivery_outbox_v3_handoff_total",
				Help: "Notification delivery outbox rows handed to the v3 ledger by mode and result.",
			},
			[]string{"mode", "result"},
		)
	})
	dispatchHandoffTotal.WithLabelValues(string(mode), result).Add(float64(rows))
}
