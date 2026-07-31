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
		return false, err
	}

	tag, err := r.pool.Exec(ctx, commandExecutionClaimSQL, id, kind, token)
	if err != nil {
		return false, safeMessageRepositoryError("claim command execution", id, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *CommandExecutionRepository) Complete(ctx context.Context, messageID, claimToken, status string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
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
	if status != CommandExecutionSucceeded && status != CommandExecutionFailed && status != CommandExecutionOutcomeUnknown {
		return false, errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported command execution status %q", status))
	}

	tag, err := r.pool.Exec(ctx, commandExecutionCompleteSQL, id, token, status)
	if err != nil {
		return false, safeMessageRepositoryError("complete command execution", id, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *CommandExecutionRepository) Heartbeat(ctx context.Context, messageID, claimToken string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
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
	tag, err := r.pool.Exec(ctx, commandExecutionHeartbeatSQL, id, token)
	if err != nil {
		return false, safeMessageRepositoryError("heartbeat command execution", id, err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *CommandExecutionRepository) State(ctx context.Context, messageID string) (*CommandExecutionState, error) {
	if err := ensurePool(r.pool); err != nil {
		return nil, err
	}
	id, err := requireMessageIdentity(messageID)
	if err != nil {
		return nil, err
	}
	var state CommandExecutionState
	err = r.pool.QueryRow(ctx, commandExecutionStateSQL, id).Scan(&state.Status, &state.ClaimedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, safeMessageRepositoryError("inspect command execution state", id, err)
	}
	return &state, nil
}

func (r *CommandExecutionRepository) ExpireStaleClaims(ctx context.Context, olderThan time.Duration, batchSize int32) (int64, error) {
	if err := ensurePool(r.pool); err != nil {
		return 0, err
	}

	ageMS, err := leaseMilliseconds(olderThan)
	if err != nil {
		return 0, err
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
