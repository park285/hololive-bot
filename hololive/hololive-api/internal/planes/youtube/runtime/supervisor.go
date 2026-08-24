package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func (r *Runtime) runClaimLoop(ctx context.Context, errCh chan<- error) {
	defer func() { r.loopDone <- struct{}{} }()

	ticker := time.NewTicker(r.Config.ClaimInterval)

	defer ticker.Stop()

	for {
		full, err := r.claimTick(ctx)
		if r.stopAfterClaimError(ctx, errCh, err) {
			return
		}

		if full {
			continue
		}

		ticker.Reset(r.Config.ClaimInterval)

		if !waitTicker(ctx, ticker) {
			return
		}
	}
}

func waitTicker(ctx context.Context, ticker *time.Ticker) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ticker.C:
		return true
	}
}

func (r *Runtime) stopAfterClaimError(ctx context.Context, errCh chan<- error, err error) bool {
	if err == nil {
		return false
	}

	if !r.claiming.Load() || (errors.Is(err, context.Canceled) && ctx.Err() != nil) {
		return true
	}

	if retryableObservationError(err) {
		r.Logger.Error("youtube plane claim tick failed", slog.Any("error", err))

		return false
	}

	r.reportLoopError(ctx, errCh, "claim community observations", err)

	return true
}

func (r *Runtime) runLiveEndLoop(ctx context.Context, errCh chan<- error) {
	defer func() { r.loopDone <- struct{}{} }()

	ticker := time.NewTicker(r.Config.LiveEndFinalizer.Interval)

	defer ticker.Stop()

	if r.stopAfterLiveEndError(ctx, errCh, r.liveEndTick(ctx)) {
		return
	}

	for waitTicker(ctx, ticker) {
		if r.stopAfterLiveEndError(ctx, errCh, r.liveEndTick(ctx)) {
			return
		}
	}
}

func (r *Runtime) stopAfterLiveEndError(ctx context.Context, errCh chan<- error, err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return true
	}

	if retryableObservationError(err) {
		r.Logger.Error("youtube plane live end finalizer failed", slog.Any("error", err))

		return false
	}

	r.reportLoopError(ctx, errCh, "finalize due live ends", err)

	return true
}

func (r *Runtime) liveEndTick(ctx context.Context) error {
	if r == nil || r.finalizer == nil || !r.Config.LiveEndFinalizer.Enabled {
		return nil
	}

	for range r.Config.ClaimBatchSize {
		processed, err := r.finalizeNextDueLiveEnd(ctx)
		if err != nil {
			return fmt.Errorf("%w", err)
		}

		if !processed {
			return nil
		}
	}

	return nil
}

func (r *Runtime) finalizeNextDueLiveEnd(ctx context.Context) (bool, error) {
	var processed bool

	err := r.withDB(ctx, func(ctx context.Context) error {
		var tickErr error

		processed, tickErr = r.finalizer.FinalizeNextDueLiveEnd(ctx, r.Config.LiveEndGrace)
		if tickErr != nil {
			return fmt.Errorf("finalize next due live end: %w", tickErr)
		}

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("with DB: %w", err)
	}

	return processed, nil
}

func (r *Runtime) runProjectionLoop(ctx context.Context, errCh chan<- error) {
	defer func() { r.loopDone <- struct{}{} }()

	ticker := time.NewTicker(r.Config.TargetProjection.Interval)

	defer ticker.Stop()

	for waitTicker(ctx, ticker) {
		if r.stopAfterProjectionError(ctx, errCh, r.refreshProjection(ctx)) {
			return
		}
	}
}

func (r *Runtime) stopAfterProjectionError(ctx context.Context, errCh chan<- error, err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return true
	}

	if isInputReadError(err) || retryableObservationError(err) {
		r.Logger.Error("youtube plane projection refresh failed", slog.Any("error", err))

		return false
	}

	r.reportLoopError(ctx, errCh, "refresh target projection", err)

	return true
}

