package collectorruntime

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	resultSuccess       = "success"
	resultTimeout       = "timeout"
	resultCanceled      = "canceled"
	resultParserDrift   = "parser_drift"
	resultPaginationGap = "pagination_gap"
	resultFailed        = "failed"
	resultNotAcquired   = "not_acquired"
	resultError         = "error"
	resultAcquired      = "acquired"
	phaseRenew          = "renew"
	phasePublish        = "publish"
	phaseCollect        = "collect"
	outcomeInserted     = "inserted"
	outcomeDuplicate    = "duplicate"
	outcomeCollision    = "collision"
	outcomeRejected     = "rejected"
	outcomeEmpty        = "empty"
)

type Metrics struct {
	attempts     *prometheus.CounterVec
	duration     *prometheus.HistogramVec
	lastSuccess  *prometheus.GaugeVec
	freshness    *prometheus.GaugeVec
	completeness *prometheus.CounterVec
	leaseAcquire *prometheus.CounterVec
	leaseLost    *prometheus.CounterVec
	publish      *prometheus.CounterVec

	mu            sync.Mutex
	lastSuccessAt map[string]time.Time
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	metrics := &Metrics{lastSuccessAt: make(map[string]time.Time)}
	metrics.attempts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_attempts_total",
		Help: "YouTube collection attempts by provider, kind, and bounded result.",
	}, []string{"provider", "kind", "result"})
	metrics.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_collection_duration_seconds",
		Help:    "YouTube collection duration by provider and kind.",
		Buckets: prometheus.DefBuckets,
	}, []string{"provider", "kind"})
	metrics.lastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "youtube_collection_last_success_timestamp_seconds",
		Help: "Unix timestamp of the last successful YouTube collection.",
	}, []string{"provider", "kind"})
	metrics.freshness = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "youtube_collection_freshness_seconds",
		Help: "Age of the last successful YouTube collection.",
	}, []string{"provider", "kind"})
	metrics.completeness = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_completeness_total",
		Help: "YouTube collection completeness and continuity outcomes.",
	}, []string{"provider", "kind", "completeness", "continuity"})
	metrics.leaseAcquire = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_lease_acquire_total",
		Help: "YouTube collection lease acquire attempts.",
	}, []string{"provider", "kind", "result"})
	metrics.leaseLost = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_lease_lost_total",
		Help: "YouTube collection lease losses by phase.",
	}, []string{"provider", "kind", "phase"})
	metrics.publish = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_observation_publish_total",
		Help: "YouTube observation publish outcomes.",
	}, []string{"provider", "kind", "outcome"})
	registerer.MustRegister(
		metrics.attempts, metrics.duration, metrics.lastSuccess, metrics.freshness,
		metrics.completeness, metrics.leaseAcquire, metrics.leaseLost, metrics.publish,
	)
	return metrics
}

func (m *Metrics) ObserveAttempt(provider contract.Provider, kind, result string, duration time.Duration) {
	if m == nil {
		return
	}
	m.attempts.WithLabelValues(string(provider), kind, boundedResult(result)).Inc()
	m.duration.WithLabelValues(string(provider), kind).Observe(duration.Seconds())
}

func (m *Metrics) ObserveSuccess(provider contract.Provider, kind string, now time.Time) {
	if m == nil {
		return
	}
	m.lastSuccess.WithLabelValues(string(provider), kind).Set(float64(now.Unix()))
	m.freshness.WithLabelValues(string(provider), kind).Set(0)
	m.mu.Lock()
	m.lastSuccessAt[string(provider)+"/"+kind] = now
	m.mu.Unlock()
}

func (m *Metrics) ObserveFreshness(provider contract.Provider, kind string, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	last, ok := m.lastSuccessAt[string(provider)+"/"+kind]
	m.mu.Unlock()
	if !ok {
		return
	}
	m.freshness.WithLabelValues(string(provider), kind).Set(now.Sub(last).Seconds())
}

func (m *Metrics) ObserveCompleteness(provider contract.Provider, kind string, completeness contract.Completeness, continuity contract.Continuity) {
	if m == nil {
		return
	}
	m.completeness.WithLabelValues(string(provider), kind, string(completeness), string(continuity)).Inc()
}

func (m *Metrics) ObserveAcquire(provider contract.Provider, kind, result string) {
	if m == nil {
		return
	}
	m.leaseAcquire.WithLabelValues(string(provider), kind, boundedAcquire(result)).Inc()
}

func (m *Metrics) ObserveLeaseLost(provider contract.Provider, kind, phase string) {
	if m == nil {
		return
	}
	m.leaseLost.WithLabelValues(string(provider), kind, boundedPhase(phase)).Inc()
}

func (m *Metrics) ObservePublish(provider contract.Provider, kind, outcome string) {
	if m == nil {
		return
	}
	m.publish.WithLabelValues(string(provider), kind, boundedOutcome(outcome)).Inc()
}

func boundedResult(value string) string {
	switch value {
	case resultSuccess, resultTimeout, resultCanceled, resultParserDrift, resultPaginationGap, resultFailed:
		return value
	default:
		return resultFailed
	}
}

func boundedAcquire(value string) string {
	switch value {
	case resultAcquired, resultNotAcquired, resultError:
		return value
	default:
		return resultError
	}
}

func boundedPhase(value string) string {
	switch value {
	case phaseRenew, phasePublish, phaseCollect:
		return value
	default:
		return phaseCollect
	}
}

func boundedOutcome(value string) string {
	switch value {
	case outcomeInserted, outcomeDuplicate, outcomeCollision, outcomeRejected, outcomeEmpty:
		return value
	default:
		return outcomeRejected
	}
}
