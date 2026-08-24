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
)

func validateInboxRelease(maxAttempts int32, retryAfter time.Duration) (int64, error) {
	if maxAttempts <= 0 {
		return 0, errors.Join(ErrInvalidArgument, errors.New("max attempts must be positive"))
	}

	out, err := leaseMilliseconds(retryAfter)
	if err != nil {
		return out, fmt.Errorf("lease milliseconds: %w", err)
	}

	return out, nil
}

func (r *InboxRepository) Abandon(ctx context.Context, messageID, claimToken, reason string) (applied bool, err error) {
	if poolErr := ensurePool(r.pool); poolErr != nil {
		return false, fmt.Errorf("ensure pool: %w", poolErr)
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		if safeErr := safeMessageRepositoryError("begin webhook abandon", id, err); safeErr != nil {
			return false, fmt.Errorf("safe message repository error: %w", safeErr)
		}

		return false, nil
	}

	terminalReason, err := requireIdentity("terminal reason", reason)
	if err != nil {
		return false, fmt.Errorf("require identity: %w", err)
	}

	tx, err := r.beginLockedMessageTx(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("begin locked message tx: %w", err)
	}

	defer func() { err = errors.Join(err, rollbackInboxTx(ctx, tx)) }()

	applied, err = abandonInboxTx(ctx, tx, id, token, terminalReason)
	if err != nil {
		return applied, fmt.Errorf("%w", err)
	}

	return applied, nil
}

func abandonInboxTx(ctx context.Context, tx pgx.Tx, id, token, terminalReason string) (applied bool, err error) {
	err = tx.QueryRow(ctx, inboxAbandonSQL, id, token,
		clampColumnText(terminalReason, terminalReasonByteLimit)).Scan(&applied)
	if err != nil {
		return false, fmt.Errorf("safe message repository error: %w", safeMessageRepositoryError("abandon webhook inbox row", id, err))
	}

	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("safe message repository error: %w", safeMessageRepositoryError("commit webhook abandon", id, err))
	}

	return applied, nil
}

func (r *InboxRepository) Heartbeat(ctx context.Context, messageID, claimToken string, lease time.Duration) (time.Time, bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return time.Time{}, false, fmt.Errorf("ensure pool: %w", err)
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("fence args: %w", err)
	}

	leaseMS, err := leaseMilliseconds(lease)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("lease milliseconds: %w", err)
	}

	var leaseUntil time.Time

	err = r.pool.QueryRow(ctx, inboxHeartbeatSQL, id, token, leaseMS).Scan(&leaseUntil)

	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}

	if err != nil {
		if safeErr := safeMessageRepositoryError("heartbeat webhook inbox row", id, err); safeErr != nil {
			return time.Time{}, false, fmt.Errorf("safe message repository error: %w", safeErr)
		}

		return time.Time{}, false, nil
	}

	return leaseUntil, true, nil
}
