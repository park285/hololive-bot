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
	for waitTicker(ctx, ticker) {
		if r.stopAfterClaimError(ctx, errCh, r.claimTick(ctx)) {
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
	for i := 0; i < r.Config.ClaimBatchSize; i++ {
		var processed bool
		err := r.withDB(ctx, func(ctx context.Context) error {
			var tickErr error
			processed, tickErr = r.finalizer.FinalizeNextDueLiveEnd(ctx, r.Config.LiveEndGrace)
			return tickErr
		})
		if err != nil {
			return err
		}
		if !processed {
			return nil
		}
	}
	return nil
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
		observation, ok := r.nextWork(ctx)
		if !ok {
			return
		}
		if err := r.processObservation(ctx, observation); err != nil {
			r.reportLoopError(ctx, errCh, "consume community observation", err)
			return
		}
	}
}

func (r *Runtime) nextWork(ctx context.Context) (sourceobservation.Observation, bool) {
	if ctx.Err() != nil {
		return sourceobservation.Observation{}, false
	}
	select {
	case <-ctx.Done():
		return sourceobservation.Observation{}, false
	case observation, ok := <-r.workCh:
		return observation, ok
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
	r.observePendingQueue(ctx)
	for i := range batch.Observations {
		r.remember(batch.Observations[i])
	}
	for i := range batch.Observations {
		if err := sendWork(ctx, r.workCh, batch.Observations[i]); err != nil {
			return err
		}
	}
	return nil
}

func sendWork(ctx context.Context, workCh chan<- sourceobservation.Observation, observation sourceobservation.Observation) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case workCh <- observation:
		return nil
	}
}

func (r *Runtime) processObservation(ctx context.Context, observation sourceobservation.Observation) error {
	r.remember(observation)
	err := r.consumeObservation(ctx, observation)
	if err == nil {
		youtubeFinalizeTotal.Inc()
		youtubeConsumeTotal.WithLabelValues("success").Inc()
		r.forget(observation.ID)
		return nil
	}
	if r.forgetLostClaim(err, observation.ID) {
		return nil
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil
	}
	return r.handleConsumeFailure(ctx, observation, err)
}

func (r *Runtime) consumeObservation(ctx context.Context, observation sourceobservation.Observation) error {
	txCtx, cancel := context.WithTimeout(ctx, r.Config.TransactionTimeout)
	defer cancel()
	return r.withDB(txCtx, func(ctx context.Context) error {
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
}

func (r *Runtime) forgetLostClaim(err error, observationID int64) bool {
	if !errors.Is(err, sourceobservation.ErrClaimLost) {
		return false
	}
	youtubeClaimLostTotal.Inc()
	youtubeConsumeTotal.WithLabelValues("claim_lost").Inc()
	r.forget(observationID)
	return true
}

func (r *Runtime) handleConsumeFailure(ctx context.Context, observation sourceobservation.Observation, err error) error {
	r.Logger.Error("youtube plane consume failed",
		slog.Int64("observation_id", observation.ID),
		slog.String("observation_kind", string(observation.ObservationKind)),
		slog.String("subject_key", observation.SubjectKey),
		slog.Any("error", err),
	)
	if !retryableObservationError(err) {
		youtubeConsumeTotal.WithLabelValues("fatal").Inc()
		r.forget(observation.ID)
		return err
	}
	return r.retryAndForget(ctx, observation, err)
}

func (r *Runtime) retryAndForget(ctx context.Context, observation sourceobservation.Observation, cause error) error {
	retryErr := r.retryObservation(ctx, observation, cause)
	if errors.Is(retryErr, sourceobservation.ErrClaimLost) {
		youtubeClaimLostTotal.Inc()
		youtubeConsumeTotal.WithLabelValues("claim_lost").Inc()
		retryErr = nil
	}
	r.forget(observation.ID)
	if retryErr != nil {
		youtubeConsumeTotal.WithLabelValues("retry_error").Inc()
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
		return r.applyRetryStatus(observation.ID, status)
	})
}

func (r *Runtime) applyRetryStatus(observationID int64, status contract.Status) error {
	if status == contract.StatusPending {
		youtubeConsumeTotal.WithLabelValues("retry").Inc()
		return nil
	}
	if status == contract.StatusDeadLetter {
		youtubeConsumeTotal.WithLabelValues("dead_letter").Inc()
		r.degraded.Store(true)
		if r.started.Load() {
			r.publishHealth()
		}
		return nil
	}
	return fmt.Errorf("retry observation %d: invalid status %q", observationID, status)
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
