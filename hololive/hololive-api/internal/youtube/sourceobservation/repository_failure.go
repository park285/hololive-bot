package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func (r *Repository) Retry(ctx context.Context, input RetryInput) (contract.Status, error) {
	if err := r.validate(); err != nil {
		return "", fmt.Errorf("validate: %w", err)
	}

	if err := validateRetryInput(input); err != nil {
		return "", fmt.Errorf("validate retry input: %w", err)
	}

	var status string

	err := r.pool.QueryRow(
		ctx,
		mustSQL("repository_retry_0017_17.sql"),
		input.ObservationID,
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

	persisted := contract.Status(status)
	if persisted != contract.StatusPending && persisted != contract.StatusDeadLetter {
		return "", fmt.Errorf("retry source observation: invalid persisted status %q", status)
	}

	return persisted, nil
}

func validateRetryInput(input RetryInput) error {
	if input.ObservationID <= 0 || !lowercaseHexToken(input.LeaseToken) {
		return errors.New("validate source observation retry: invalid observation id or lease token")
	}

	if input.Delay < 0 || input.Delay > 24*time.Hour {
		return errors.New("validate source observation retry: delay is outside the accepted range")
	}

	if err := validateErrorFields("retry", input.ErrorCode, input.ErrorDetail); err != nil {
		return fmt.Errorf("validate error fields: %w", err)
	}

	return nil
}

func (r *Repository) DeadLetter(ctx context.Context, input DeadLetterInput) error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if err := dbx.InPgxTx(ctx, r.pool, func(tx dbx.Tx) error {
		return deadLetterTx(ctx, tx, input)
	}); err != nil {
		return fmt.Errorf("in pgx tx: %w", err)
	}

	return nil
}

func deadLetterTx(ctx context.Context, tx dbx.Tx, input DeadLetterInput) error {
	if input.ObservationID <= 0 || !lowercaseHexToken(input.LeaseToken) {
		return errors.New("validate source observation dead letter: invalid observation id or lease token")
	}

	if err := validateErrorFields("dead letter", input.ErrorCode, input.ErrorDetail); err != nil {
		return fmt.Errorf("validate error fields: %w", err)
	}

	var observationID int64

	err := tx.QueryRow(
		ctx,
		mustSQL("repository_dead_letter_0018_18.sql"),
		input.ObservationID,
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
