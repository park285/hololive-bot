package youtubejs

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type rpcMetrics struct {
	duration *prometheus.HistogramVec
	inFlight *prometheus.GaugeVec
}

// EnableMetrics는 요청을 시작하기 전에 호출 제한 대기와 helper 수행의 관측을 등록합니다.
// 호출 간격 interval은 프로세스 전체가 공유하며 외부 HTTP 요청 수의 상한은 아닙니다.
func (c *RPC) EnableMetrics(registerer prometheus.Registerer, interval time.Duration) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	m := &rpcMetrics{
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "youtubejs_rpc_phase_duration_seconds",
			Help:    "YouTube.js RPC limiter wait or helper execution including response decoding; excludes local scheduler queue time.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15, 30, 60, 120, 300},
		}, []string{"operation", "phase", "outcome"}),
		inFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "youtubejs_rpc_phase_in_flight",
			Help: "Current RPC calls waiting for the limiter or executing in the helper; not CPU utilization.",
		}, []string{"operation", "phase"}),
	}
	configuredInterval := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "youtubejs_rpc_request_interval_seconds",
		Help: "Configured shared process-wide interval between YouTube.js helper RPC admissions.",
	})
	configuredInterval.Set(interval.Seconds())
	registerer.MustRegister(m.duration, m.inFlight, configuredInterval)

	c.metrics = m
}

func (m *rpcMetrics) begin(path, phase string) {
	if m != nil {
		m.inFlight.WithLabelValues(rpcOperation(path), phase).Inc()
	}
}

func (m *rpcMetrics) end(path, phase string, started time.Time, err error) {
	if m == nil {
		return
	}

	operation := rpcOperation(path)
	m.inFlight.WithLabelValues(operation, phase).Dec()
	m.duration.WithLabelValues(operation, phase, rpcOutcome(err)).Observe(time.Since(started).Seconds())
}

func rpcOperation(path string) string {
	switch path {
	case "/v1/community":
		return "community"
	case "/v1/content":
		return "content"
	case "/v1/channel":
		return "channel"
	case "/v1/viewer":
		return "viewer"
	default:
		return "unknown"
	}
}

func rpcOutcome(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "error"
	}
}
