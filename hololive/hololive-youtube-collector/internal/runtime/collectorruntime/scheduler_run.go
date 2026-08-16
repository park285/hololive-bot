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

func (s *leaseScheduler) runSpec(ctx context.Context, spec *joblease.JobSpec) {
	runner, ok := s.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	if !ok {
		proof := contract.LeaseProof{}
		s.logFailure("collect", collecterr.Failed, spec, &proof)
		return
	}
	lease, err := s.acquireLease(ctx, spec)
	if lease == nil {
		return
	}
	s.runAcquired(ctx, runner, spec, lease, err)
}

func (s *leaseScheduler) acquireLease(ctx context.Context, spec *joblease.JobSpec) (joblease.Lease, error) {
	lease, err := s.repository.Acquire(ctx, spec, s.owner)
	if errors.Is(err, joblease.ErrNotAcquired) {
		s.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultNotAcquired)
		return nil, nil
	}
	if err != nil {
		s.observeAcquireError(spec, err)
		return nil, err
	}
	s.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultAcquired)
	return lease, nil
}

func (s *leaseScheduler) observeAcquireError(spec *joblease.JobSpec, err error) {
	if errors.Is(err, joblease.ErrProjectionStale) || errors.Is(err, joblease.ErrTargetDisabled) {
		return
	}
	s.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultError)
	proof := contract.LeaseProof{}
	s.logFailure("acquire", collecterr.AcquireFailed, spec, &proof)
}

func (s *leaseScheduler) runAcquired(ctx context.Context, runner JobRunner, spec *joblease.JobSpec, lease joblease.Lease, _ error) {
	proof := lease.Proof()
	started := time.Now()
	err := s.repository.Run(ctx, lease, func(runCtx context.Context, leaseProof contract.LeaseProof) error {
		return s.collectAndPublish(runCtx, runner, spec, lease, &leaseProof)
	})
	s.metrics.ObserveAttempt(spec.Provider, spec.CollectionJobKind, attemptResult(err), time.Since(started))
	if ignoreRunError(err) {
		s.observeFenceLost(spec, err)
		return
	}
	s.deferFailedRun(ctx, lease, spec, &proof, err)
}

func ignoreRunError(err error) bool {
	return err == nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, joblease.ErrFenceLost) ||
		errors.Is(err, sourceobservation.ErrProjectionStale) ||
		errors.Is(err, sourceobservation.ErrTargetDisabled)
}

func (s *leaseScheduler) observeFenceLost(spec *joblease.JobSpec, err error) {
	if errors.Is(err, joblease.ErrFenceLost) {
		s.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phaseCollect)
	}
}

func (s *leaseScheduler) deferFailedRun(
	ctx context.Context,
	lease joblease.Lease,
	spec *joblease.JobSpec,
	proof *contract.LeaseProof,
	err error,
) {
	retryAt := s.retryAt(err)
	if deferErr := lease.Defer(ctx, retryAt, collecterr.Code(err)); deferErr != nil && !errors.Is(deferErr, joblease.ErrFenceLost) {
		s.logFailure("defer", collecterr.DeferFailed, spec, proof)
		return
	}
	s.logFailure("collect", collecterr.Code(err), spec, proof)
}

func (s *leaseScheduler) collectAndPublish(
	ctx context.Context,
	runner JobRunner,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
) error {
	generations, err := s.publisher.LoadContractGenerations(ctx, spec.Provider, runner.Emissions())
	if err != nil {
		return err
	}
	enabled, err := s.loadEnabledSubjects(ctx, proof.ProjectionGeneration, runner.TargetKinds())
	if err != nil {
		return err
	}
	output, err := s.collectOutput(ctx, runner, spec, proof, generations, enabled)
	if err != nil {
		return err
	}
	return s.publishOutput(ctx, spec, lease, proof, output)
}

func (s *leaseScheduler) collectOutput(
	ctx context.Context,
	runner JobRunner,
	spec *joblease.JobSpec,
	proof *contract.LeaseProof,
	generations map[contract.ObservationKind]int64,
	enabled map[contract.ObservationKind][]string,
) (collectutil.RunOutput, error) {
	if err := s.acquireProvider(ctx, spec.Provider); err != nil {
		return collectutil.RunOutput{}, collecterr.FromContext(err)
	}
	defer s.releaseProvider(spec.Provider)
	input := &collectutil.RunInput{
		Spec:                *spec,
		Lease:               *proof,
		ContractGenerations: generations,
		MaxPages:            s.collector.MaxPages,
		MaxAggregateBytes:   s.collector.MaxAggregateBytes,
		EnabledSubjects:     enabled,
	}
	return runner.Collect(ctx, input)
}

