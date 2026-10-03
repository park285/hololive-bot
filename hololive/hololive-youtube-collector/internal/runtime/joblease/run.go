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
	// LeaseRunReleasedAfterSuperseded는 소유는 유지됐지만 job membership이 무효가 되어 callback을 취소·join한 뒤
	// fenced superseded release를 시도한 결과다. 소유 손실(LeaseRunFenceLost)과 구분한다.
	LeaseRunReleasedAfterSuperseded LeaseRunOutcome = "RELEASED_AFTER_SUPERSEDED"
	LeaseRunFenceLost               LeaseRunOutcome = "FENCE_LOST"
	LeaseRunCleanupTimedOut         LeaseRunOutcome = "CLEANUP_TIMED_OUT"
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

	if supersededRenewError(err) {
		return r.finishSuperseded(runCtx, cancel, lease, result, err)
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

func supersededRenewError(err error) bool {
	return errors.Is(err, ErrProjectionStale) || errors.Is(err, ErrTargetDisabled)
}

// finishSuperseded는 callback을 먼저 취소·join하고, join이 끝난 경우에만 자기 증명으로 fenced release한다.
// Join 기한을 넘기면 callback이 아직 lease를 쓰고 있을 수 있으므로 해제하지 않고 만료에 맡긴다.
func (r *Repository) finishSuperseded(
	runCtx context.Context,
	cancel context.CancelFunc,
	lease Lease,
	result <-chan error,
	err error,
) LeaseRunResult {
	select {
	case runErr := <-result:
		if runErr == nil {
			return finishRunResult(cancel, nil)
		}

		cancel()

		return r.releaseSuperseded(runCtx, lease, err, runErr)
	default:
	}

	cancel()

	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(runCtx), r.config.CleanupTimeout)

	defer cleanupCancel()

	joined, runErr := waitRunResult(cleanupCtx, result)
	if !joined {
		return LeaseRunResult{Outcome: LeaseRunCleanupTimedOut, Err: fmt.Errorf("run collection job: join after superseded renew: %w", errors.Join(err, runErr))}
	}

	// callback이 취소 전에 이미 terminal까지 끝냈다면 lease는 더 이상 ACTIVE가 아니다.
	if runErr == nil {
		return LeaseRunResult{Outcome: LeaseRunCallbackCompleted}
	}

	return r.releaseSuperseded(cleanupCtx, lease, err, runErr)
}

func (r *Repository) releaseSuperseded(ctx context.Context, lease Lease, renewErr, runErr error) LeaseRunResult {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.config.CleanupTimeout)
	defer cancel()

	releaseErr := releaseWithTimeout(releaseCtx, lease, ReleaseSuperseded, r.config.DBTimeout)
	if errors.Is(releaseErr, ErrFenceLost) {
		// 판정과 해제 사이에 소유를 잃었다. 다른 소유자의 lease는 건드리지 않았다.
		releaseErr = nil
	}

	return LeaseRunResult{
		Outcome: LeaseRunReleasedAfterSuperseded,
		Err:     fmt.Errorf("run collection job: superseded renew: %w", errors.Join(renewErr, releaseErr, runErr)),
	}
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
