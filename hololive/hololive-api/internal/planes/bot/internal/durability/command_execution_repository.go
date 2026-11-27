package durability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	CommandExecutionClaimed        = "claimed"
	CommandExecutionSucceeded      = "succeeded"
	CommandExecutionFailed         = "failed"
	CommandExecutionOutcomeUnknown = "outcome_unknown"
)

type CommandExecutionRepository struct {
	store *Store
}

type CommandExecutionState struct {
	Status    string
	ClaimedAt time.Time
}

func NewCommandExecutionRepository(store *Store) *CommandExecutionRepository {
	return &CommandExecutionRepository{store: store}
}

func (r *CommandExecutionRepository) Claim(ctx context.Context, messageID, commandKind, claimToken string) (bool, error) {
	if err := ensureStore(r.store); err != nil {
		return false, err
	}

	id, err := requireMessageIdentity(messageID)
	if err != nil {
		return false, err
	}

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return false, err
	}

	kind, err := requireBoundedCommandKind(commandKind)
	if err != nil {
		return false, fmt.Errorf("require bounded command kind: %w", err)
	}

	claimed, err := r.exec(ctx, commandExecutionClaimSQL, id, kind, token)
	if err != nil {
		return false, safeMessageRepositoryError("claim command execution", id, err)
	}

	return claimed == 1, nil
}

func (r *CommandExecutionRepository) Complete(ctx context.Context, messageID, claimToken, status string) (bool, error) {
	if err := ensureStore(r.store); err != nil {
		return false, err
	}

	id, err := requireMessageIdentity(messageID)
	if err != nil {
		return false, err
	}

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return false, err
	}

	if statusErr := requireCommandExecutionStatus(status); statusErr != nil {
		return false, fmt.Errorf("%w", statusErr)
	}

	completed, err := r.exec(ctx, commandExecutionCompleteSQL, id, token, status)
	if err != nil {
		return false, safeMessageRepositoryError("complete command execution", id, err)
	}

	return completed == 1, nil
}

func requireCommandExecutionStatus(status string) error {
	if status == CommandExecutionSucceeded || status == CommandExecutionFailed || status == CommandExecutionOutcomeUnknown {
		return nil
	}

	return errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported command execution status %q", status))
}

func (r *CommandExecutionRepository) Heartbeat(ctx context.Context, messageID, claimToken string) (bool, error) {
	if err := ensureStore(r.store); err != nil {
		return false, err
	}

	id, err := requireMessageIdentity(messageID)
	if err != nil {
		return false, err
	}

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return false, err
	}

	renewed, err := r.exec(ctx, commandExecutionHeartbeatSQL, id, token)
	if err != nil {
		return false, safeMessageRepositoryError("heartbeat command execution", id, err)
	}

	return renewed == 1, nil
}

func (r *CommandExecutionRepository) State(ctx context.Context, messageID string) (*CommandExecutionState, error) {
	if err := ensureStore(r.store); err != nil {
		return nil, err
	}

	id, err := requireMessageIdentity(messageID)
	if err != nil {
		return nil, err
	}

	var (
		state       CommandExecutionState
		claimedAtMS int64
	)

	err = r.store.read().QueryRowContext(ctx, commandExecutionStateSQL, id).Scan(&state.Status, &claimedAtMS)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // 행 없음(no row)을 알리는 계약값이다. runtime 폴링 루프가 nil 결과로 유휴를 판정하므로 sentinel 오류로 바꾸려면 이 패키지 밖 호출자를 함께 고쳐야 한다.
	}

	if err != nil {
		return nil, safeMessageRepositoryError("inspect command execution state", id, err)
	}

	state.ClaimedAt = timeFromMS(claimedAtMS)

	return &state, nil
}

func (r *CommandExecutionRepository) ExpireStaleClaims(ctx context.Context, olderThan time.Duration, batchSize int32) (int64, error) {
	if err := ensureStore(r.store); err != nil {
		return 0, err
	}

	ageMS, err := leaseMilliseconds(olderThan)
	if err != nil {
		return 0, err
	}

	if batchSize <= 0 {
		return 0, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	var expired int64

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		result, execErr := tx.ExecContext(ctx, commandExecutionExpireStaleSQL, now, ageMS, batchSize)
		if execErr != nil {
			return fmt.Errorf("expire stale command claims: %w", execErr)
		}

		expired, execErr = rowsAffected(result)

		return execErr
	})
	if err != nil {
		return 0, fmt.Errorf("expire stale command execution claims: %w", err)
	}

	return expired, nil
}

func (r *CommandExecutionRepository) exec(ctx context.Context, query string, args ...any) (int64, error) {
	var affected int64

	err := r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		result, execErr := tx.ExecContext(ctx, query, append(args, now)...)
		if execErr != nil {
			return fmt.Errorf("execute command execution update: %w", execErr)
		}

		affected, execErr = rowsAffected(result)

		return execErr
	})

	return affected, err
}
