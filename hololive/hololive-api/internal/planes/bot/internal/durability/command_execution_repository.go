// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package durability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	commandExecutionClaimSQL       = mustSQL("command_execution_claim.sql")
	commandExecutionCompleteSQL    = mustSQL("command_execution_complete.sql")
	commandExecutionExpireStaleSQL = mustSQL("command_execution_expire_stale.sql")
	commandExecutionHeartbeatSQL   = mustSQL("command_execution_heartbeat.sql")
	commandExecutionStateSQL       = mustSQL("command_execution_state.sql")
)

const (
	CommandExecutionClaimed        = "claimed"
	CommandExecutionSucceeded      = "succeeded"
	CommandExecutionFailed         = "failed"
	CommandExecutionOutcomeUnknown = "outcome_unknown"
)

type CommandExecutionRepository struct {
	pool *pgxpool.Pool
}

type CommandExecutionState struct {
	Status    string
	ClaimedAt time.Time
}

func NewCommandExecutionRepository(pool *pgxpool.Pool) *CommandExecutionRepository {
	return &CommandExecutionRepository{pool: pool}
}

func (r *CommandExecutionRepository) Claim(ctx context.Context, messageID, commandKind, claimToken string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, fmt.Errorf("ensure pool: %w", err)
	}

	id, err := requireMessageIdentity(messageID)
	if err != nil {
		return false, fmt.Errorf("require message identity: %w", err)
	}

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return false, fmt.Errorf("require bounded identity: %w", err)
	}

	kind, err := requireBoundedCommandKind(commandKind)
	if err != nil {
		return false, fmt.Errorf("require bounded command kind: %w", err)
	}

	tag, err := r.pool.Exec(ctx, commandExecutionClaimSQL, id, kind, token)
	if err != nil {
		if safeErr := safeMessageRepositoryError("claim command execution", id, err); safeErr != nil {
			return false, fmt.Errorf("safe message repository error: %w", safeErr)
		}

		return false, nil
	}

	return tag.RowsAffected() == 1, nil
}

func (r *CommandExecutionRepository) Complete(ctx context.Context, messageID, claimToken, status string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, fmt.Errorf("ensure pool: %w", err)
	}

	id, err := requireMessageIdentity(messageID)
	if err != nil {
		return false, fmt.Errorf("require message identity: %w", err)
	}

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return false, fmt.Errorf("require bounded identity: %w", err)
	}

	if statusErr := requireCommandExecutionStatus(status); statusErr != nil {
		return false, fmt.Errorf("%w", statusErr)
	}

	tag, err := r.pool.Exec(ctx, commandExecutionCompleteSQL, id, token, status)
	if err != nil {
		return false, fmt.Errorf("safe message repository error: %w", safeMessageRepositoryError("complete command execution", id, err))
	}

	return tag.RowsAffected() == 1, nil
}

func requireCommandExecutionStatus(status string) error {
	if status == CommandExecutionSucceeded || status == CommandExecutionFailed || status == CommandExecutionOutcomeUnknown {
		return nil
	}

	return errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported command execution status %q", status))
}

func (r *CommandExecutionRepository) Heartbeat(ctx context.Context, messageID, claimToken string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, fmt.Errorf("ensure pool: %w", err)
	}

	id, err := requireMessageIdentity(messageID)
	if err != nil {
		return false, fmt.Errorf("require message identity: %w", err)
	}

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return false, fmt.Errorf("require bounded identity: %w", err)
	}

	tag, err := r.pool.Exec(ctx, commandExecutionHeartbeatSQL, id, token)
	if err != nil {
		if safeErr := safeMessageRepositoryError("heartbeat command execution", id, err); safeErr != nil {
			return false, fmt.Errorf("safe message repository error: %w", safeErr)
		}

		return false, nil
	}

	return tag.RowsAffected() == 1, nil
}

func (r *CommandExecutionRepository) State(ctx context.Context, messageID string) (*CommandExecutionState, error) {
	if err := ensurePool(r.pool); err != nil {
		return nil, fmt.Errorf("ensure pool: %w", err)
	}

	id, err := requireMessageIdentity(messageID)
	if err != nil {
		return nil, fmt.Errorf("require message identity: %w", err)
	}

	var state CommandExecutionState

	err = r.pool.QueryRow(ctx, commandExecutionStateSQL, id).Scan(&state.Status, &state.ClaimedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // 행 없음(no row)을 알리는 계약값이다. runtime 폴링 루프가 nil 결과로 유휴를 판정하므로 sentinel 오류로 바꾸려면 이 패키지 밖 호출자를 함께 고쳐야 한다.
	}

	if err != nil {
		if safeErr := safeMessageRepositoryError("inspect command execution state", id, err); safeErr != nil {
			return nil, fmt.Errorf("safe message repository error: %w", safeErr)
		}

		return nil, nil //nolint:nilnil // safe*Error가 오류를 삼킨 경우도 위 ErrNoRows 분기와 같은 "행 없음"으로 접는다.
	}

	return &state, nil
}

func (r *CommandExecutionRepository) ExpireStaleClaims(ctx context.Context, olderThan time.Duration, batchSize int32) (int64, error) {
	if err := ensurePool(r.pool); err != nil {
		return 0, fmt.Errorf("ensure pool: %w", err)
	}

	ageMS, err := leaseMilliseconds(olderThan)
	if err != nil {
		return 0, fmt.Errorf("lease milliseconds: %w", err)
	}

	if batchSize <= 0 {
		return 0, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	tag, err := r.pool.Exec(ctx, commandExecutionExpireStaleSQL, ageMS, batchSize)
	if err != nil {
		return 0, fmt.Errorf("expire stale command execution claims: %w", err)
	}

	return tag.RowsAffected(), nil
}
