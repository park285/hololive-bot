package collectorruntime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/park285/shared-go/pkg/workercontract"
)

type collectionExecutor struct {
	repository    *joblease.Repository
	registry      *Registry
	publisher     *Publisher
	metrics       *Metrics
	owner         string
	logger        *slog.Logger
	config        joblease.Config
	collector     settings.YouTubeCollectorConfig
	gates         map[contract.Provider]chan struct{}
	readiness     *readinessTracker
	workerTracker *workercontract.ExecutorTracker
	workerTotals  *workercontract.Counters
	reportFatal   func(error)
}

func (s *leaseScheduler) exec() *collectionExecutor {
	return newCollectionExecutor(s)
}

func (s *leaseScheduler) runSpec(ctx context.Context, spec *joblease.JobSpec) {
	s.exec().runSpec(ctx, spec)
}

func (s *leaseScheduler) releaseSuperseded(ctx context.Context, lease joblease.Lease) error {
	return s.exec().releaseSuperseded(ctx, lease)
}

func (e *collectionExecutor) runSpec(ctx context.Context, spec *joblease.JobSpec) {
	registration, ok := e.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	if !ok {
		proof := contract.LeaseProof{}
		e.logFailure("collect", string(collecterr.Failed), collecterr.UnknownClass, "", spec, &proof)
		return
	}
	lease, err := e.acquireLease(ctx, spec)
	if lease == nil {
		return
	}
	e.runAcquired(ctx, registration, spec, lease, err)
}

func (e *collectionExecutor) acquireLease(ctx context.Context, spec *joblease.JobSpec) (joblease.Lease, error) {
	dbCtx, cancel := context.WithTimeout(ctx, e.collector.DBTimeout)
	defer cancel()
	lease, err := e.repository.Acquire(dbCtx, spec, e.owner)
	if errors.Is(err, joblease.ErrNotAcquired) {
		e.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultNotAcquired)
		return nil, nil
	}
	if err != nil {
		e.observeAcquireError(spec, err)
		return nil, err
	}
	e.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultAcquired)
	return lease, nil
}

func (e *collectionExecutor) observeAcquireError(spec *joblease.JobSpec, err error) {
	if supersededError(err) {
		return
	}
	e.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultError)
	proof := contract.LeaseProof{}
	e.logFailure("acquire", string(collecterr.AcquireFailed), string(collecterr.ClassOf(err)), collecterr.DiagnosticOf(err).Detail(), spec, &proof)
}

func (e *collectionExecutor) runAcquired(ctx context.Context, registration RegisteredRunner, spec *joblease.JobSpec, lease joblease.Lease, _ error) {
	proof := lease.Proof()
	started := time.Now()
	attemptID := e.workerTracker.BeginAttempt(started)
	runResult := e.repository.Run(ctx, lease, func(runCtx context.Context, leaseProof contract.LeaseProof) error {
		return e.collectAndPublish(runCtx, registration, spec, lease, &leaseProof)
	})
	err := runResult.Err
	e.workerTracker.EndAttempt(attemptID)
	e.workerTotals.RecordAttempt(collectionAttemptOutcome(err))
	e.metrics.ObserveAttempt(spec.Provider, spec.CollectionJobKind, attemptResult(err), time.Since(started))
	if e.handleLeaseRunOutcome(runResult, spec) {
		return
	}
	e.handleRunError(ctx, lease, spec, &proof, err)
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

func (e *collectionExecutor) handleLeaseRunOutcome(runResult joblease.LeaseRunResult, spec *joblease.JobSpec) bool {
	if leaseRunCompletesWithoutAction(runResult.Outcome) {
		return true
	}
	if runResult.Outcome == joblease.LeaseRunFenceLost {
		e.observeFenceLost(spec, joblease.ErrFenceLost)
		return true
	}
	if leaseRunIsSupervisionFailure(runResult.Outcome) {
		e.reportFatal(&FatalRuntimeError{Phase: "lease_supervision", Err: runResult.Err})
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

func (e *collectionExecutor) handleRunError(
	ctx context.Context,
	lease joblease.Lease,
	spec *joblease.JobSpec,
	proof *contract.LeaseProof,
	err error,
) {
	if supersededError(err) {
		e.handleSuperseded(ctx, lease)
		return
	}
	if ignoreRunError(err) {
		e.observeFenceLost(spec, err)
		return
	}
	e.deferFailedRun(ctx, lease, spec, proof, err)
	class := collecterr.ClassOf(err)
	if class == collecterr.ClassInternal || class == collecterr.ClassProtocol {
		e.reportFatal(&FatalRuntimeError{Phase: "collection", Err: err})
	}
}

func (e *collectionExecutor) handleSuperseded(ctx context.Context, lease joblease.Lease) {
	releaseErr := e.releaseSuperseded(ctx, lease)
	if releaseErr != nil && !errors.Is(releaseErr, joblease.ErrFenceLost) {
		e.reportFatal(&FatalRuntimeError{Phase: "lease_supervision", Err: releaseErr})
	}
}

func (e *collectionExecutor) releaseSuperseded(ctx context.Context, lease joblease.Lease) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.collector.CleanupTimeout)
	defer cancel()
	return lease.Release(cleanupCtx, joblease.ReleaseSuperseded)
}

func ignoreRunError(err error) bool {
	return err == nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, joblease.ErrFenceLost) ||
		supersededError(err)
}

