package joblease

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type RunFunc func(ctx context.Context, proof contract.LeaseProof) error

type LeaseRunOutcome string

const (
	LeaseRunCallbackCompleted         LeaseRunOutcome = "CALLBACK_COMPLETED"
	LeaseRunCallbackFailed            LeaseRunOutcome = "CALLBACK_FAILED"
	LeaseRunReleasedAfterParentCancel LeaseRunOutcome = "RELEASED_AFTER_PARENT_CANCEL"
	LeaseRunReleasedAfterRenewFailure LeaseRunOutcome = "RELEASED_AFTER_RENEW_FAILURE"
	LeaseRunFenceLost                 LeaseRunOutcome = "FENCE_LOST"
	LeaseRunCleanupTimedOut           LeaseRunOutcome = "CLEANUP_TIMED_OUT"
)

type LeaseRunResult struct {
	Outcome LeaseRunOutcome
	Err     error
}

func (r *Repository) Run(ctx context.Context, lease Lease, run RunFunc) LeaseRunResult {
	if r == nil || lease == nil || run == nil {
		return LeaseRunResult{Outcome: LeaseRunCallbackFailed, Err: fmt.Errorf("run collection job: %w", ErrInvalidJob)}
	}

	runCtx, cancel := context.WithCancel(ctx)

	defer cancel()

	result := make(chan error, 1)

	go panicguard.Run(nil, panicguard.BackgroundTask, "collection-job-run", func() {
		result <- panicguard.RunE(nil, panicguard.BackgroundTask, "collection-job-run", func() error {
			return run(runCtx, lease.Proof())
		})
	})

	return r.awaitRun(ctx, runCtx, cancel, lease, result)
}

func (r *Repository) awaitRun(
	ctx context.Context,
	runCtx context.Context,
	cancel context.CancelFunc,
	lease Lease,
	result <-chan error,
) LeaseRunResult {
	ticker := time.NewTicker(r.config.RenewInterval)
	defer ticker.Stop()

	for {
		if err, done := r.awaitRunOnce(ctx, runCtx, cancel, lease, result, ticker); done {
			return err
		}
	}
}

func (r *Repository) awaitRunOnce(
	ctx context.Context,
	runCtx context.Context,
	cancel context.CancelFunc,
	lease Lease,
	result <-chan error,
	ticker *time.Ticker,
) (LeaseRunResult, bool) {
	if ctx.Err() != nil {
		return r.handleRunCancel(ctx, cancel, lease, result), true
	}

	select {
	case err := <-result:
		return r.finishAvailableRun(ctx, cancel, lease, err), true
	case <-ticker.C:
		return r.handleRunRenew(runCtx, cancel, lease, result)
	case <-ctx.Done():
		return r.handleRunCancel(ctx, cancel, lease, result), true
	}
}

func (r *Repository) finishAvailableRun(
	ctx context.Context,
	cancel context.CancelFunc,
	lease Lease,
	runErr error,
) LeaseRunResult {
	if ctx.Err() == nil {
		return finishRunResult(cancel, runErr)
	}

	cancel()

	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), r.config.CleanupTimeout)

	defer cleanupCancel()

	releaseErr := releaseWithTimeout(cleanupCtx, lease, ReleaseShutdown, r.config.DBTimeout)

	return LeaseRunResult{
		Outcome: LeaseRunReleasedAfterParentCancel,
		Err:     fmt.Errorf("run collection job: canceled: %w", errors.Join(ctx.Err(), releaseErr, runErr)),
	}
}

func finishRunResult(cancel context.CancelFunc, err error) LeaseRunResult {
	cancel()

	if err != nil {
		return LeaseRunResult{Outcome: LeaseRunCallbackFailed, Err: fmt.Errorf("run collection job: %w", err)}
	}

	return LeaseRunResult{Outcome: LeaseRunCallbackCompleted}
}

