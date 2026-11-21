package collectorruntime

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	resultSuccess     = "success"
	resultTimeout     = "timeout"
	resultCanceled    = "canceled"
	resultParserDrift = "parser_drift"
	resultFailed      = "failed"
	resultSuperseded  = "superseded"
	resultNotAcquired = "not_acquired"
	resultError       = "error"
	resultAcquired    = "acquired"
	phaseRenew        = "renew"
	phasePublish      = "publish"
	phaseCollect      = "collect"
	outcomeInserted   = "inserted"
	outcomeDuplicate  = "duplicate"
	outcomeCollision  = "collision"
	outcomeRejected   = "rejected"
	outcomeSuperseded = "superseded"
	outcomeEmpty      = "empty"
	labelProvider     = "provider"
	labelKind         = "kind"
	labelResult       = "result"

	discoveryCycleOK                  = "ok"
	discoveryCycleCandidateLoadFailed = "candidate_load_failed"
)

type Metrics struct {
	attempts       *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	lastSuccess    *prometheus.GaugeVec
	freshness      *prometheus.GaugeVec
	completeness   *prometheus.CounterVec
	leaseAcquire   *prometheus.CounterVec
	leaseLost      *prometheus.CounterVec
	publish        *prometheus.CounterVec
	publishTime    *prometheus.HistogramVec
	publishBytes   *prometheus.HistogramVec
	acceptInterval *prometheus.HistogramVec
	lastAccepted   *prometheus.GaugeVec
	enqueue        *prometheus.CounterVec
	invalidTuple   *prometheus.CounterVec
	discoveryCycle *prometheus.CounterVec

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
	}, []string{labelProvider, labelKind, labelResult})
	metrics.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_collection_duration_seconds",
		Help:    "YouTube collection duration by provider and kind.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15, 30, 60, 120, 300},
	}, []string{labelProvider, labelKind})
	metrics.lastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "youtube_collection_last_success_timestamp_seconds",
		Help: "Unix timestamp of the last successful YouTube collection.",
	}, []string{labelProvider, labelKind})
	metrics.freshness = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "youtube_collection_freshness_seconds",
		Help: "Age of the last successful YouTube collection.",
	}, []string{labelProvider, labelKind})
	metrics.completeness = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_completeness_total",
		Help: "YouTube collection completeness and continuity outcomes.",
	}, []string{labelProvider, labelKind, "completeness", "continuity"})
	metrics.leaseAcquire = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_lease_acquire_total",
		Help: "YouTube collection lease acquire attempts.",
	}, []string{labelProvider, labelKind, labelResult})
	metrics.leaseLost = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_lease_lost_total",
		Help: "YouTube collection lease losses by phase.",
	}, []string{labelProvider, labelKind, "phase"})
	metrics.publish = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_observation_publish_total",
		Help: "YouTube observation publish outcomes.",
	}, []string{labelProvider, labelKind, "outcome"})
	metrics.publishTime, metrics.publishBytes = newPublishHistograms()
	metrics.acceptInterval = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_observation_accept_interval_seconds",
		Help:    "Interval between consecutive durable acceptances of the same observation checkpoint.",
		Buckets: []float64{30, 60, 120, 300, 600, 900, 1200, 1800, 2700, 3600, 7200, 21600, 86400},
	}, []string{labelProvider, labelKind})
	metrics.lastAccepted = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "youtube_observation_last_accepted_timestamp_seconds",
		Help: "Unix timestamp of the last durably committed inserted or duplicate observation by kind.",
	}, []string{labelProvider, labelKind})
	metrics.enqueue = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_enqueue_total",
		Help: "YouTube collection local queue enqueue results.",
	}, []string{labelResult})
	metrics.invalidTuple = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_invalid_failure_tuple_total",
		Help: "YouTube collection attempts that ended with a code/class tuple outside the durable failure contract.",
	}, []string{labelProvider, labelKind})
	metrics.discoveryCycle = newDiscoveryCycleCounter()
	registerer.MustRegister(
		metrics.attempts, metrics.duration, metrics.lastSuccess, metrics.freshness,
		metrics.completeness, metrics.leaseAcquire, metrics.leaseLost, metrics.publish,
		metrics.publishTime, metrics.publishBytes, metrics.acceptInterval, metrics.lastAccepted, metrics.enqueue, metrics.invalidTuple,
		metrics.discoveryCycle,
	)

	return metrics
}