func (r *Runtime) runWorker(ctx context.Context, errCh chan<- error) {
	defer func() { r.workerDone <- struct{}{} }()

	for {
		work, ok := r.nextWork(ctx)
		if !ok {
			return
		}

		if err := r.processClaim(ctx, work); err != nil {
			r.reportLoopError(ctx, errCh, "consume community observation", err)

			return
		}
	}
}

func (r *Runtime) nextWork(ctx context.Context) (sourceobservation.ClaimWork, bool) {
	if ctx.Err() != nil {
		return sourceobservation.ClaimWork{}, false
	}

	select {
	case <-ctx.Done():
		return sourceobservation.ClaimWork{}, false
	case work, ok := <-r.workCh:
		return work, ok
	}
}

func (r *Runtime) claimTick(ctx context.Context) (bool, error) {
	if r == nil || !r.claiming.Load() {
		return false, nil
	}

	batch, err := r.claimObservationBatch(ctx)
	if err != nil {
		return false, fmt.Errorf("%w", err)
	}

	r.observePendingQueue(ctx)

	for i := range batch.Claims {
		r.remember(batch.Claims[i])
	}

	for i := range batch.Claims {
		if err := sendWork(ctx, r.workCh, batch.Claims[i]); err != nil {
			return false, fmt.Errorf("send work: %w", err)
		}
	}

	return len(batch.Claims) >= r.claim.Limit, nil
}

func (r *Runtime) claimObservationBatch(ctx context.Context) (sourceobservation.ClaimedBatch, error) {
	var batch sourceobservation.ClaimedBatch

	if err := r.withDB(ctx, func(ctx context.Context) error {
		var err error

		batch, err = r.claimer.ClaimBatch(ctx, r.claim)
		if err != nil {
			return fmt.Errorf("claim batch: %w", err)
		}

		return nil
	}); err != nil {
		return batch, fmt.Errorf("claim batch: %w", err)
	}

	return batch, nil
}

func sendWork(ctx context.Context, workCh chan<- sourceobservation.ClaimWork, work sourceobservation.ClaimWork) error {
	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("send work: %w", err)
		}

		return nil
	case workCh <- work:
		return nil
	}
}

func (r *Runtime) processClaim(ctx context.Context, work sourceobservation.ClaimWork) error {
	attemptID := r.workerTracker.BeginAttempt(time.Now())
	outcome := workercontract.AttemptFailed

	defer func() {
		r.workerTracker.EndAttempt(attemptID)
		r.workerTotals.RecordAttempt(outcome)
	}()

	r.remember(work)

	err := r.consumeClaim(ctx, work)
	if err == nil {
		outcome = workercontract.AttemptSuccess

		youtubeFinalizeTotal.Inc()
		youtubeConsumeTotal.WithLabelValues("success").Inc()
		r.forget(work)

		return nil
	}

	if r.forgetLostClaim(err, work) {
		outcome = workercontract.AttemptCanceled
		return nil
	}

	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		outcome = workercontract.AttemptCanceled
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		outcome = workercontract.AttemptTimeout
	}

	if handleErr := r.handleConsumeFailure(ctx, work, err); handleErr != nil {
		return fmt.Errorf("handle consume failure: %w", handleErr)
	}

	return nil
}

func (r *Runtime) consumeClaim(ctx context.Context, work sourceobservation.ClaimWork) error {
	txCtx, cancel := context.WithTimeout(ctx, r.Config.TransactionTimeout)
	defer cancel()

	if err := r.withDB(txCtx, func(ctx context.Context) error {
		claim := work.Claim(r.claim.ConsumerName)
		if err := r.claimer.EnsureClaimBudget(ctx, claim, r.Config.TransactionTimeout); err != nil {
			return fmt.Errorf("ensure claim budget: %w", err)
		}

		return r.consumer.ConsumeClaim(ctx, claim)
	}); err != nil {
		return fmt.Errorf("with DB: %w", err)
	}

	return nil
}
