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

const attemptsExhaustedCode = "attempts_exhausted"

func attemptsExhausted(attemptCount int) bool {
	return attemptCount >= MaxAttempts
}

func claimDeadLetterCode(replayEpochRejected bool, attemptCount int) (string, bool) {
	if replayEpochRejected {
		return replayEpochExpiredCode, true
	}

	if attemptsExhausted(attemptCount) {
		return attemptsExhaustedCode, true
	}

	return "", false
}

func (r *Repository) Retry(ctx context.Context, input RetryInput) (contract.Status, error) {
	if err := r.validate(); err != nil {
		return "", fmt.Errorf("validate: %w", err)
	}

	if err := validateRetryInput(input); err != nil {
		return "", fmt.Errorf("validate retry input: %w", err)
	}

	query := retrySQL
	target := contract.StatusPending
	args := []any{input.ObservationID, input.LeaseToken, input.Delay.Milliseconds(), input.ErrorCode, input.ErrorDetail, input.AttemptCount}

	if attemptsExhausted(input.AttemptCount) {
		query = repositoryRetryExhaustedSQL
		target = contract.StatusDeadLetter
		args = []any{input.ObservationID, input.LeaseToken, input.ErrorDetail, input.AttemptCount, attemptsExhaustedCode}
	}

	var observationID int64

	err := r.pool.QueryRow(ctx, query, args...).Scan(&observationID)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrClaimLost
	}

	if err != nil {
		return "", fmt.Errorf("retry source observation: %w", err)
	}

	return target, nil
}

func validateRetryInput(input RetryInput) error {
	if input.ObservationID <= 0 || !lowercaseHexToken(input.LeaseToken) {
		return errors.New("validate source observation retry: invalid observation id or lease token")
	}

	if input.AttemptCount <= 0 || input.AttemptCount > MaxAttempts {
		return errors.New("validate source observation retry: invalid claimed attempt count")
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

	if err := persistDeadLetter(ctx, r.pool, input); err != nil {
		return fmt.Errorf("persist dead letter: %w", err)
	}

	return nil
}

func persistDeadLetter(ctx context.Context, db dbx.Querier, input DeadLetterInput) error {
	if input.ObservationID <= 0 || !lowercaseHexToken(input.LeaseToken) {
		return errors.New("validate source observation dead letter: invalid observation id or lease token")
	}

	if err := validateErrorFields("dead letter", input.ErrorCode, input.ErrorDetail); err != nil {
		return fmt.Errorf("validate error fields: %w", err)
	}

	var observationID int64

	err := db.QueryRow(
		ctx,
		persistDeadLetterSQL,
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
