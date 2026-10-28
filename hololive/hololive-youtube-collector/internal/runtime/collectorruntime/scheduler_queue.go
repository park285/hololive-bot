package collectorruntime

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

// queuedJob은 admission 시점의 종류별 실행 예산과 큐 만료 정책을 값으로 고정한다.
// 큐 대기는 maxAge까지만 허용하고, 그 안에 시작한 작업에는 전체 실행 예산을 보장한다.
type queuedJob struct {
	joblease.JobSpec

	registration RegisteredRunner
	enqueuedAt   time.Time
	maxAge       time.Duration
	budget       time.Duration
	deadline     time.Time
}

func (e *collectionExecutor) admit(spec *joblease.JobSpec, now time.Time, maxAge time.Duration) (queuedJob, bool) {
	if e == nil || !validJobSpec(spec) || now.IsZero() || maxAge <= 0 {
		return queuedJob{}, false
	}

	registration, ok := e.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	if !ok || registration.Runner() == nil || registration.Profile().Validate() != nil || spec.ValidateContract(registration.Contract()) != nil {
		return queuedJob{}, false
	}

	// acquire와 두 input 조회는 각각 기존 DB 단계 예산을 보존한다.
	// terminal은 publish 또는 empty-complete 중 하나이며 cleanup은 별도 bounded context다.
	cfg := &e.collector
	if cfg.DBTimeout <= 0 || cfg.ProviderAdmissionTimeout <= 0 || cfg.PublishTimeout <= 0 {
		return queuedJob{}, false
	}

	budget, err := checkedAddDuration(cfg.DBTimeout, cfg.ProviderAdmissionTimeout,
		cfg.DBTimeout, cfg.DBTimeout, registration.Profile().CollectTimeout(), max(cfg.PublishTimeout, cfg.DBTimeout))
	if err != nil || budget <= 0 {
		return queuedJob{}, false
	}

	lifetime, err := checkedAddDuration(maxAge, budget)
	if err != nil {
		return queuedJob{}, false
	}

	deadline := now.Add(lifetime)
	if !deadline.After(now) || deadline.Sub(now) != lifetime {
		return queuedJob{}, false
	}

	return queuedJob{JobSpec: *spec, registration: registration, enqueuedAt: now, maxAge: maxAge, budget: budget, deadline: deadline}, true
}

func (job *queuedJob) executionDeadline(started time.Time) time.Time {
	return minTime(job.deadline, started.Add(job.budget))
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
}

func (s *leaseScheduler) enqueue(ctx context.Context, spec *joblease.JobSpec) EnqueueResult {
	if !validJobSpec(spec) {
		s.workerTotals.RecordAdmission(workercontract.AdmissionRejected)

		return EnqueueInvalid
	}

	if ctx.Err() != nil {
		s.workerTotals.RecordAdmission(workercontract.AdmissionRejected)

		return EnqueueCanceled
	}

	job, valid := s.executor.admit(spec, time.Now(), s.queueMaxAge)
	if !valid {
		s.workerTotals.RecordAdmission(workercontract.AdmissionRejected)

		return EnqueueInvalid
	}

	result, marked := s.markQueued(&job)
	if !marked {
		s.recordEnqueueAdmission(result)

		return result
	}

	result = s.sendQueued(ctx, &job)
	s.recordEnqueueAdmission(result)

	return result
}

func (s *leaseScheduler) recordEnqueueAdmission(result EnqueueResult) {
	switch result {
	case EnqueueAccepted:
		s.workerTotals.RecordAdmission(workercontract.AdmissionAccepted)
	case EnqueueDeduped:
		s.workerTotals.RecordAdmission(workercontract.AdmissionDuplicate)
	case EnqueueFull, EnqueueCanceled, EnqueueInvalid:
		s.workerTotals.RecordAdmission(workercontract.AdmissionRejected)
	default:
		s.workerTotals.RecordAdmission(workercontract.AdmissionRejected)
	}
}

func validJobSpec(spec *joblease.JobSpec) bool {
	if spec == nil {
		return false
	}

	trimmed := strings.TrimSpace(spec.JobKey)

	return trimmed != "" && spec.JobKey == trimmed
}

func (s *leaseScheduler) sendQueued(ctx context.Context, spec *queuedJob) EnqueueResult {
	select {
	case s.queue <- *spec:
		return EnqueueAccepted
	case <-ctx.Done():
		s.unmarkQueued(spec.JobKey)

		return EnqueueCanceled
	default:
		s.unmarkQueued(spec.JobKey)

		return EnqueueFull
	}
}

func (s *leaseScheduler) worker(ctx context.Context) {
	if err := panicguard.RunE(s.logger, panicscope.BackgroundTask, "youtube-collector-worker", func() error {
		for {
			spec, ok := s.nextSpec(ctx)
			if !ok {
				return nil
			}

			s.runQueued(ctx, &spec)
		}
	}); err != nil {
		s.reportFatal(collecterr.Wrap(contract.ErrorInternalInvariant, contract.ClassInternal, err))
	}
}

