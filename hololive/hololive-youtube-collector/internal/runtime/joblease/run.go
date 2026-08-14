package joblease

import (
	"context"
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type RunFunc func(ctx context.Context, proof contract.LeaseProof) error

func (r *Repository) Run(ctx context.Context, lease Lease, run RunFunc) error {
	if r == nil || lease == nil || run == nil {
		return fmt.Errorf("run collection job: %w", ErrInvalidJob)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- run(runCtx, lease.Proof())
	}()

	ticker := time.NewTicker(r.config.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			cancel()
			if err != nil {
				return fmt.Errorf("run collection job: %w", err)
			}
			return nil
		case <-ticker.C:
			if err := lease.Renew(runCtx); err != nil {
				cancel()
				runErr := <-result
				if runErr != nil && !errors.Is(runErr, context.Canceled) {
					return fmt.Errorf("run collection job: renew lease: %w", errors.Join(err, runErr))
				}
				return fmt.Errorf("run collection job: renew lease: %w", err)
			}
		case <-ctx.Done():
			cancel()
			runErr := <-result
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), r.config.PublishBudget)
			releaseErr := lease.Release(releaseCtx)
			releaseCancel()
			return fmt.Errorf("run collection job: canceled: %w", errors.Join(ctx.Err(), runErr, releaseErr))
		}
	}
}
