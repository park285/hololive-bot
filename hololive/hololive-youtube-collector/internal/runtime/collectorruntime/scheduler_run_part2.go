package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func providerAdmissionError(err error) error {
	if fromErr := collecterr.FromContext(err); fromErr != nil {
		return fmt.Errorf("from context: %w", fromErr)
	}

	return nil
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
) error {
	output := result.Output()
	if result.Kind() == collectutil.CollectComplete && output.Empty() {
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

	if result.Kind() == collectutil.CollectPartial {
		retry, retryErr := joblease.NewRetryAt(e.retryAt(resultPartialCause(result)))
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

		return fmt.Errorf("publish complete: %w", err)
	}

	e.observePublished(output, published)
	e.recordTerminalSuccess(&published)
	e.metrics.ObserveSuccess(spec.Provider, spec.CollectionJobKind, time.Now().UTC())

	return nil
}

func resultPartialCause(result *collectutil.CollectResult) error {
	partial, _ := result.PartialFailure()
	if partial == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
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
		e.logFailure("defer", string(collecterr.DeferFailed), string(collecterr.ClassOf(deferErr)), collecterr.DiagnosticOf(deferErr).Detail(), spec, proof)
	}
}
