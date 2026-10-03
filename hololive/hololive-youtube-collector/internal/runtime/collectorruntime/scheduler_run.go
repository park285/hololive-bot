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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	collectorconfig "github.com/kapu/hololive-shared/pkg/config/settings/collector"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
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
	config        joblease.Config
	collector     collectorconfig.Config
	gates         map[contract.Provider]chan struct{}
	readiness     *readinessTracker
	workerTracker *workercontract.ExecutorTracker
	workerTotals  *workercontract.Counters
	reportFatal   func(error)
}

func (e *collectionExecutor) runSpec(ctx context.Context, spec *joblease.JobSpec) {
	ctx, span := otel.Tracer("hololive/collector").Start(ctx, "youtube.collection.attempt", trace.WithAttributes(
		attribute.String("youtube.collector.instance_id", e.collector.InstanceID),
		attribute.String("collection.provider", string(spec.Provider)),
		attribute.String("collection.job_kind", spec.CollectionJobKind),
	))

	var (
		result       *collectutil.CollectResult
		operationErr error
	)

	defer func() { finishCollectionResultSpan(span, result, operationErr) }()

	registration, ok := e.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	if !ok {
		proof := contract.LeaseProof{}

		operationErr = errors.New("collection runner registration missing")

		e.logFailure(ctx, "collect", string(collecterr.Failed), collecterr.UnknownClass, "", spec, &proof)

		return
	}

	lease, err := e.acquireLease(ctx, spec)
	if err != nil || lease == nil {
		if errors.Is(err, joblease.ErrNotAcquired) {
			span.SetAttributes(attribute.String("collection.admission", "not_acquired"))
		} else {
			operationErr = err
		}

		return
	}

	result, operationErr = e.runAcquired(ctx, registration, spec, lease)
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
		e.observeAcquireError(ctx, spec, err)

		return nil, fmt.Errorf("acquire: %w", err)
	}

	e.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultAcquired)

	return lease, nil
}

// observeAcquireError는 획득 시점 membership·projection 무효를 오류가 아닌 superseded로 센다.
func (e *collectionExecutor) observeAcquireError(ctx context.Context, spec *joblease.JobSpec, err error) {
	if supersededError(err) {
		e.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultSuperseded)

		return
	}

	e.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultError)

	proof := contract.LeaseProof{}
	e.logFailure(ctx, "acquire", string(collecterr.AcquireFailed), string(collecterr.ClassOf(err)), collecterr.DiagnosticOf(err).Detail(), spec, &proof)
}

func (e *collectionExecutor) runAcquired(ctx context.Context, registration RegisteredRunner, spec *joblease.JobSpec, lease joblease.Lease) (*collectutil.CollectResult, error) {
	var collected *collectutil.CollectResult

	proof := lease.Proof()
	started := time.Now()
	attemptID := e.workerTracker.BeginAttempt(started)
	runResult := e.repository.Run(ctx, lease, func(runCtx context.Context, leaseProof contract.LeaseProof) error {
		var err error

		collected, err = e.collectAndPublish(runCtx, registration, spec, lease, &leaseProof)

		return err
	})
	err := runResult.Err

	e.workerTracker.EndAttempt(attemptID)
	e.workerTotals.RecordAttempt(collectionAttemptOutcome(err))
	e.metrics.ObserveAttempt(spec.Provider, spec.CollectionJobKind, attemptResult(err), time.Since(started))

	if errors.Is(err, collecterr.ErrInvalidFailureTuple) {
		e.metrics.ObserveInvalidFailureTuple(spec.Provider, spec.CollectionJobKind)
	}

	trace.SpanFromContext(ctx).SetAttributes(attribute.String("collection.lease_outcome", string(runResult.Outcome)))

	if e.handleLeaseRunOutcome(ctx, runResult, spec, &proof) {
		// callback 완료 통지를 받은 경우만 결과를 읽는다. 취소 후 남은 callback과 경합하지 않는다.
		if runResult.Outcome == joblease.LeaseRunCallbackCompleted {
			return collected, nil
		}

		return nil, err
	}

	e.handleRunError(ctx, lease, spec, &proof, err)

	return nil, err
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

func (e *collectionExecutor) handleLeaseRunOutcome(ctx context.Context, runResult joblease.LeaseRunResult, spec *joblease.JobSpec, proof *contract.LeaseProof) bool {
	switch runResult.Outcome {
	case joblease.LeaseRunCallbackCompleted:
		return true
	case joblease.LeaseRunFenceLost:
		// renew가 실제 소유 손실을 확인한 경우다. callback 오류에 섞인 fence 손실을 따로 세지 않는다.
		e.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phaseRenew)
		e.failFatalCleanup(ctx, runResult.Err, spec, proof)

		return true
	case joblease.LeaseRunReleasedAfterSuperseded:
		// 소유는 유지된 채 membership이 무효가 되어 join 뒤 fenced release까지 끝났다. lease 손실이 아니다.
		e.failFatalCleanup(ctx, runResult.Err, spec, proof)

		return true
	case joblease.LeaseRunReleasedAfterParentCancel:
		e.observeFenceLost(spec, runResult.Err)
		e.failFatalCleanup(ctx, runResult.Err, spec, proof)

		return true
	case joblease.LeaseRunReleasedAfterRenewFailure, joblease.LeaseRunCleanupTimedOut:
		e.observeSupervisionFailure(ctx, runResult, spec, proof)

		return true
	case joblease.LeaseRunCallbackFailed:
		return false
	default:
		return false
	}
}