func (s *leaseScheduler) publishOutput(
	ctx context.Context,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
	output collectutil.RunOutput,
) error {
	if len(output.Observations) == 0 {
		s.metrics.ObservePublish(spec.Provider, spec.CollectionJobKind, outcomeEmpty)
		return lease.Complete(ctx)
	}
	publishCtx, cancel := context.WithTimeout(ctx, s.config.PublishBudget)
	defer cancel()
	result, err := s.publisher.Publish(publishCtx, proof, output)
	if err != nil {
		s.observePublishError(spec, output, err)
		return err
	}
	s.observePublished(output, result)
	s.metrics.ObserveSuccess(spec.Provider, spec.CollectionJobKind, time.Now().UTC())
	return nil
}

func (s *leaseScheduler) observePublishError(spec *joblease.JobSpec, output collectutil.RunOutput, err error) {
	if supersededError(err) {
		s.observePublishOutcome(spec.Provider, output, outcomeSuperseded)
		return
	}
	if errors.Is(err, joblease.ErrFenceLost) {
		s.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phasePublish)
	}
	s.observePublishOutcome(spec.Provider, output, outcomeRejected)
}

func (s *leaseScheduler) loadEnabledSubjects(
	ctx context.Context,
	generation int64,
	kinds []contract.ObservationKind,
) (map[contract.ObservationKind][]string, error) {
	result := make(map[contract.ObservationKind][]string, len(kinds))
	for _, kind := range kinds {
		subjects, err := s.repository.EnabledSubjects(ctx, generation, kind)
		if err != nil {
			return nil, err
		}
		result[kind] = subjects
	}
	return result, nil
}

func (s *leaseScheduler) acquireProvider(ctx context.Context, provider contract.Provider) error {
	gate := s.gates[provider]
	if gate == nil {
		return collecterr.New(collecterr.Failed, "provider gate is not configured")
	}
	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *leaseScheduler) releaseProvider(provider contract.Provider) {
	gate := s.gates[provider]
	if gate == nil {
		return
	}
	select {
	case <-gate:
	default:
	}
}

func (s *leaseScheduler) observePublished(output collectutil.RunOutput, result sourceobservation.PublishBatchResult) {
	for i := range output.Observations {
		envelope := &output.Observations[i]
		outcome := publishedOutcome(result, i)
		if i < len(result.Results) && outcome != outcomeCollision {
			s.metrics.ObservePublishedObservation(result.Results[i].ObservationID)
		}
		s.metrics.ObservePublish(envelope.Provider, string(envelope.ObservationKind), outcome)
		s.metrics.ObserveCompleteness(envelope.Provider, string(envelope.ObservationKind), envelope.Completeness, envelope.Continuity)
	}
}

func publishedOutcome(result sourceobservation.PublishBatchResult, index int) string {
	if index >= len(result.Results) {
		return outcomeInserted
	}
	if result.Results[index].Outcome == sourceobservation.PublishDuplicate {
		return outcomeDuplicate
	}
	if result.Results[index].Outcome == sourceobservation.PublishCollision {
		return outcomeCollision
	}
	return outcomeInserted
}

func (s *leaseScheduler) observePublishOutcome(provider contract.Provider, output collectutil.RunOutput, outcome string) {
	for i := range output.Observations {
		envelope := &output.Observations[i]
		s.metrics.ObservePublish(provider, string(envelope.ObservationKind), outcome)
	}
}

func attemptResult(err error) string {
	if err == nil {
		return resultSuccess
	}
	if supersededError(err) {
		return resultSuperseded
	}
	return attemptFailureResult(collecterr.Code(err))
}

func supersededError(err error) bool {
	return errors.Is(err, sourceobservation.ErrProjectionStale) ||
		errors.Is(err, sourceobservation.ErrTargetDisabled)
}

func attemptFailureResult(code string) string {
	if code == collecterr.Timeout {
		return resultTimeout
	}
	if code == collecterr.Canceled {
		return resultCanceled
	}
	if code == collecterr.ParserDrift {
		return resultParserDrift
	}
	if code == collecterr.PaginationGap {
		return resultPaginationGap
	}
	return resultFailed
}

func (s *leaseScheduler) retryAt(err error) time.Time {
	now := time.Now().UTC()
	minAt := now.Add(s.config.MinRetryDelay)
	maxAt := now.Add(s.config.MaxRetryDelay)
	if retryAt, ok := collecterr.RetryAt(err); ok {
		return clampRetryAt(retryAt, minAt, maxAt)
	}
	return now.Add(s.config.MinRetryDelay + (s.config.MaxRetryDelay-s.config.MinRetryDelay)/2)
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

func (s *leaseScheduler) logFailure(phase, code string, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	if s.logger == nil {
		return
	}
	s.logger.Warn("YouTube collection job failed",
		slog.String("job_key", spec.JobKey),
		slog.String("provider", string(spec.Provider)),
		slog.String("job_kind", spec.CollectionJobKind),
		slog.String("subject_key", spec.SubjectKey),
		slog.Int64("fence_epoch", proof.FenceEpoch),
		slog.Int64("projection_generation", proof.ProjectionGeneration),
		slog.String("error_code", code),
		slog.String("phase", phase),
	)
}
