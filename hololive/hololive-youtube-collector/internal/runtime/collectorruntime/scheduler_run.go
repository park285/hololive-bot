package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

type collectionExecutor struct {
	repository    *joblease.Repository
	registry      *Registry
	publisher     *Publisher
	metrics       *Metrics
	owner         string
	logger        *slog.Logger
	collector     collectorconfig.Config
	retryBounds   collection.RetryBounds
	gates         map[contract.Provider]chan struct{}
	readiness     *readinessTracker
	workerTracker *workercontract.ExecutorTracker
	workerTotals  *workercontract.Counters
	reportFatal   func(error)
}

func (e *collectionExecutor) runSpec(ctx context.Context, spec *joblease.JobSpec) {
	registration, ok := e.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	if !ok {
		proof := contract.LeaseProof{}
		e.logFailure("collect", string(collecterr.Failed), collecterr.UnknownClass, "", spec, &proof)

		return
	}

	lease, err := e.acquireLease(ctx, spec)
	if err != nil || lease == nil {
		return
	}

	e.runAcquired(ctx, registration, spec, lease)
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

func (e *collectionExecutor) runAcquired(ctx context.Context, registration RegisteredRunner, spec *joblease.JobSpec, lease joblease.Lease) {
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

	if errors.Is(err, collecterr.ErrInvalidFailureTuple) {
		e.metrics.ObserveInvalidFailureTuple(spec.Provider, spec.CollectionJobKind)
	}

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
	if runResult.Outcome == joblease.LeaseRunCallbackCompleted {
		return true
	}

	if runResult.Outcome == joblease.LeaseRunFenceLost || runResult.Outcome == joblease.LeaseRunReleasedAfterParentCancel {
		e.observeFenceLost(spec, runResult.Err)

		if fatalCollectionError(runResult.Err) {
			e.failSupervision("cleanup", runResult.Err, spec, proof)
		}

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
	if failure, ok := errors.AsType[*FatalRuntimeError](err); ok && failure.Phase == "result_validation" {
		// 검증 실패의 terminal 처리는 callback join 뒤 한 번만 수행합니다.
		e.deferInvariant(ctx, lease, spec, proof, err)
		e.reportFatal(failure)

		return
	}

	if fatalCollectionError(err) {
		e.reportFatal(&FatalRuntimeError{Phase: "collection", Err: err})
	}

	if supersededError(err) {
		e.handleSuperseded(ctx, lease, spec, proof)

		return
	}

	if ignoreRunError(err) {
		e.observeFenceLost(spec, err)

		return
	}

	e.deferFailedRun(ctx, lease, spec, proof, err)
}

func fatalCollectionError(err error) bool {
	if err == nil {
		return false
	}

	class := collecterr.ClassOf(err)
	if !collecterr.IsUnclassified(err) && (class == collecterr.ClassInternal || class == collecterr.ClassProtocol) {
		return true
	}

	// Join된 release/context 오류의 순서가 callback의 fatal 분류를 가리지 않게 합니다.
	if joined, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok {
		return slices.ContainsFunc(joined.Unwrap(), fatalCollectionError)
	}

	return false
}

func (e *collectionExecutor) handleSuperseded(ctx context.Context, lease joblease.Lease, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	releaseErr := e.releaseSuperseded(ctx, lease)
	if releaseErr == nil || errors.Is(releaseErr, collection.ErrFenceLost) {
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
		errors.Is(err, collection.ErrFenceLost) ||
		supersededError(err)
}

func (e *collectionExecutor) observeFenceLost(spec *joblease.JobSpec, err error) {
	if errors.Is(err, collection.ErrFenceLost) {
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

	if deferErr := e.deferLease(cleanupCtx, lease, diagnostic, retryAt); deferErr != nil && !errors.Is(deferErr, collection.ErrFenceLost) {
		e.logFailure("defer", string(collecterr.DeferFailed), string(collecterr.ClassOf(deferErr)), collecterr.DiagnosticOf(deferErr).Detail(), spec, proof)

		return
	}

	e.logFailure("collect", code, class, detail, spec, proof)
}

// deferLease는 executor가 소유한 재시도 범위로 typed defer 입력을 한 번 만들어 lease에 넘깁니다.
// 입력을 만들 수 없는 진단(예: defer할 수 없는 code)도 lease 거절과 같은 비치명 defer 실패로 돌려줍니다.
func (e *collectionExecutor) deferLease(
	ctx context.Context,
	lease joblease.Lease,
	diagnostic contract.FailureDiagnostic,
	retryAt time.Time,
) error {
	schedule, err := collection.NewRetryAtSchedule(retryAt)
	if err != nil {
		return fmt.Errorf("retry schedule: %w", err)
	}

	input, err := collection.NewDeferCollectionInput(diagnostic, e.retryBounds, schedule)
	if err != nil {
		return fmt.Errorf("defer collection input: %w", err)
	}

	if err := lease.Defer(ctx, input); err != nil {
		return fmt.Errorf("defer: %w", err)
	}

	return nil
}

func (e *collectionExecutor) collectAndPublish(
	ctx context.Context,
	registration RegisteredRunner,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
) error {
	input, result, fatal, err := e.collectWithAdmission(ctx, registration, spec, proof)
	if err != nil {
		return err
	}

	if validationErr := ValidateCollectResult(&input, registration, &result, fatal); validationErr != nil {
		return &FatalRuntimeError{Phase: "result_validation", Err: errors.Join(validationErr, fatal)}
	}

	if fatal != nil {
		return fatal
	}

	if err := e.commitCollectResult(ctx, spec, lease, proof, &result); err != nil {
		return fmt.Errorf("commit collect result: %w", err)
	}

	return nil
}

// collectWithAdmission은 provider gate를 얻은 뒤 실행 입력 적재와 수집만 gate 안에서 수행합니다.
// Collector가 반환하면 이 함수가 끝나면서 gate를 정확히 한 번 반환하므로 결과 검증과 발행은 gate 밖에서 진행합니다.
// Collector가 반환하지 않으면 upstream 호출이 남아 있을 수 있어 gate를 계속 점유합니다.
// 반환값 setupErr는 수집 전에 끝난 실패이고, fatal은 검증 대상인 수집 실패입니다.
func (e *collectionExecutor) collectWithAdmission(
	ctx context.Context,
	registration RegisteredRunner,
	spec *joblease.JobSpec,
	proof *contract.LeaseProof,
) (input collection.RunInput, result collection.CollectResult, fatal, setupErr error) {
	admissionCtx, admissionCancel := context.WithTimeout(ctx, e.collector.ProviderAdmissionTimeout)
	err := e.acquireProvider(admissionCtx, spec.Provider)

	admissionCancel()

	if err != nil {
		return collection.RunInput{}, collection.CollectResult{}, nil, fmt.Errorf("provider admission: %w", err)
	}

	defer e.releaseProvider(spec.Provider)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return collection.RunInput{}, collection.CollectResult{}, nil, fmt.Errorf("start collect: %w", ctxErr)
	}

	input, err = e.buildRunInput(ctx, registration, spec, proof)
	if err != nil {
		return collection.RunInput{}, collection.CollectResult{}, nil, fmt.Errorf("build run input: %w", err)
	}

	collectCtx, collectCancel := context.WithTimeout(ctx, registration.Profile().CollectTimeout())

	result, fatal = e.runCollector(collectCtx, registration.Runner(), &input)

	collectErr := collectCtx.Err()

	collectCancel()

	if collectErr != nil {
		result = collection.CollectResult{}
		fatal = errors.Join(fatal, collectErr)
	}

	return input, result, fatal, nil
}

func (e *collectionExecutor) runCollector(ctx context.Context, runner collection.JobRunner, input *collection.RunInput) (collection.CollectResult, error) {
	var (
		result     collection.CollectResult
		collectErr error
	)

	returned := false
	recoveredErr := panicguard.RunE(e.logger, panicguard.BackgroundTask, "youtube-collector-collect", func() error {
		result, collectErr = runner.Collect(ctx, input)
		returned = true

		return nil
	})
	// 반환 오류의 분류는 provider가 소유합니다. 실제 panic만 실행 불변 위반으로 분류합니다.
	if !returned {
		return collection.CollectResult{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, recoveredErr)
	}

	if collectErr != nil {
		return result, fmt.Errorf("collect runner: %w", collectErr)
	}

	return result, nil
}

func (e *collectionExecutor) buildRunInput(
	ctx context.Context,
	registration RegisteredRunner,
	spec *joblease.JobSpec,
	proof *contract.LeaseProof,
) (collection.RunInput, error) {
	dbCtx, dbCancel := context.WithTimeout(ctx, e.collector.DBTimeout)
	snapshot, err := e.publisher.LoadContractSnapshot(dbCtx, registration)

	dbCancel()

	if err != nil {
		return collection.RunInput{}, fmt.Errorf("load contract snapshot: %w", err)
	}

	dbCtx, dbCancel = context.WithTimeout(ctx, e.collector.DBTimeout)

	targets, err := e.repository.LoadTargetSnapshot(
		dbCtx, proof, spec, registration.Contract(), e.collector.MaxTargetRosterRows,
	)

	dbCancel()

	if err != nil {
		return collection.RunInput{}, fmt.Errorf("load target snapshot: %w", err)
	}

	input, err := collection.NewRunInput(
		registration.Contract(), spec.SubjectKey, proof, snapshot, targets,
		e.collector.MaxPages, e.collector.MaxSuccessResponseBytes,
	)
	if err != nil {
		return collection.RunInput{}, fmt.Errorf("run input: %w", err)
	}

	return input, nil
}

func (e *collectionExecutor) commitCollectResult(
	ctx context.Context,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
	result *collection.CollectResult,
) error {
	output := result.Output()
	if result.Kind() == collection.CollectComplete && output.Empty() {
		e.metrics.ObservePublish(spec.Provider, spec.CollectionJobKind, outcomeEmpty)

		dbCtx, cancel := context.WithTimeout(ctx, e.collector.DBTimeout)

		defer cancel()

		if err := lease.CompleteCurrent(dbCtx); err != nil {
			return fmt.Errorf("complete current: %w", err)
		}

		e.recordTerminalSuccess(nil)
		e.metrics.ObserveSuccess(spec.Provider, spec.CollectionJobKind, time.Now().UTC())

		return nil
	}

	publishCtx, cancel := context.WithTimeout(ctx, e.collector.PublishTimeout)

	defer cancel()

	var (
		published sourceobservation.PublishBatchResult
		err       error
	)

	if result.Kind() == collection.CollectPartial {
		retry, retryErr := collection.NewRetryAtSchedule(e.retryAt(resultPartialCause(result)))
		if retryErr != nil {
			return fmt.Errorf("retry at: %w", retryErr)
		}

		published, err = e.publisher.PublishPartial(publishCtx, proof, result, retry, e.retryBounds)
	} else {
		published, err = e.publisher.PublishComplete(publishCtx, proof, output)
	}

	if err != nil {
		e.observePublishError(spec, output, err)

		return fmt.Errorf("publish complete: %w", err)
	}

	e.observePublished(output, published)
	e.recordTerminalSuccess(&published)
	e.metrics.ObserveSuccess(spec.Provider, spec.CollectionJobKind, time.Now().UTC())

	return nil
}

func resultPartialCause(result *collection.CollectResult) error {
	partial, _ := result.PartialFailure()
	if partial == nil {
		return collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial result failure is missing")
	}

	if err := partial.Cause(); err != nil {
		return fmt.Errorf("cause: %w", err)
	}

	return nil
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

	retryAt := time.Now().UTC().Add(e.retryBounds.Maximum)

	if deferErr := e.deferLease(cleanupCtx, lease, collecterr.DiagnosticOf(err), retryAt); deferErr != nil &&
		!errors.Is(deferErr, collection.ErrFenceLost) {
		e.logFailure("defer", string(collecterr.DeferFailed), string(collecterr.ClassOf(deferErr)), collecterr.DiagnosticOf(deferErr).Detail(), spec, proof)
	}
}