func (s *leaseScheduler) runQueued(ctx context.Context, spec *queuedJob) {
	if spec == nil {
		return
	}

	defer s.unmarkQueued(spec.JobKey)

	if ctx.Err() != nil {
		s.workerTotals.RecordDiscard(workercontract.DiscardShutdown)

		return
	}

	if err := panicguard.RunE(s.logger, panicscope.BackgroundTask, "youtube-collector-job", func() error {
		s.executor.runSpec(ctx, spec)

		return nil
	}); err != nil {
		s.reportFatal(collecterr.Wrap(contract.ErrorInternalInvariant, contract.ClassInternal, err))
	}
}

// nextSpec는 lease 획득 전에 만료 항목을 버리고 실행할 다음 항목을 반환합니다.
// 반환값 false는 취소 또는 불변식 위반으로 worker가 종료됨을 뜻합니다.
func (s *leaseScheduler) nextSpec(ctx context.Context) (queuedJob, bool) {
	for {
		if ctx.Err() != nil {
			return queuedJob{}, false
		}

		select {
		case <-ctx.Done():
			return queuedJob{}, false
		case spec := <-s.queue:
			switch s.acceptDequeued(ctx, &spec) {
			case dequeueRun:
				return spec, true
			case dequeueStale:
				continue
			case dequeueStop:
				return queuedJob{}, false
			}
		}
	}
}

type dequeueDecision int

const (
	dequeueStop dequeueDecision = iota
	dequeueRun
	dequeueStale
)

// acceptDequeued는 lease를 취득하지 않은 만료 항목을 DB terminal 없이 버립니다.
func (s *leaseScheduler) acceptDequeued(ctx context.Context, spec *queuedJob) dequeueDecision {
	if spec == nil {
		return dequeueStop
	}

	if ctx.Err() != nil {
		s.unmarkQueued(spec.JobKey)
		s.workerTotals.RecordDiscard(workercontract.DiscardShutdown)

		return dequeueStop
	}

	maxAge := spec.maxAge

	s.queueMu.Lock()

	_, tracked := s.queuedAt[spec.JobKey]
	if !tracked {
		delete(s.queued, spec.JobKey)
		s.queueMu.Unlock()
		s.reportFatal(collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "lease scheduler dequeued a job without a queued timestamp"))

		return dequeueStop
	}

	age := time.Since(spec.enqueuedAt)
	if age > maxAge {
		delete(s.queued, spec.JobKey)
		delete(s.queuedAt, spec.JobKey)
		s.queueMu.Unlock()
		s.workerTotals.RecordDiscard(workercontract.DiscardStale)
		s.logger.Warn("discarded stale queued collection job before lease acquisition",
			slog.String("job_key", spec.JobKey),
			slog.Duration("queue_age", age),
			slog.Duration("max_age", maxAge),
		)

		return dequeueStale
	}

	delete(s.queuedAt, spec.JobKey)
	s.queueMu.Unlock()

	return dequeueRun
}

func (s *leaseScheduler) markQueued(job *queuedJob) (EnqueueResult, bool) {
	jobKey := job.JobKey

	s.queueMu.Lock()

	if _, exists := s.queued[jobKey]; exists {
		s.queueMu.Unlock()

		return EnqueueDeduped, false
	}

	if s.queued == nil {
		s.queued = make(map[string]struct{})
	}

	if s.queuedAt == nil {
		s.queuedAt = make(map[string]time.Time)
	}

	s.queued[jobKey] = struct{}{}
	s.queuedAt[jobKey] = job.enqueuedAt

	overflow := len(s.queued) > s.queueCapacity

	if overflow {
		delete(s.queued, jobKey)
		delete(s.queuedAt, jobKey)
	}

	s.queueMu.Unlock()

	if overflow {
		s.reportFatal(collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "lease scheduler queued set exceeded queue capacity"))

		return EnqueueInvalid, false
	}

	return EnqueueAccepted, true
}

func (s *leaseScheduler) unmarkQueued(jobKey string) {
	s.queueMu.Lock()
	delete(s.queued, jobKey)
	delete(s.queuedAt, jobKey)
	s.queueMu.Unlock()
}

func (s *leaseScheduler) drainQueue() {
	if s.queue != nil {
		for len(s.queue) > 0 {
			spec := <-s.queue
			s.unmarkQueued(spec.JobKey)
			s.workerTotals.RecordDiscard(workercontract.DiscardShutdown)
		}
	}

	s.resetQueued()
}

func (s *leaseScheduler) resetQueued() {
	s.queueMu.Lock()

	s.queued = make(map[string]struct{})
	s.queuedAt = make(map[string]time.Time)
	s.queueMu.Unlock()
}
