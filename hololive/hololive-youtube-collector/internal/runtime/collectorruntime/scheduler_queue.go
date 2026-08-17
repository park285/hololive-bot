package collectorruntime

import (
	"context"
	"strings"

	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func (s *leaseScheduler) enqueue(ctx context.Context, spec *joblease.JobSpec) EnqueueResult {
	if !validJobSpec(spec) {
		return EnqueueInvalid
	}
	if ctx.Err() != nil {
		return EnqueueCanceled
	}
	result, marked := s.markQueued(spec.JobKey)
	if !marked {
		return result
	}
	return s.sendQueued(ctx, spec)
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
	defer s.wg.Done()
	if err := panicguard.RunE(s.logger, "youtube-collector-worker", func() error {
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
	if err := panicguard.RunE(s.logger, "youtube-collector-job", func() error {
		s.runSpec(ctx, spec)
		return nil
	}); err != nil {
		s.reportFatal(collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err))
	}
}

func (s *leaseScheduler) nextSpec(ctx context.Context) (joblease.JobSpec, bool) {
	if ctx.Err() != nil {
		return joblease.JobSpec{}, false
	}
	select {
	case <-ctx.Done():
		return joblease.JobSpec{}, false
	case spec := <-s.queue:
		return s.acceptDequeued(ctx, &spec)
	}
}

func (s *leaseScheduler) acceptDequeued(ctx context.Context, spec *joblease.JobSpec) (joblease.JobSpec, bool) {
	if spec == nil {
		return joblease.JobSpec{}, false
	}
	if ctx.Err() != nil {
		s.unmarkQueued(spec.JobKey)
		return joblease.JobSpec{}, false
	}
	return *spec, true
}

func (s *leaseScheduler) markQueued(jobKey string) (EnqueueResult, bool) {
	s.mu.Lock()
	if _, exists := s.queued[jobKey]; exists {
		s.mu.Unlock()
		return EnqueueDeduped, false
	}
	if s.queued == nil {
		s.queued = make(map[string]struct{})
	}
	s.queued[jobKey] = struct{}{}
	overflow := len(s.queued) > s.config.QueueCapacity
	if overflow {
		delete(s.queued, jobKey)
	}
	s.mu.Unlock()
	if overflow {
		s.reportFatal(collecterr.New(collecterr.Internal, collecterr.ClassInternal, "lease scheduler queued set exceeded queue capacity"))
		return EnqueueInvalid, false
	}
	return EnqueueAccepted, true
}

func (s *leaseScheduler) unmarkQueued(jobKey string) {
	s.mu.Lock()
	delete(s.queued, jobKey)
	s.mu.Unlock()
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
	s.mu.Lock()
	s.queued = make(map[string]struct{})
	s.mu.Unlock()
}
