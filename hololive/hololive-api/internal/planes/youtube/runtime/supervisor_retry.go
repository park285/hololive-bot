package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

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

	if err := r.withDB(retryCtx, func(ctx context.Context) error {
		status, err := r.claimer.Retry(ctx, sourceobservation.RetryInput{
			ObservationID: work.ObservationID,
			LeaseToken:    work.LeaseToken,
			Delay:         r.Config.ClaimInterval,
			ErrorCode:     "youtube_plane_retry",
			ErrorDetail:   boundedError(cause),
		})
		if err != nil {
			return fmt.Errorf("retry: %w", err)
		}

		return r.applyRetryStatus(work.ObservationID, status)
	}); err != nil {
		return fmt.Errorf("with DB: %w", err)
	}

	return nil
}

func (r *Runtime) applyRetryStatus(observationID int64, status contract.Status) error {
	if status == contract.StatusPending {
		youtubeConsumeTotal.WithLabelValues("retry").Inc()

		return nil
	}

	if status == contract.StatusDeadLetter {
		youtubeConsumeTotal.WithLabelValues("dead_letter").Inc()
		r.markDeadLettered()

		return nil
	}

	return fmt.Errorf("retry observation %d: invalid status %q", observationID, status)
}

func (r *Runtime) markDeadLettered() {
	r.degraded.Store(true)

	if r.started.Load() {
		r.publishHealth()
	}
}

type observationDeadLetterer interface {
	DeadLetter(context.Context, sourceobservation.DeadLetterInput) error
}

var _ observationDeadLetterer = (*sourceobservation.ConsumeRepository)(nil)

func (r *Runtime) deadLetterObservation(ctx context.Context, deadLetterer observationDeadLetterer, work sourceobservation.ClaimWork, cause error) error {
	if r.Config.TransactionTimeout <= 0 {
		return errors.New("youtube plane transaction timeout must be positive")
	}

	deadLetterCtx, cancel := context.WithTimeout(ctx, r.Config.TransactionTimeout)

	defer cancel()

	if err := r.withDB(deadLetterCtx, func(ctx context.Context) error {
		return deadLetterer.DeadLetter(ctx, sourceobservation.DeadLetterInput{
			ObservationID: work.ObservationID,
			LeaseToken:    work.LeaseToken,
			ErrorCode:     "youtube_plane_fatal",
			ErrorDetail:   boundedError(cause),
		})
	}); err != nil {
		return fmt.Errorf("with DB: %w", err)
	}

	return nil
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
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) || pgconn.SafeToRetry(err) {
		return true
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, pgconn.ErrConnClosed) {
		return true
	}

	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return false
	}

	return transientSQLState(pgErr.Code)
}

func transientSQLState(code string) bool {
	if strings.HasPrefix(code, "08") {
		return true
	}

	switch code {
	case "40001", "40P01", "55P03", "53300", "57014", "57P01", "57P02", "57P03":
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
