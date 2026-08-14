package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func (r *Runtime) runClaimLoop(ctx context.Context, errCh chan<- error) {
	defer r.loopWG.Done()
	ticker := time.NewTicker(r.Config.ClaimInterval)
	defer ticker.Stop()
	if r.stopAfterClaimError(ctx, errCh, r.claimTick(ctx)) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.stopAfterClaimError(ctx, errCh, r.claimTick(ctx)) {
				return
			}
		}
	}
}

func (r *Runtime) stopAfterClaimError(ctx context.Context, errCh chan<- error, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || !r.claiming.Load() {
		return true
	}
	if permanentObservationError(err) {
		r.reportLoopError(ctx, errCh, "claim community observations", err)
		return true
	}
	r.Logger.Error("youtube plane claim tick failed", slog.Any("error", err))
	return false
}

func (r *Runtime) runProjectionLoop(ctx context.Context, errCh chan<- error) {
	defer r.loopWG.Done()
	ticker := time.NewTicker(r.Config.TargetProjection.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.refreshProjection(ctx); err != nil && !errors.Is(err, context.Canceled) {
				if !isInputReadError(err) && permanentObservationError(err) {
					r.reportLoopError(ctx, errCh, "refresh target projection", err)
					return
				}
				r.Logger.Error("youtube plane projection refresh failed", slog.Any("error", err))
			}
		}
	}
}

func (r *Runtime) runWorker() {
	defer r.workerWG.Done()
	for observation := range r.workCh {
		r.processObservation(context.Background(), observation)
	}
}

func (r *Runtime) claimTick(ctx context.Context) error {
	if r == nil || !r.claiming.Load() {
		return nil
	}
	var batch sourceobservation.ClaimedBatch
	if err := r.withDB(ctx, func(ctx context.Context) error {
		var err error
		batch, err = r.claimer.ClaimBatch(ctx, r.claim)
		return err
	}); err != nil {
		return err
	}
	for i := range batch.Observations {
		r.remember(batch.Observations[i])
	}
	for i := range batch.Observations {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case r.workCh <- batch.Observations[i]:
		}
	}
	return nil
}

func (r *Runtime) processObservation(ctx context.Context, observation sourceobservation.Observation) {
	r.remember(observation)
	defer r.forget(observation.ID)
	txCtx, cancel := context.WithTimeout(ctx, r.Config.TransactionTimeout)
	defer cancel()
	err := r.withDB(txCtx, func(ctx context.Context) error {
		claim := sourceobservation.Claim{
			ConsumerName:  r.claim.ConsumerName,
			ObservationID: observation.ID,
			LeaseToken:    observation.LeaseToken,
		}
		if err := r.claimer.EnsureClaimBudget(ctx, claim, r.Config.TransactionTimeout); err != nil {
			return err
		}
		return r.consumer.ConsumeObservation(ctx, observation, r.claim.ConsumerName)
	})
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) {
		r.retryObservation(observation, err)
		return
	}
	r.Logger.Error("youtube plane consume failed",
		slog.Int64("observation_id", observation.ID),
		slog.String("observation_kind", string(observation.ObservationKind)),
		slog.String("subject_key", observation.SubjectKey),
		slog.Any("error", err),
	)
	r.retryObservation(observation, err)
}

func (r *Runtime) refreshProjection(ctx context.Context) error {
	var refreshErr error
	err := r.withDB(ctx, func(ctx context.Context) error {
		_, refreshErr = r.refresher.Refresh(ctx, r.builder, r.now())
		return refreshErr
	})
	if err == nil {
		r.degraded.Store(false)
		return nil
	}
	if isInputReadError(err) {
		r.degraded.Store(true)
		return err
	}
	return err
}

func (r *Runtime) remember(observation sourceobservation.Observation) {
	r.inFlight.Store(observation.ID, observation)
}

func (r *Runtime) forget(id int64) {
	r.inFlight.Delete(id)
}

func (r *Runtime) releaseInFlight() {
	r.inFlight.Range(func(key, value any) bool {
		observation, ok := value.(sourceobservation.Observation)
		if !ok {
			r.inFlight.Delete(key)
			return true
		}
		r.retryObservation(observation, errors.New("youtube plane shutting down"))
		r.inFlight.Delete(key)
		return true
	})
}

func (r *Runtime) retryObservation(observation sourceobservation.Observation, cause error) {
	timeout := r.Config.TransactionTimeout
	if timeout <= 0 {
		timeout = time.Second
	}
	retryCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = r.withDB(retryCtx, func(ctx context.Context) error {
		_, err := r.claimer.Retry(ctx, sourceobservation.RetryInput{
			ObservationID: observation.ID,
			LeaseToken:    observation.LeaseToken,
			Delay:         0,
			ErrorCode:     "youtube_plane_retry",
			ErrorDetail:   boundedError(cause),
		})
		return err
	})
}

func (r *Runtime) reportLoopError(ctx context.Context, errCh chan<- error, action string, err error) {
	r.ready.Store(false)
	wrapped := fmt.Errorf("%s: %w", action, err)
	if errCh == nil {
		return
	}
	select {
	case errCh <- wrapped:
	case <-ctx.Done():
	}
}

func isInputReadError(err error) bool {
	return errors.Is(err, targetprojection.ErrInputRead)
}

func permanentObservationError(err error) bool {
	return errors.Is(err, sourceobservation.ErrInvalidRepository)
}

func boundedError(err error) string {
	if err == nil {
		return "youtube plane retry"
	}
	text := err.Error()
	if len(text) > 2048 {
		return text[:2048]
	}
	return text
}
