package collectorruntime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func (s *leaseScheduler) observePublishError(spec *joblease.JobSpec, output collectutil.RunOutput, err error) {
	s.exec().observePublishError(spec, output, err)
}

func (s *leaseScheduler) recordTerminalSuccess(published *sourceobservation.PublishBatchResult) {
	if s == nil {
		return
	}
	s.exec().recordTerminalSuccess(published)
}

func (s *leaseScheduler) logFailure(phase, code, class, detail string, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	s.exec().logFailure(phase, code, class, detail, spec, proof)
}

func (e *collectionExecutor) observePublishError(spec *joblease.JobSpec, output collectutil.RunOutput, err error) {
	if supersededError(err) {
		e.observePublishOutcome(spec.Provider, output, outcomeSuperseded)
		return
	}
	if errors.Is(err, joblease.ErrFenceLost) {
		e.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phasePublish)
	}
	e.observePublishOutcome(spec.Provider, output, outcomeRejected)
}

func (e *collectionExecutor) acquireProvider(ctx context.Context, provider contract.Provider) error {
	gate := e.gates[provider]
	if gate == nil {
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider gate is not configured")
	}
	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *collectionExecutor) releaseProvider(provider contract.Provider) {
	gate := e.gates[provider]
	if gate == nil {
		return
	}
	select {
	case <-gate:
	default:
	}
}

func (e *collectionExecutor) observePublished(output collectutil.RunOutput, result sourceobservation.PublishBatchResult) {
	observations := output.Observations()
	for i := range observations {
		envelope := &observations[i]
		outcome, ok := publishedOutcome(result, i)
		if !ok {
			continue
		}
		e.metrics.ObservePublish(envelope.Provider, string(envelope.ObservationKind), outcome)
		e.metrics.ObserveCompleteness(envelope.Provider, string(envelope.ObservationKind), envelope.Completeness, envelope.Continuity)
	}
}

func (e *collectionExecutor) recordTerminalSuccess(published *sourceobservation.PublishBatchResult) {
	if e == nil || e.readiness == nil {
		return
	}
	e.readiness.ObserveCollectionSuccess()
	if published == nil {
		return
	}
	e.readiness.AddHandoffCandidates(handoffCandidateIDs(*published)...)
}

func handoffCandidateIDs(result sourceobservation.PublishBatchResult) []int64 {
	ids := make([]int64, 0, len(result.Results))
	for i := range result.Results {
		outcome, ok := publishedOutcome(result, i)
		if !ok || outcome == outcomeCollision {
			continue
		}
		id := result.Results[i].ObservationID
		if id <= 0 {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func publishedOutcome(result sourceobservation.PublishBatchResult, index int) (string, bool) {
	if index < 0 || index >= len(result.Results) {
		return "", false
	}
	return publishOutcomeLabel(result.Results[index].Outcome)
}

func publishOutcomeLabel(outcome sourceobservation.PublishOutcome) (string, bool) {
	if outcome == sourceobservation.PublishInserted {
		return outcomeInserted, true
	}
	if outcome == sourceobservation.PublishDuplicate {
		return outcomeDuplicate, true
	}
	if outcome == sourceobservation.PublishCollision {
		return outcomeCollision, true
	}
	return "", false
}

func (e *collectionExecutor) observePublishOutcome(provider contract.Provider, output collectutil.RunOutput, outcome string) {
	observations := output.Observations()
	for i := range observations {
		envelope := &observations[i]
		e.metrics.ObservePublish(provider, string(envelope.ObservationKind), outcome)
	}
}

func attemptResult(err error) string {
	if err == nil {
		return resultSuccess
	}
	if supersededError(err) {
		return resultSuperseded
	}
	return attemptFailureResult(err)
}

func supersededError(err error) bool {
	return errors.Is(err, joblease.ErrProjectionStale) ||
		errors.Is(err, joblease.ErrTargetDisabled)
}

func attemptFailureResult(err error) string {
	switch collecterr.CodeOf(err) {
	case collecterr.Timeout:
		return resultTimeout
	case collecterr.Canceled:
		return resultCanceled
	case collecterr.ParserDrift:
		return resultParserDrift
	default:
		return resultFailed
	}
}

func (e *collectionExecutor) retryAt(err error) time.Time {
	now := time.Now().UTC()
	minAt := now.Add(e.config.MinRetryDelay)
	maxAt := now.Add(e.config.MaxRetryDelay)
	hint := collecterr.RetryOf(err)
	switch hint.Kind() {
	case collecterr.RetryAt:
		return clampRetryAt(hint.At(), minAt, maxAt)
	case collecterr.RetryAfter:
		return clampRetryAt(now.Add(hint.After()), minAt, maxAt)
	case collecterr.RetryDefault:
		return now.Add(e.config.MinRetryDelay + (e.config.MaxRetryDelay-e.config.MinRetryDelay)/2)
	default:
		return now.Add(e.config.MinRetryDelay + (e.config.MaxRetryDelay-e.config.MinRetryDelay)/2)
	}
}

func clampRetryAt(retryAt, minAt, maxAt time.Time) time.Time {
	if retryAt.Before(minAt) {
		return minAt
	}
	if retryAt.After(maxAt) {
		return maxAt
	}
	return retryAt.UTC()
}

func (e *collectionExecutor) logFailure(phase, code, class, detail string, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	if e.logger == nil {
		return
	}
	detail = collecterr.SanitizeDetail(detail)
	e.logger.Warn("YouTube collection job failed",
		slog.String("job_key", spec.JobKey),
		slog.String("provider", string(spec.Provider)),
		slog.String("job_kind", spec.CollectionJobKind),
		slog.String("subject_key", spec.SubjectKey),
		slog.Int64("fence_epoch", proof.FenceEpoch),
		slog.Int64("projection_generation", proof.ProjectionGeneration),
		slog.String("error_code", code),
		slog.String("error_class", class),
		slog.String("error_detail", detail),
		slog.String("phase", phase),
	)
}