func (e *collectionExecutor) observeFenceLost(spec *joblease.JobSpec, err error) {
	if errors.Is(err, joblease.ErrFenceLost) {
		e.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phaseCollect)
	}
}

func (e *collectionExecutor) deferFailedRun(
	ctx context.Context,
	lease joblease.Lease,
	spec *joblease.JobSpec,
	proof *contract.LeaseProof,
	err error,
) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.collector.CleanupTimeout)
	defer cancel()
	retryAt := e.retryAt(err)
	diagnostic := collecterr.DiagnosticOf(err)
	code := string(diagnostic.Code())
	class := string(diagnostic.Class())
	detail := diagnostic.Detail()
	if deferErr := lease.Defer(cleanupCtx, retryAt, code, class, detail); deferErr != nil && !errors.Is(deferErr, joblease.ErrFenceLost) {
		e.logFailure("defer", string(collecterr.DeferFailed), string(collecterr.ClassOf(deferErr)), collecterr.DiagnosticOf(deferErr).Detail(), spec, proof)
		return
	}
	e.logFailure("collect", code, class, detail, spec, proof)
}

func (e *collectionExecutor) collectAndPublish(
	ctx context.Context,
	registration RegisteredRunner,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
) error {
	admissionCtx, admissionCancel := context.WithTimeout(ctx, e.collector.ProviderAdmissionTimeout)
	err := e.acquireProvider(admissionCtx, spec.Provider)
	admissionCancel()
	if err != nil {
		return collecterr.FromContext(err)
	}
	defer e.releaseProvider(spec.Provider)
	if err := ctx.Err(); err != nil {
		return err
	}
	dbCtx, dbCancel := context.WithTimeout(ctx, e.collector.DBTimeout)
	snapshot, err := e.publisher.LoadContractSnapshot(dbCtx, registration)
	dbCancel()
	if err != nil {
		return err
	}
	dbCtx, dbCancel = context.WithTimeout(ctx, e.collector.DBTimeout)
	targets, err := e.repository.LoadTargetSnapshot(
		dbCtx, proof, spec, registration.Contract(), e.collector.MaxTargetRosterRows,
	)
	dbCancel()
	if err != nil {
		return err
	}
	input, err := collectutil.NewRunInput(
		spec, proof, snapshot, targets, e.collector.MaxPages,
		e.collector.MaxSuccessResponseBytes, registration.Contract(),
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
		e.deferInvariant(ctx, lease, spec, proof, validationErr)
		e.reportFatal(&FatalRuntimeError{Phase: "result_validation", Err: validationErr})
		return nil
	}
	if fatal != nil {
		return fatal
	}
	return e.commitCollectResult(ctx, spec, lease, proof, &result)
}

func (e *collectionExecutor) commitCollectResult(
	ctx context.Context,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
	result *collectutil.CollectResult,
) error {
	output := result.Output()
	if result.Kind() == collectutil.CollectComplete && output.Empty() {
		e.metrics.ObservePublish(spec.Provider, spec.CollectionJobKind, outcomeEmpty)
		dbCtx, cancel := context.WithTimeout(ctx, e.collector.DBTimeout)
		defer cancel()
		if err := lease.CompleteCurrent(dbCtx); err != nil {
			return err
		}
		e.recordTerminalSuccess(nil)
		e.metrics.ObserveSuccess(spec.Provider, spec.CollectionJobKind, time.Now().UTC())
		return nil
	}
	publishCtx, cancel := context.WithTimeout(ctx, e.collector.PublishTimeout)
	defer cancel()
	var published sourceobservation.PublishBatchResult
	var err error
	if result.Kind() == collectutil.CollectPartial {
		retry, retryErr := joblease.NewRetryAt(e.retryAt(resultPartialCause(result)))
		if retryErr != nil {
			return retryErr
		}
		published, err = e.publisher.PublishPartial(
			publishCtx, proof, result, retry,
			sourceobservation.RetryBounds{Minimum: e.config.MinRetryDelay, Maximum: e.config.MaxRetryDelay},
		)
	} else {
		published, err = e.publisher.PublishComplete(publishCtx, proof, output)
	}
	if err != nil {
		e.observePublishError(spec, output, err)
		return err
	}
	e.observePublished(output, published)
	e.recordTerminalSuccess(&published)
	e.metrics.ObserveSuccess(spec.Provider, spec.CollectionJobKind, time.Now().UTC())
	return nil
}

func resultPartialCause(result *collectutil.CollectResult) error {
	partial, _ := result.PartialFailure()
	if partial == nil {
		return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial result failure is missing")
	}
	return partial.Cause()
}

func (e *collectionExecutor) deferInvariant(
	ctx context.Context,
	lease joblease.Lease,
	spec *joblease.JobSpec,
	proof *contract.LeaseProof,
	err error,
) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.collector.CleanupTimeout)
	defer cancel()
	retryAt := time.Now().UTC().Add(e.config.MaxRetryDelay)
	diagnostic := collecterr.DiagnosticOf(err)
	if deferErr := lease.Defer(cleanupCtx, retryAt, string(diagnostic.Code()), string(diagnostic.Class()), diagnostic.Detail()); deferErr != nil &&
		!errors.Is(deferErr, joblease.ErrFenceLost) {
		e.logFailure("defer", string(collecterr.DeferFailed), string(collecterr.ClassOf(deferErr)), collecterr.DiagnosticOf(deferErr).Detail(), spec, proof)
	}
}
