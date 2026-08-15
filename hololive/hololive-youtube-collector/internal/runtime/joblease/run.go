package joblease

import (
	"context"
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/panicguard"
)

type RunFunc func(ctx context.Context, proof contract.LeaseProof) error

func (r *Repository) Run(ctx context.Context, lease Lease, run RunFunc) error {
	if r == nil || lease == nil || run == nil {
		return fmt.Errorf("run collection job: %w", ErrInvalidJob)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	panicguard.Go(nil, "collection-job-run", func() {
		result <- panicguard.RunE(nil, "collection-job-run", func() error {
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
) error {
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
) (error, bool) {
	select {
	case err := <-result:
		return finishRunResult(cancel, err), true
	case <-ticker.C:
		return handleRunRenew(runCtx, cancel, lease, result, r.config.PublishBudget)
	case <-ctx.Done():
		return handleRunCancel(ctx, cancel, lease, result, r.config.PublishBudget), true
	}
}

func finishRunResult(cancel context.CancelFunc, err error) error {
	cancel()
	if err != nil {
		return fmt.Errorf("run collection job: %w", err)
	}
	return nil
}

func handleRunRenew(
	runCtx context.Context,
	cancel context.CancelFunc,
	lease Lease,
	result <-chan error,
	cleanupBudget time.Duration,
) (error, bool) {
	if err := lease.Renew(runCtx); err != nil {
		return finishRenewFailure(runCtx, cancel, result, cleanupBudget, err), true
	}
	return nil, false
}

func finishRenewFailure(
	runCtx context.Context,
	cancel context.CancelFunc,
	result <-chan error,
	cleanupBudget time.Duration,
	err error,
) error {
	cancel()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(runCtx), cleanupBudget)
	defer cleanupCancel()
	runErr := waitRunResult(cleanupCtx, result)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return fmt.Errorf("run collection job: renew lease: %w", errors.Join(err, runErr))
	}
	return fmt.Errorf("run collection job: renew lease: %w", err)
}

func handleRunCancel(
	ctx context.Context,
	cancel context.CancelFunc,
	lease Lease,
	result <-chan error,
	publishBudget time.Duration,
) error {
	cancel()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), publishBudget)
	defer cleanupCancel()
	releaseErr := lease.Release(cleanupCtx)
	runErr := waitRunResult(cleanupCtx, result)
	return fmt.Errorf("run collection job: canceled: %w", errors.Join(ctx.Err(), runErr, releaseErr))
}

func waitRunResult(ctx context.Context, result <-chan error) error {
	select {
	case runErr := <-result:
		return runErr
	case <-ctx.Done():
		return fmt.Errorf("join collection job runner: %w", ctx.Err())
	}
}
