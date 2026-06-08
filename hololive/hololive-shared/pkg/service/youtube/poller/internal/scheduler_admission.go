package polling

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/internal/admission"
)

type jobClaimDeferrer interface {
	Defer(ctx context.Context, retryAfter time.Duration) (bool, error)
}

func isAdmissionDeferredPollError(err error) bool {
	return admission.IsDeferred(err)
}

func admissionRetryAfterFromError(err error, fallback time.Duration) time.Duration {
	if retryAfter, ok := admission.RetryAfter(err); ok && retryAfter > 0 {
		return retryAfter
	}
	var delayed retryDelayError
	if errors.As(err, &delayed) && delayed.RetryDelay() > 0 {
		return delayed.RetryDelay()
	}
	return fallback
}

func (s *Scheduler) updateJobNextRunAfterAdmissionDeferred(job *Job, pollErr error, now time.Time) {
	retryAfter := admissionRetryAfterFromError(pollErr, s.errorBackoffMin)
	if retryAfter <= 0 {
		retryAfter = s.errorBackoffMin
	}

	job.consecutiveFailures = 0
	job.NextRunAt = now.Add(retryAfter)

	s.logger.Debug("Poll job rescheduled after admission defer",
		"poller", job.Poller.Name(),
		"channel_id", job.ChannelID,
		"reason", string(JobSkipAdmissionDeferred),
		"retry_after", retryAfter,
		"next_run_at", job.NextRunAt)
}

func (s *Scheduler) deferJobClaim(ctx context.Context, job *Job, claim JobClaim, pollErr error) error {
	retryAfter := admissionRetryAfterFromError(pollErr, s.errorBackoffMin)
	if retryAfter <= 0 {
		retryAfter = s.errorBackoffMin
	}

	deferrer, ok := claim.(jobClaimDeferrer)
	if !ok {
		s.releaseJobClaim(ctx, job, claim)
		return pollErr
	}

	deferred, err := deferrer.Defer(ctx, retryAfter)
	result := boolResult(deferred, err)
	s.metrics.ObserveJobDefer(job.Poller.Name(), result)
	s.logger.Debug("job_defer",
		"poller", job.Poller.Name(),
		"channel_id", job.ChannelID,
		"result", result,
		"retry_after", retryAfter)
	if err != nil {
		return fmt.Errorf("defer poll job claim: %w", err)
	}
	if !deferred {
		return fmt.Errorf("defer poll job claim: ownership lost")
	}
	return pollErr
}
