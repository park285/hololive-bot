package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func (r *Repository) Retry(ctx context.Context, input RetryInput) (contract.Status, error) {
	if r == nil || r.pool == nil {
		return "", ErrInvalidRepository
	}
	if err := input.validate(); err != nil {
		return "", err
	}
	var status string
	err := r.pool.QueryRow(
		ctx,
		mustSQL("repository_retry_0009_09.sql"),
		input.ObservationID,
		input.SourceKind,
		input.LeaseToken,
		input.Delay.Milliseconds(),
		input.ErrorCode,
		input.ErrorDetail,
		MaxAttempts,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrClaimLost
	}
	if err != nil {
		return "", fmt.Errorf("retry source observation: %w", err)
	}
	persistedStatus := contract.Status(status)
	if persistedStatus != contract.StatusPending && persistedStatus != contract.StatusDeadLetter {
		return "", fmt.Errorf("retry source observation: invalid persisted status %q", status)
	}
	return persistedStatus, nil
}

func (r *Repository) DeadLetter(ctx context.Context, input DeadLetterInput) error {
	if r == nil || r.pool == nil {
		return ErrInvalidRepository
	}
	if err := input.validate(); err != nil {
		return err
	}
	var observationID int64
	err := r.pool.QueryRow(
		ctx,
		mustSQL("repository_dead_letter_0010_10.sql"),
		input.ObservationID,
		input.SourceKind,
		input.LeaseToken,
		input.ErrorCode,
		input.ErrorDetail,
	).Scan(&observationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrClaimLost
	}
	if err != nil {
		return fmt.Errorf("dead letter source observation: %w", err)
	}
	return nil
}
