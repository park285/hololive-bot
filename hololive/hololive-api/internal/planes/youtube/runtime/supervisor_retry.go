package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func (r *Runtime) remember(work sourceobservation.ClaimWork) {
	r.inFlight.Store(work.Key(), work)
}

func (r *Runtime) forget(work sourceobservation.ClaimWork) {
	r.inFlight.CompareAndDelete(work.Key(), work)
}

func (r *Runtime) releaseInFlight(ctx context.Context) error {
	var releaseErrors []error
	r.inFlight.Range(func(key, value any) bool {
		work, ok := value.(sourceobservation.ClaimWork)
		if !ok {
			releaseErrors = append(releaseErrors, fmt.Errorf("release youtube observation: invalid in-flight value for %v", key))
			return true
		}
		err := r.retryObservation(ctx, work, errors.New("youtube plane shutting down"))
		if errors.Is(err, sourceobservation.ErrClaimLost) {
			youtubeClaimLostTotal.Inc()
			err = nil
		}
		if err != nil {
			releaseErrors = append(releaseErrors, fmt.Errorf("release observation %d: %w", work.ObservationID, err))
			return ctx.Err() == nil
		}
		r.inFlight.CompareAndDelete(key, work)
		return true
	})
	return errors.Join(releaseErrors...)
}

func (r *Runtime) retryObservation(ctx context.Context, work sourceobservation.ClaimWork, cause error) error {
	if r.Config.TransactionTimeout <= 0 {
		return errors.New("youtube plane transaction timeout must be positive")
	}
	retryCtx, cancel := context.WithTimeout(ctx, r.Config.TransactionTimeout)
	defer cancel()
	return r.withDB(retryCtx, func(ctx context.Context) error {
		status, err := r.claimer.Retry(ctx, sourceobservation.RetryInput{
			ObservationID: work.ObservationID,
			LeaseToken:    work.LeaseToken,
			Delay:         r.Config.ClaimInterval,
			ErrorCode:     "youtube_plane_retry",
			ErrorDetail:   boundedError(cause),
		})
		if err != nil {
			return err
		}
		return r.applyRetryStatus(work.ObservationID, status)
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