func (e *collectionExecutor) failFatalCleanup(ctx context.Context, err error, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	if fatalCollectionError(err) {
		e.failSupervision(ctx, "cleanup", err, spec, proof)
	}
}

func (e *collectionExecutor) observeSupervisionFailure(ctx context.Context, runResult joblease.LeaseRunResult, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	phase := "cleanup"

	if runResult.Outcome == joblease.LeaseRunReleasedAfterRenewFailure {
		phase = phaseRenew
		e.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phaseRenew)
	}

	e.failSupervision(ctx, phase, runResult.Err, spec, proof)
}

func (e *collectionExecutor) failSupervision(ctx context.Context, phase string, err error, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	diagnostic := collecterr.DiagnosticOf(err)
	e.logFailure(ctx, phase, string(diagnostic.Code()), string(diagnostic.Class()), diagnostic.Detail(), spec, proof)

	if fatalCollectionError(err) {
		e.reportFatal(&FatalRuntimeError{Phase: "lease_supervision", Err: err})
	}
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
	if releaseErr == nil || errors.Is(releaseErr, joblease.ErrFenceLost) {
		return
	}

	e.failSupervision(ctx, "release", releaseErr, spec, proof)
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

// observeFenceLost는 callback join 뒤 fence 손실을 한 번만 센다. Publish·empty complete terminal에서 생긴 손실은
// leasePhaseError 표식으로 phase=publish, 그 밖의 callback 경로는 phase=collect다.
func (e *collectionExecutor) observeFenceLost(spec *joblease.JobSpec, err error) {
	if !errors.Is(err, joblease.ErrFenceLost) {
		return
	}

	phase := phaseCollect

	if marked, ok := errors.AsType[*leasePhaseError](err); ok {
		phase = marked.phase
	}

	e.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phase)
}

// leasePhaseError는 오류가 생긴 lease 단계를 표시한다. 분류·errors.Is는 감싼 오류를 그대로 따른다.
type leasePhaseError struct {
	phase string
	err   error
}

func (e *leasePhaseError) Error() string { return e.err.Error() }

func (e *leasePhaseError) Unwrap() error { return e.err }

func markLeasePhase(phase string, err error) error {
	if err == nil {
		return nil
	}

	return &leasePhaseError{phase: phase, err: err}
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
		e.logFailure(ctx, "defer", string(collecterr.DeferFailed), string(collecterr.ClassOf(deferErr)), collecterr.DiagnosticOf(deferErr).Detail(), spec, proof)

		return
	}

	e.logFailure(ctx, "collect", code, class, detail, spec, proof)
}

func (e *collectionExecutor) collectAndPublish(
	ctx context.Context,
	registration RegisteredRunner,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
) (*collectutil.CollectResult, error) {
	admissionCtx, admissionCancel := context.WithTimeout(ctx, e.collector.ProviderAdmissionTimeout)
	err := e.acquireProvider(admissionCtx, spec.Provider)

	admissionCancel()

	if err != nil {
		return nil, fmt.Errorf("provider admission: %w", err)
	}

	defer e.releaseProvider(spec.Provider)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("start collect: %w", ctxErr)
	}

	inputCtx, inputSpan := otel.Tracer("hololive/collector").Start(ctx, "youtube.collection.input.load")
	input, err := e.buildRunInput(inputCtx, registration, spec, proof)
	finishCollectionSpan(inputSpan, err)

	if err != nil {
		return nil, fmt.Errorf("build run input: %w", err)
	}

	collectCtx, collectCancel := context.WithTimeout(ctx, registration.Profile().CollectTimeout())
	result, fatal := e.runCollector(collectCtx, registration.Runner(), &input)
	collectErr := collectCtx.Err()

	collectCancel()

	if collectErr != nil {
		result = collectutil.CollectResult{}
		fatal = errors.Join(fatal, collectErr)
	}

	_, validationSpan := otel.Tracer("hololive/collector").Start(ctx, "youtube.collection.validate")
	validationErr := ValidateCollectResult(&input, registration, &result, fatal)
	finishCollectionSpan(validationSpan, validationErr)

	if validationErr != nil {
		return nil, &FatalRuntimeError{Phase: "result_validation", Err: errors.Join(validationErr, fatal)}
	}

	if fatal != nil {
		return nil, fatal
	}

	if err := e.commitCollectResult(ctx, spec, lease, proof, &result); err != nil {
		return nil, fmt.Errorf("commit collect result: %w", err)
	}

	return &result, nil
}

