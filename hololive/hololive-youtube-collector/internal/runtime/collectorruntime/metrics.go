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
	resultSuperseded    = "superseded"
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
	outcomeSuperseded   = "superseded"
	outcomeEmpty        = "empty"
	labelProvider       = "provider"
	labelKind           = "kind"
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
	}, []string{labelProvider, labelKind, "result"})
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
	}, []string{labelProvider, labelKind, "result"})
	metrics.leaseLost = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_lease_lost_total",
		Help: "YouTube collection lease losses by phase.",
	}, []string{labelProvider, labelKind, "phase"})
	metrics.publish = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_observation_publish_total",
		Help: "YouTube observation publish outcomes.",
	}, []string{labelProvider, labelKind, "outcome"})
	metrics.publishTime, metrics.publishBytes = newPublishHistograms()
	// 관측 kind별 실제 durable 수락 간격이다. 같은 checkpoint가 commit으로 전진했을 때만 직전 수락 이후 경과를 기록한다.
	// subject는 label로 두지 않아 cardinality가 provider×kind로 제한된다.
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
	}, []string{"result"})
	metrics.invalidTuple = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_collection_invalid_failure_tuple_total",
		Help: "YouTube collection attempts that ended with a code/class tuple outside the durable failure contract.",
	}, []string{labelProvider, labelKind})
	registerer.MustRegister(
		metrics.attempts, metrics.duration, metrics.lastSuccess, metrics.freshness,
		metrics.completeness, metrics.leaseAcquire, metrics.leaseLost, metrics.publish,
		metrics.publishTime, metrics.publishBytes, metrics.acceptInterval, metrics.lastAccepted, metrics.enqueue, metrics.invalidTuple,
	)

	return metrics
}

// newPublishHistograms는 job kind별 발행 트랜잭션 소요 시간과 인코딩 크기 histogram을 만든다.
func newPublishHistograms() (duration, encodedBytes *prometheus.HistogramVec) {
	// 발행 트랜잭션(fence 확인·관측 저장·lease 종료) 한 번의 소요 시간이다. 결과와 무관하게 모든 발행 시도를 기록한다.
	duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_observation_publish_duration_seconds",
		Help:    "YouTube observation publish transaction duration by provider and job kind.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{labelProvider, labelKind})
	// 성공한 발행이 SQL에 보낸 관측 JSON 크기다. 8 MiB 배치 상한(MaxPublishBatchBytes) 근접 빈도를 보려고 상단 버킷을 촘촘히 둔다.
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

// ObservePublishDuration은 job kind별 발행 트랜잭션 한 번의 소요 시간을 기록한다.
func (m *Metrics) ObservePublishDuration(provider contract.Provider, kind string, duration time.Duration) {
	if m == nil {
		return
	}

	m.publishTime.WithLabelValues(string(provider), kind).Observe(duration.Seconds())
}

// ObservePublishBytes는 commit된 발행이 SQL에 보낸 관측 JSON 크기를 job kind별로 기록한다.
func (m *Metrics) ObservePublishBytes(provider contract.Provider, kind string, encodedBytes int) {
	if m == nil {
		return
	}

	m.publishBytes.WithLabelValues(string(provider), kind).Observe(float64(encodedBytes))
}

// ObserveAccepted는 commit된 inserted·duplicate 관측의 수락 시각과, checkpoint가 전진한 경우 직전 수락 이후 간격을 기록한다.
func (m *Metrics) ObserveAccepted(provider contract.Provider, kind contract.ObservationKind, at time.Time, interval time.Duration, hasInterval bool) {
	if m == nil {
		return
	}

	m.lastAccepted.WithLabelValues(string(provider), string(kind)).Set(float64(at.Unix()))

	if hasInterval {
		m.acceptInterval.WithLabelValues(string(provider), string(kind)).Observe(interval.Seconds())
	}
}

// ObserveInvalidFailureTuple은 호출 코드가 계약 밖 failure tuple을 만든 시도를 센다. 오류 자체는 미분류 Internal로
// 지연 처리되므로 이 counter가 위반 추세를 드러내는 유일한 신호다.
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

func boundedResult(value string) string {
	switch value {
	case resultSuccess, resultTimeout, resultCanceled, resultParserDrift, resultPaginationGap, resultFailed, resultSuperseded:
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
