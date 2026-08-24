package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func (r *Runtime) forgetLostClaim(err error, work sourceobservation.ClaimWork) bool {
	if !errors.Is(err, sourceobservation.ErrClaimLost) {
		return false
	}

	youtubeClaimLostTotal.Inc()
	youtubeConsumeTotal.WithLabelValues("claim_lost").Inc()
	r.forget(work)

	return true
}

func (r *Runtime) handleConsumeFailure(ctx context.Context, work sourceobservation.ClaimWork, err error) error {
	r.Logger.Error("youtube plane consume failed",
		slog.Int64("observation_id", work.ObservationID),
		slog.String("observation_kind", string(work.ObservationKind)),
		slog.String("subject_key", work.SubjectKey),
		slog.Any("error", err),
	)

	if retryableObservationError(err) {
		if retryAndErr := r.retryAndForget(ctx, work, err); retryAndErr != nil {
			return fmt.Errorf("retry and forget: %w", retryAndErr)
		}

		return nil
	}

	if deadErr := r.deadLetterAndForget(ctx, work, err); deadErr != nil {
		return fmt.Errorf("dead letter and forget: %w", deadErr)
	}

	return nil
}

func (r *Runtime) deadLetterAndForget(ctx context.Context, work sourceobservation.ClaimWork, cause error) error {
	deadLetterer, ok := r.claimer.(observationDeadLetterer)
	if !ok {
		youtubeConsumeTotal.WithLabelValues("fatal").Inc()
		r.forget(work)

		return cause
	}

	deadLetterErr := r.deadLetterObservation(ctx, deadLetterer, work, cause)
	r.forget(work)

	if errors.Is(deadLetterErr, sourceobservation.ErrClaimLost) {
		youtubeClaimLostTotal.Inc()
		youtubeConsumeTotal.WithLabelValues("claim_lost").Inc()

		return nil
	}

	if deadLetterErr != nil {
		youtubeConsumeTotal.WithLabelValues("dead_letter_error").Inc()

		return fmt.Errorf("dead letter observation %d: %w", work.ObservationID, deadLetterErr)
	}

	youtubeConsumeTotal.WithLabelValues("fatal").Inc()
	r.markDeadLettered()

	return nil
}

func (r *Runtime) retryAndForget(ctx context.Context, work sourceobservation.ClaimWork, cause error) error {
	retryErr := r.retryObservation(ctx, work, cause)
	if errors.Is(retryErr, sourceobservation.ErrClaimLost) {
		youtubeClaimLostTotal.Inc()
		youtubeConsumeTotal.WithLabelValues("claim_lost").Inc()

		retryErr = nil
	}

	r.forget(work)

	if retryErr != nil {
		youtubeConsumeTotal.WithLabelValues("retry_error").Inc()

		return fmt.Errorf("retry observation %d: %w", work.ObservationID, retryErr)
	}

	return nil
}

func (r *Runtime) refreshProjection(ctx context.Context) error {
	err := r.withDB(ctx, func(ctx context.Context) error {
		if _, refreshErr := r.refresher.Refresh(ctx, r.builder, r.now()); refreshErr != nil {
			return fmt.Errorf("refresh target projection: %w", refreshErr)
		}

		return nil
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

	return fmt.Errorf("with DB: %w", err)
}