func (e *collectionExecutor) runCollector(ctx context.Context, runner collectutil.JobRunner, input *collectutil.RunInput) (result collectutil.CollectResult, resultErr error) {
	ctx, span := otel.Tracer("hololive/collector").Start(ctx, "youtube.collection.fetch")

	defer func() { finishCollectionResultSpan(span, &result, resultErr) }()

	var collectErr error

	returned := false
	recoveredErr := panicguard.RunE(e.logger, panicguard.BackgroundTask, "youtube-collector-collect", func() error {
		result, collectErr = runner.Collect(ctx, input)
		returned = true

		return nil
	})
	// 반환 오류의 분류는 provider가 소유합니다. 실제 panic만 실행 불변 위반으로 분류합니다.
	if !returned {
		return collectutil.CollectResult{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, recoveredErr)
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
) (collectutil.RunInput, error) {
	dbCtx, dbCancel := context.WithTimeout(ctx, e.collector.DBTimeout)
	snapshot, err := e.publisher.LoadContractSnapshot(dbCtx, registration)

	dbCancel()

	if err != nil {
		return collectutil.RunInput{}, fmt.Errorf("load contract snapshot: %w", err)
	}

	dbCtx, dbCancel = context.WithTimeout(ctx, e.collector.DBTimeout)

	targets, err := e.repository.LoadTargetSnapshot(
		dbCtx, proof, spec, registration.Contract(), e.collector.MaxTargetRosterRows,
	)

	dbCancel()

	if err != nil {
		return collectutil.RunInput{}, fmt.Errorf("load target snapshot: %w", err)
	}

	input, err := collectutil.NewRunInput(
		spec, proof, snapshot, targets, e.collector.MaxPages,
		e.collector.MaxSuccessResponseBytes, registration.Contract(),
	)
	if err != nil {
		return collectutil.RunInput{}, fmt.Errorf("run input: %w", err)
	}

	return input, nil
}

func (e *collectionExecutor) commitCollectResult(
	ctx context.Context,
	spec *joblease.JobSpec,
	lease joblease.Lease,
	proof *contract.LeaseProof,
	result *collectutil.CollectResult,
) (resultErr error) {
	ctx, span := otel.Tracer("hololive/collector").Start(ctx, "youtube.collection.publish")

	defer func() { finishCollectionSpan(span, resultErr) }()

	output := result.Output()
	if result.Kind() == collectutil.CollectComplete && output.Empty() {
		dbCtx, cancel := context.WithTimeout(ctx, e.collector.DBTimeout)

		defer cancel()

		// tab이 없는 정상 empty COMPLETE는 checkpoint 없이 lease만 완료한다. 관측 수락과 구분해 job kind로 센다.
		if err := lease.CompleteCurrent(dbCtx); err != nil {
			return markLeasePhase(phasePublish, fmt.Errorf("complete current: %w", err))
		}

		e.metrics.ObservePublish(spec.Provider, spec.CollectionJobKind, outcomeEmpty)
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

	if result.Kind() == collectutil.CollectPartial {
		retry, retryErr := sourceobservation.NewRetryAtSchedule(e.retryAt(resultPartialCause(result)))
		if retryErr != nil {
			return fmt.Errorf("retry at: %w", retryErr)
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

		return markLeasePhase(phasePublish, fmt.Errorf("publish complete: %w", err))
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

	retryAt := time.Now().UTC().Add(e.config.MaxRetryDelay)
	diagnostic := collecterr.DiagnosticOf(err)

	if deferErr := lease.Defer(cleanupCtx, retryAt, string(diagnostic.Code()), string(diagnostic.Class()), diagnostic.Detail()); deferErr != nil &&
		!errors.Is(deferErr, joblease.ErrFenceLost) {
		e.logFailure(ctx, "defer", string(collecterr.DeferFailed), string(collecterr.ClassOf(deferErr)), collecterr.DiagnosticOf(deferErr).Detail(), spec, proof)
	}
}
