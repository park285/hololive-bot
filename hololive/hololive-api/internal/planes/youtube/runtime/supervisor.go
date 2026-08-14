package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func (r *Runtime) runClaimLoop(ctx context.Context, errCh chan<- error) {
	defer func() { r.loopDone <- struct{}{} }()
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

func (r *Runtime) runProjectionLoop(ctx context.Context, errCh chan<- error) {
	defer func() { r.loopDone <- struct{}{} }()
	ticker := time.NewTicker(r.Config.TargetProjection.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := r.refreshProjection(ctx)
			if err == nil {
				continue
			}
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return
			}
			if isInputReadError(err) || retryableObservationError(err) {
				r.Logger.Error("youtube plane projection refresh failed", slog.Any("error", err))
				continue
			}
			r.reportLoopError(ctx, errCh, "refresh target projection", err)
			return
		}
	}
}

func (r *Runtime) runWorker(ctx context.Context, errCh chan<- error) {
	defer func() { r.workerDone <- struct{}{} }()
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case observation, ok := <-r.workCh:
			if !ok {
				return
			}
			if err := r.processObservation(ctx, observation); err != nil {
				r.reportLoopError(ctx, errCh, "consume community observation", err)
				return
			}
		}
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

func (r *Runtime) processObservation(ctx context.Context, observation sourceobservation.Observation) error {
	r.remember(observation)
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
		r.forget(observation.ID)
		return nil
	}
	if errors.Is(err, sourceobservation.ErrClaimLost) {
		youtubeClaimLostTotal.Inc()
		r.forget(observation.ID)
		return nil
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	r.Logger.Error("youtube plane consume failed",
		slog.Int64("observation_id", observation.ID),
		slog.String("observation_kind", string(observation.ObservationKind)),
		slog.String("subject_key", observation.SubjectKey),
		slog.Any("error", err),
	)
	if !retryableObservationError(err) {
		r.forget(observation.ID)
		return err
	}
	retryErr := r.retryObservation(ctx, observation, err)
	if errors.Is(retryErr, sourceobservation.ErrClaimLost) {
		youtubeClaimLostTotal.Inc()
		retryErr = nil
	}
	r.forget(observation.ID)
	if retryErr != nil {
		return fmt.Errorf("retry observation %d: %w", observation.ID, retryErr)
	}
	return nil
}

func (r *Runtime) refreshProjection(ctx context.Context) error {
	var refreshErr error
	err := r.withDB(ctx, func(ctx context.Context) error {
		_, refreshErr = r.refresher.Refresh(ctx, r.builder, r.now())
		return refreshErr
	})
	if err == nil {
		r.degraded.Store(false)
		if r.started.Load() {
			r.publishHealth()
		}
		return nil
	}
	r.degraded.Store(true)
	if r.started.Load() {
		r.publishHealth()
	}
	return err
}

func (r *Runtime) remember(observation sourceobservation.Observation) {
	r.inFlight.Store(observation.ID, observation)
}

func (r *Runtime) forget(id int64) {
	r.inFlight.Delete(id)
}

func (r *Runtime) releaseInFlight(ctx context.Context) error {
	var releaseErrors []error
	r.inFlight.Range(func(key, value any) bool {
		observation, ok := value.(sourceobservation.Observation)
		if !ok {
			releaseErrors = append(releaseErrors, fmt.Errorf("release youtube observation: invalid in-flight value for %v", key))
			return true
		}
		err := r.retryObservation(ctx, observation, errors.New("youtube plane shutting down"))
		if errors.Is(err, sourceobservation.ErrClaimLost) {
			youtubeClaimLostTotal.Inc()
			err = nil
		}
		if err != nil {
			releaseErrors = append(releaseErrors, fmt.Errorf("release observation %d: %w", observation.ID, err))
			return ctx.Err() == nil
		}
		r.inFlight.Delete(key)
		return true
	})
	return errors.Join(releaseErrors...)
}

func (r *Runtime) retryObservation(ctx context.Context, observation sourceobservation.Observation, cause error) error {
	if r.Config.TransactionTimeout <= 0 {
		return errors.New("youtube plane transaction timeout must be positive")
	}
	retryCtx, cancel := context.WithTimeout(ctx, r.Config.TransactionTimeout)
	defer cancel()
	return r.withDB(retryCtx, func(ctx context.Context) error {
		status, err := r.claimer.Retry(ctx, sourceobservation.RetryInput{
			ObservationID: observation.ID,
			LeaseToken:    observation.LeaseToken,
			Delay:         r.Config.ClaimInterval,
			ErrorCode:     "youtube_plane_retry",
			ErrorDetail:   boundedError(cause),
		})
		if err != nil {
			return err
		}
		switch status {
		case contract.StatusPending:
			return nil
		case contract.StatusDeadLetter:
			r.degraded.Store(true)
			if r.started.Load() {
				r.publishHealth()
			}
			return nil
		default:
			return fmt.Errorf("retry observation %d: invalid status %q", observation.ID, status)
		}
	})
}

func (r *Runtime) reportLoopError(ctx context.Context, errCh chan<- error, action string, err error) {
	r.ready.Store(false)
	r.publishHealth()
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

func retryableObservationError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || pgconn.SafeToRetry(err) {
		return true
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "40001", "40P01", "55P03":
		return true
	default:
		return false
	}
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
