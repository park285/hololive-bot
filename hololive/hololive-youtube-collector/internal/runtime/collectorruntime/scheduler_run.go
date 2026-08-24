package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
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
	if err := s.exec().releaseSuperseded(ctx, lease); err != nil {
		return fmt.Errorf("release superseded: %w", err)
	}

	return nil
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

		return nil, fmt.Errorf("acquire: %w", err)
	}

	if err != nil {
		e.observeAcquireError(spec, err)

		return nil, fmt.Errorf("acquire: %w", err)
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

	if e.handleLeaseRunOutcome(runResult, spec, &proof) {
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

func (e *collectionExecutor) handleLeaseRunOutcome(runResult joblease.LeaseRunResult, spec *joblease.JobSpec, proof *contract.LeaseProof) bool {
	if leaseRunCompletesWithoutAction(runResult.Outcome) {
		return true
	}

	if runResult.Outcome == joblease.LeaseRunFenceLost {
		e.observeFenceLost(spec, joblease.ErrFenceLost)

		return true
	}

	if leaseRunIsSupervisionFailure(runResult.Outcome) {
		e.observeSupervisionFailure(runResult, spec, proof)

		return true
	}

	return false
}

func (e *collectionExecutor) observeSupervisionFailure(runResult joblease.LeaseRunResult, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	phase := "cleanup"

	if runResult.Outcome == joblease.LeaseRunReleasedAfterRenewFailure {
		phase = phaseRenew
		e.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phaseRenew)
	}

	e.failSupervision(phase, runResult.Err, spec, proof)
}

func (e *collectionExecutor) failSupervision(phase string, err error, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	diagnostic := collecterr.DiagnosticOf(err)
	e.logFailure(phase, string(diagnostic.Code()), string(diagnostic.Class()), diagnostic.Detail(), spec, proof)

	if fatalCollectionError(err) {
		e.reportFatal(&FatalRuntimeError{Phase: "lease_supervision", Err: err})
	}
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
		e.handleSuperseded(ctx, lease, spec, proof)

		return
	}

	if ignoreRunError(err) {
		e.observeFenceLost(spec, err)

		return
	}

	e.deferFailedRun(ctx, lease, spec, proof, err)

	if fatalCollectionError(err) {
		e.reportFatal(&FatalRuntimeError{Phase: "collection", Err: err})
	}
}

func fatalCollectionError(err error) bool {
	if err == nil || collecterr.IsUnclassified(err) {
		return false
	}

	class := collecterr.ClassOf(err)

	return class == collecterr.ClassInternal || class == collecterr.ClassProtocol
}

func (e *collectionExecutor) handleSuperseded(ctx context.Context, lease joblease.Lease, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	releaseErr := e.releaseSuperseded(ctx, lease)
	if releaseErr == nil || errors.Is(releaseErr, joblease.ErrFenceLost) {
		return
	}

	e.failSupervision("release", releaseErr, spec, proof)
}

func (e *collectionExecutor) releaseSuperseded(ctx context.Context, lease joblease.Lease) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.collector.CleanupTimeout)
	defer cancel()

	if err := lease.Release(cleanupCtx, joblease.ReleaseSuperseded); err != nil {
		return fmt.Errorf("release: %w", err)
	}

	return nil
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
		return errors.Join(providerAdmissionError(err))
	}

	defer e.releaseProvider(spec.Provider)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("start collect: %w", ctxErr)
	}

	input, err := e.buildRunInput(ctx, registration, spec, proof)
	if err != nil {
		return fmt.Errorf("build run input: %w", err)
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

	if err := e.commitCollectResult(ctx, spec, lease, proof, &result); err != nil {
		return fmt.Errorf("commit collect result: %w", err)
	}

	return nil
}
