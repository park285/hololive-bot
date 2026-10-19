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

// attemptsExhausted는 재시도 정책의 소진 판정이다. 소진한 관측은 다시 선점하지 않고 DEAD_LETTER로 끝낸다.
func attemptsExhausted(attemptCount int) bool {
	return attemptCount >= MaxAttempts
}

// claimDeadLetterCode는 claim이 잠갔지만 선점하지 않은 due 행의 종료 사유를 정한다. Replay epoch 이전 관측은
// 시도 횟수와 무관하게 replay_epoch_expired이고, 그 밖에는 시도를 소진한 행만 attempts_exhausted로 끝낸다.
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

	// claim이 반환한 시도 횟수로 정책을 정하고 동일 횟수·token을 DB에서 다시 검증한다.
	// 추가 SELECT 없이 재시도와 소진 종료를 결정하며 DB 시각에 따른 lease 만료 검증은 유지한다.
	query := mustSQL("repository_retry_0017_17.sql")
	target := contract.StatusPending
	args := []any{input.ObservationID, input.LeaseToken, input.Delay.Milliseconds(), input.ErrorCode, input.ErrorDetail, input.AttemptCount}

	if attemptsExhausted(input.AttemptCount) {
		query = mustSQL("repository_retry_exhausted.sql")
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

	// 단일 fenced UPDATE이므로 별도 BEGIN/COMMIT 왕복 없이 같은 원자성을 얻는다.
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