func (r *Repository) handleRunRenew(
	runCtx context.Context,
	cancel context.CancelFunc,
	lease Lease,
	result <-chan error,
) (LeaseRunResult, bool) {
	select {
	case err := <-result:
		return r.finishAvailableRun(runCtx, cancel, lease, err), true
	default:
	}

	renewCtx, renewCancel := context.WithTimeout(runCtx, r.config.RenewTimeout)
	err := lease.Renew(renewCtx)

	renewCancel()

	if runCtx.Err() != nil {
		return r.handleRunCancel(runCtx, cancel, lease, result), true
	}

	if err != nil {
		return r.finishRenewFailure(runCtx, cancel, lease, result, err), true
	}

	return LeaseRunResult{}, false
}

func (r *Repository) finishRenewFailure(
	runCtx context.Context,
	cancel context.CancelFunc,
	lease Lease,
	result <-chan error,
	err error,
) LeaseRunResult {
	if errors.Is(err, ErrFenceLost) {
		return r.finishFenceLoss(runCtx, cancel, result)
	}

	cancel()

	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(runCtx), r.config.CleanupTimeout)

	defer cleanupCancel()

	releaseErr := releaseWithTimeout(cleanupCtx, lease, ReleaseRenewFail, r.config.DBTimeout)
	joined, runErr := waitRunResult(cleanupCtx, result)

	if !joined {
		return LeaseRunResult{Outcome: LeaseRunCleanupTimedOut, Err: fmt.Errorf("run collection job: join after renew failure: %w", errors.Join(err, releaseErr, runErr))}
	}

	return LeaseRunResult{
		Outcome: LeaseRunReleasedAfterRenewFailure,
		Err:     fmt.Errorf("run collection job: renew lease: %w", errors.Join(err, releaseErr, runErr)),
	}
}

func (r *Repository) finishFenceLoss(
	runCtx context.Context,
	cancel context.CancelFunc,
	result <-chan error,
) LeaseRunResult {
	select {
	case runErr := <-result:
		return finishRunResult(cancel, runErr)
	default:
	}

	cancel()

	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(runCtx), r.config.CleanupTimeout)

	defer cleanupCancel()

	joined, runErr := waitRunResult(cleanupCtx, result)
	if !joined {
		return LeaseRunResult{Outcome: LeaseRunCleanupTimedOut, Err: fmt.Errorf("run collection job: join after fence loss: %w", errors.Join(ErrFenceLost, runErr))}
	}

	return LeaseRunResult{Outcome: LeaseRunFenceLost, Err: errors.Join(ErrFenceLost, runErr)}
}

func (r *Repository) handleRunCancel(
	ctx context.Context,
	cancel context.CancelFunc,
	lease Lease,
	result <-chan error,
) LeaseRunResult {
	cancel()

	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), r.config.CleanupTimeout)

	defer cleanupCancel()

	releaseErr := releaseWithTimeout(cleanupCtx, lease, ReleaseShutdown, r.config.DBTimeout)
	joined, runErr := waitRunResult(cleanupCtx, result)

	if !joined {
		return LeaseRunResult{Outcome: LeaseRunCleanupTimedOut, Err: fmt.Errorf("run collection job: canceled cleanup: %w", errors.Join(ctx.Err(), releaseErr, runErr))}
	}

	return LeaseRunResult{
		Outcome: LeaseRunReleasedAfterParentCancel,
		Err:     fmt.Errorf("run collection job: canceled: %w", errors.Join(ctx.Err(), releaseErr, runErr)),
	}
}

func releaseWithTimeout(ctx context.Context, lease Lease, reason ReleaseReason, timeout time.Duration) error {
	releaseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := lease.Release(releaseCtx, reason); err != nil {
		return fmt.Errorf("release: %w", err)
	}

	return nil
}

func waitRunResult(ctx context.Context, result <-chan error) (bool, error) {
	// Cleanup 기한이 지났더라도 이미 도착한 결과는 join 완료로 판정합니다.
	select {
	case runErr := <-result:
		return true, runErr
	default:
	}

	select {
	case runErr := <-result:
		return true, runErr
	case <-ctx.Done():
		return false, fmt.Errorf("join collection job runner: %w", ctx.Err())
	}
}