func newDiscoveryCycleCounter() *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_discovery_cycles_total",
		Help: "YouTube collection candidate discovery cycles by bounded result.",
	}, []string{labelResult})
	counter.WithLabelValues(discoveryCycleOK)
	counter.WithLabelValues(discoveryCycleCandidateLoadFailed)

	return counter
}

func newPublishHistograms() (duration, encodedBytes *prometheus.HistogramVec) {
	duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_observation_publish_duration_seconds",
		Help:    "YouTube observation publish transaction duration by provider and job kind.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{labelProvider, labelKind})
	encodedBytes = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_observation_publish_encoded_bytes",
		Help:    "Encoded observation JSON bytes of committed YouTube observation publishes by provider and job kind.",
		Buckets: []float64{1 << 10, 4 << 10, 16 << 10, 64 << 10, 256 << 10, 1 << 20, 2 << 20, 4 << 20, 6 << 20, 7 << 20, 8 << 20},
	}, []string{labelProvider, labelKind})

	return duration, encodedBytes
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

func (m *Metrics) ObservePublishDuration(provider contract.Provider, kind string, duration time.Duration) {
	if m == nil {
		return
	}

	m.publishTime.WithLabelValues(string(provider), kind).Observe(duration.Seconds())
}

func (m *Metrics) ObservePublishBytes(provider contract.Provider, kind string, encodedBytes int) {
	if m == nil {
		return
	}

	m.publishBytes.WithLabelValues(string(provider), kind).Observe(float64(encodedBytes))
}

func (m *Metrics) ObserveAccepted(provider contract.Provider, kind contract.ObservationKind, at time.Time, interval time.Duration, hasInterval bool) {
	if m == nil {
		return
	}

	m.lastAccepted.WithLabelValues(string(provider), string(kind)).Set(float64(at.Unix()))

	if hasInterval {
		m.acceptInterval.WithLabelValues(string(provider), string(kind)).Observe(interval.Seconds())
	}
}

func (m *Metrics) ObserveInvalidFailureTuple(provider contract.Provider, kind string) {
	if m == nil {
		return
	}

	m.invalidTuple.WithLabelValues(string(provider), kind).Inc()
}

func (m *Metrics) ObserveEnqueue(result EnqueueResult) {
	if m == nil {
		return
	}

	m.enqueue.WithLabelValues(boundedEnqueue(string(result))).Inc()
}

func (m *Metrics) ObserveDiscoveryCycle(failed bool) {
	if m == nil {
		return
	}

	result := discoveryCycleOK

	if failed {
		result = discoveryCycleCandidateLoadFailed
	}

	m.discoveryCycle.WithLabelValues(result).Inc()
}

func boundedResult(value string) string {
	switch value {
	case resultSuccess, resultTimeout, resultCanceled, resultParserDrift, resultFailed, resultSuperseded:
		return value
	default:
		return resultFailed
	}
}

func boundedAcquire(value string) string {
	switch value {
	case resultAcquired, resultNotAcquired, resultSuperseded, resultError:
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
	case outcomeInserted, outcomeDuplicate, outcomeCollision, outcomeRejected, outcomeSuperseded, outcomeEmpty:
		return value
	default:
		return outcomeRejected
	}
}

func boundedEnqueue(value string) string {
	switch EnqueueResult(value) {
	case EnqueueAccepted, EnqueueDeduped, EnqueueFull, EnqueueCanceled, EnqueueInvalid:
		return value
	default:
		return string(EnqueueInvalid)
	}
}
