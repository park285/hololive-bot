package collectorruntime

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func (s *leaseScheduler) enqueue(ctx context.Context, spec *joblease.JobSpec) EnqueueResult {
	if !validJobSpec(spec) {
		s.workerTotals.RecordAdmission(workercontract.AdmissionRejected)

		return EnqueueInvalid
	}

	if ctx.Err() != nil {
		s.workerTotals.RecordAdmission(workercontract.AdmissionRejected)

		return EnqueueCanceled
	}

	result, marked := s.markQueued(spec.JobKey)
	if !marked {
		s.recordEnqueueAdmission(result)

		return result
	}

	result = s.sendQueued(ctx, spec)
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

func (s *leaseScheduler) sendQueued(ctx context.Context, spec *joblease.JobSpec) EnqueueResult {
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
	if err := panicguard.RunE(s.logger, panicguard.BackgroundTask, "youtube-collector-worker", func() error {
		for {
			spec, ok := s.nextSpec(ctx)
			if !ok {
				return nil
			}

			s.runQueued(ctx, &spec)
		}
	}); err != nil {
		s.reportFatal(collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err))
	}
}

func (s *leaseScheduler) runQueued(ctx context.Context, spec *joblease.JobSpec) {
	if spec == nil {
		return
	}

	defer s.unmarkQueued(spec.JobKey)

	if err := panicguard.RunE(s.logger, panicguard.BackgroundTask, "youtube-collector-job", func() error {
		s.executor.runSpec(ctx, spec)

		return nil
	}); err != nil {
		s.reportFatal(collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err))
	}
}

// nextSpec는 lease 획득 전에 만료 항목을 버리고 실행할 다음 항목을 반환합니다.
// 반환값 false는 취소 또는 불변식 위반으로 worker가 종료됨을 뜻합니다.
func (s *leaseScheduler) nextSpec(ctx context.Context) (joblease.JobSpec, bool) {
	for {
		if ctx.Err() != nil {
			return joblease.JobSpec{}, false
		}

		select {
		case <-ctx.Done():
			return joblease.JobSpec{}, false
		case spec := <-s.queue:
			switch s.acceptDequeued(ctx, &spec) {
			case dequeueRun:
				return spec, true
			case dequeueStale:
				continue
			case dequeueStop:
				return joblease.JobSpec{}, false
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
func (s *leaseScheduler) acceptDequeued(ctx context.Context, spec *joblease.JobSpec) dequeueDecision {
	if spec == nil {
		return dequeueStop
	}

	if ctx.Err() != nil {
		s.unmarkQueued(spec.JobKey)

		return dequeueStop
	}

	maxAge := s.queueMaxAge

	s.queueMu.Lock()

	enqueuedAt, tracked := s.queuedAt[spec.JobKey]
	if !tracked {
		delete(s.queued, spec.JobKey)
		s.queueMu.Unlock()
		s.reportFatal(collecterr.New(collecterr.Internal, collecterr.ClassInternal, "lease scheduler dequeued a job without a queued timestamp"))

		return dequeueStop
	}

	age := time.Since(enqueuedAt)
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

func (s *leaseScheduler) markQueued(jobKey string) (EnqueueResult, bool) {
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
	s.queuedAt[jobKey] = time.Now()

	overflow := len(s.queued) > s.queueCapacity

	if overflow {
		delete(s.queued, jobKey)
		delete(s.queuedAt, jobKey)
	}

	s.queueMu.Unlock()

	if overflow {
		s.reportFatal(collecterr.New(collecterr.Internal, collecterr.ClassInternal, "lease scheduler queued set exceeded queue capacity"))

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
