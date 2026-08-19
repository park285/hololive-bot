package collectorruntime

import (
	"context"
	"errors"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/park285/shared-go/pkg/workercontract"
)

func (s *leaseScheduler) runSpec(ctx context.Context, spec *joblease.JobSpec) {
	registration, ok := s.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	if !ok {
		proof := contract.LeaseProof{}
		s.logFailure("collect", string(collecterr.Failed), collecterr.UnknownClass, "", spec, &proof)
		return
	}
	lease, err := s.acquireLease(ctx, spec)
	if lease == nil {
		return
	}
	s.runAcquired(ctx, registration, spec, lease, err)
}

func (s *leaseScheduler) acquireLease(ctx context.Context, spec *joblease.JobSpec) (joblease.Lease, error) {
	dbCtx, cancel := context.WithTimeout(ctx, s.collector.DBTimeout)
	defer cancel()
	lease, err := s.repository.Acquire(dbCtx, spec, s.owner)
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
	if supersededError(err) {
		return
	}
	s.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultError)
	proof := contract.LeaseProof{}
	s.logFailure("acquire", string(collecterr.AcquireFailed), string(collecterr.ClassOf(err)), collecterr.DiagnosticOf(err).Detail(), spec, &proof)
}

func (s *leaseScheduler) runAcquired(ctx context.Context, registration RegisteredRunner, spec *joblease.JobSpec, lease joblease.Lease, _ error) {
	proof := lease.Proof()
	started := time.Now()
	attemptID := s.workerTracker.BeginAttempt(started)
	runResult := s.repository.Run(ctx, lease, func(runCtx context.Context, leaseProof contract.LeaseProof) error {
		return s.collectAndPublish(runCtx, registration, spec, lease, &leaseProof)
	})
	err := runResult.Err
	s.workerTracker.EndAttempt(attemptID)
	s.workerTotals.RecordAttempt(collectionAttemptOutcome(err))
	s.metrics.ObserveAttempt(spec.Provider, spec.CollectionJobKind, attemptResult(err), time.Since(started))
	if s.handleLeaseRunOutcome(runResult, spec) {
		return
	}
	s.handleRunError(ctx, lease, spec, &proof, err)
}

func collectionAttemptOutcome(err error) workercontract.AttemptOutcome {
	switch attemptResult(err) {
	case resultSuccess:
		return workercontract.AttemptSuccess
	case resultTimeout:
		return workercontract.AttemptTimeout
	case resultCanceled, resultSuperseded:
		return workercontract.AttemptCanceled
	default:
		return workercontract.AttemptFailed
	}
}

func (s *leaseScheduler) handleLeaseRunOutcome(runResult joblease.LeaseRunResult, spec *joblease.JobSpec) bool {
	if leaseRunCompletesWithoutAction(runResult.Outcome) {
		return true
	}
	if runResult.Outcome == joblease.LeaseRunFenceLost {
		s.observeFenceLost(spec, joblease.ErrFenceLost)
		return true
	}
	if leaseRunIsSupervisionFailure(runResult.Outcome) {
		s.reportFatal(&FatalRuntimeError{Phase: "lease_supervision", Err: runResult.Err})
		return true
	}
	return false
}

func leaseRunCompletesWithoutAction(outcome joblease.LeaseRunOutcome) bool {
	return outcome == joblease.LeaseRunCallbackCompleted || outcome == joblease.LeaseRunReleasedAfterParentCancel
}

func leaseRunIsSupervisionFailure(outcome joblease.LeaseRunOutcome) bool {
	return outcome == joblease.LeaseRunReleasedAfterRenewFailure || outcome == joblease.LeaseRunCleanupTimedOut
}

func (s *leaseScheduler) handleRunError(
	ctx context.Context,
	lease joblease.Lease,
	spec *joblease.JobSpec,
	proof *contract.LeaseProof,
	err error,
) {
	if supersededError(err) {
		s.handleSuperseded(ctx, lease)
		return
	}
	if ignoreRunError(err) {
		s.observeFenceLost(spec, err)
		return
	}
	s.deferFailedRun(ctx, lease, spec, proof, err)
	class := collecterr.ClassOf(err)
	if class == collecterr.ClassInternal || class == collecterr.ClassProtocol {
		s.reportFatal(&FatalRuntimeError{Phase: "collection", Err: err})
	}
}

func (s *leaseScheduler) handleSuperseded(ctx context.Context, lease joblease.Lease) {
	releaseErr := s.releaseSuperseded(ctx, lease)
	if releaseErr != nil && !errors.Is(releaseErr, joblease.ErrFenceLost) {
		s.reportFatal(&FatalRuntimeError{Phase: "lease_supervision", Err: releaseErr})
	}
}

func (s *leaseScheduler) releaseSuperseded(ctx context.Context, lease joblease.Lease) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.collector.CleanupTimeout)
	defer cancel()
	return lease.Release(cleanupCtx, joblease.ReleaseSuperseded)
}

func ignoreRunError(err error) bool {
	return err == nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, joblease.ErrFenceLost) ||
		supersededError(err)
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
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.collector.CleanupTimeout)
	defer cancel()
	retryAt := s.retryAt(err)
	diagnostic := collecterr.DiagnosticOf(err)
	code := string(diagnostic.Code())
	class := string(diagnostic.Class())
	detail := diagnostic.Detail()
	if deferErr := lease.Defer(cleanupCtx, retryAt, code, class, detail); deferErr != nil && !errors.Is(deferErr, joblease.ErrFenceLost) {
		s.logFailure("defer", string(collecterr.DeferFailed), string(collecterr.ClassOf(deferErr)), collecterr.DiagnosticOf(deferErr).Detail(), spec, proof)
		return
	}
	s.logFailure("collect", code, class, detail, spec, proof)
}

func (s *leaseScheduler) collectAndPublish(
	ctx context.Context,
	registration RegisteredRunner,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
) error {
	admissionCtx, admissionCancel := context.WithTimeout(ctx, s.collector.ProviderAdmissionTimeout)
	err := s.acquireProvider(admissionCtx, spec.Provider)
	admissionCancel()
	if err != nil {
		return collecterr.FromContext(err)
	}
	defer s.releaseProvider(spec.Provider)
	if err := ctx.Err(); err != nil {
		return err
	}
	dbCtx, dbCancel := context.WithTimeout(ctx, s.collector.DBTimeout)
	snapshot, err := s.publisher.LoadContractSnapshot(dbCtx, registration)
	dbCancel()
	if err != nil {
		return err
	}
	dbCtx, dbCancel = context.WithTimeout(ctx, s.collector.DBTimeout)
	targets, err := s.repository.LoadTargetSnapshot(
		dbCtx, proof, spec, registration.Contract(), s.collector.MaxTargetRosterRows,
	)
	dbCancel()
	if err != nil {
		return err
	}
	input, err := collectutil.NewRunInput(
		spec, proof, snapshot, targets, s.collector.MaxPages,
		s.collector.MaxSuccessResponseBytes, registration.Contract(),
	)
	if err != nil {
		return err
	}
	collectCtx, collectCancel := context.WithTimeout(ctx, registration.Profile().CollectTimeout())
	result, fatal := registration.Runner().Collect(collectCtx, &input)
	collectErr := collectCtx.Err()
	collectCancel()
	if collectErr != nil {
		result = collectutil.CollectResult{}
		fatal = collectErr
	}
	if validationErr := ValidateCollectResult(&input, registration, &result, fatal); validationErr != nil {
		s.deferInvariant(ctx, lease, spec, proof, validationErr)
		s.reportFatal(&FatalRuntimeError{Phase: "result_validation", Err: validationErr})
		return nil
	}
	if fatal != nil {
		return fatal
	}
	return s.commitCollectResult(ctx, spec, lease, proof, &result)
}

func (s *leaseScheduler) commitCollectResult(
	ctx context.Context,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
	result *collectutil.CollectResult,
) error {
	output := result.Output()
	if result.Kind() == collectutil.CollectComplete && output.Empty() {
		s.metrics.ObservePublish(spec.Provider, spec.CollectionJobKind, outcomeEmpty)
		dbCtx, cancel := context.WithTimeout(ctx, s.collector.DBTimeout)
		defer cancel()
		if err := lease.CompleteCurrent(dbCtx); err != nil {
			return err
		}
		s.recordTerminalSuccess(nil)
		s.metrics.ObserveSuccess(spec.Provider, spec.CollectionJobKind, time.Now().UTC())
		return nil
	}
	publishCtx, cancel := context.WithTimeout(ctx, s.collector.PublishTimeout)
	defer cancel()
	var published sourceobservation.PublishBatchResult
	var err error
	if result.Kind() == collectutil.CollectPartial {
		retry, retryErr := joblease.NewRetryAt(s.retryAt(resultPartialCause(result)))
		if retryErr != nil {
			return retryErr
		}
		published, err = s.publisher.PublishPartial(
			publishCtx, proof, result, retry,
			sourceobservation.RetryBounds{Minimum: s.config.MinRetryDelay, Maximum: s.config.MaxRetryDelay},
		)
	} else {
		published, err = s.publisher.PublishComplete(publishCtx, proof, output)
	}
	if err != nil {
		s.observePublishError(spec, output, err)
		return err
	}
	s.observePublished(output, published)
	s.recordTerminalSuccess(&published)
	s.metrics.ObserveSuccess(spec.Provider, spec.CollectionJobKind, time.Now().UTC())
	return nil
}

func resultPartialCause(result *collectutil.CollectResult) error {
	partial, _ := result.PartialFailure()
	if partial == nil {
		return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial result failure is missing")
	}
	return partial.Cause()
}

func (s *leaseScheduler) deferInvariant(
	ctx context.Context,
	lease joblease.Lease,
	spec *joblease.JobSpec,
	proof *contract.LeaseProof,
	err error,
) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.collector.CleanupTimeout)
	defer cancel()
	retryAt := time.Now().UTC().Add(s.config.MaxRetryDelay)
	diagnostic := collecterr.DiagnosticOf(err)
	if deferErr := lease.Defer(cleanupCtx, retryAt, string(diagnostic.Code()), string(diagnostic.Class()), diagnostic.Detail()); deferErr != nil &&
		!errors.Is(deferErr, joblease.ErrFenceLost) {
		s.logFailure("defer", string(collecterr.DeferFailed), string(collecterr.ClassOf(deferErr)), collecterr.DiagnosticOf(deferErr).Detail(), spec, proof)
	}
}
