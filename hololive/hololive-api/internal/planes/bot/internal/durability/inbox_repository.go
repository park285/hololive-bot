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
	inboxAdmitSQL          = mustSQL("inbox_admit.sql")
	inboxClaimSQL          = mustSQL("inbox_claim.sql")
	inboxCompleteSQL       = mustSQL("inbox_complete.sql")
	inboxReleaseSQL        = mustSQL("inbox_release.sql")
	inboxAbandonSQL        = mustSQL("inbox_abandon.sql")
	inboxReclaimExpiredSQL = mustSQL("inbox_reclaim_expired.sql")
)

type InboxReleaseOutcome int

const (
	InboxReleaseNotOwned InboxReleaseOutcome = iota
	InboxReleaseRetried
	InboxReleaseAbandoned
)

func (o InboxReleaseOutcome) String() string {
	switch o {
	case InboxReleaseNotOwned:
		return "not_owned"
	case InboxReleaseRetried:
		return "retried"
	case InboxReleaseAbandoned:
		return "abandoned"
	default:
		return "unknown"
	}
}

type InboxMessage struct {
	MessageID   string
	RoomID      string
	OrderingKey string
	Payload     []byte
}

type InboxClaim struct {
	MessageID   string
	RoomID      string
	OrderingKey string
	Payload     []byte
	Attempts    int32
}

type InboxRepository struct {
	pool *pgxpool.Pool
}

func NewInboxRepository(pool *pgxpool.Pool) *InboxRepository {
	return &InboxRepository{pool: pool}
}

func (r *InboxRepository) Admit(ctx context.Context, msg InboxMessage) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	messageID, err := requireMessageIdentity(msg.MessageID)
	if err != nil {
		return false, err
	}
	roomID, err := requireRoomID(msg.RoomID)
	if err != nil {
		return false, err
	}
	orderingKey, err := requireBoundedIdentity("ordering key", msg.OrderingKey, orderingKeyRuneLimit)
	if err != nil {
		return false, err
	}
	if len(msg.Payload) == 0 {
		return false, errors.Join(ErrInvalidArgument, errors.New("payload must not be empty"))
	}

	tag, err := r.pool.Exec(ctx, inboxAdmitSQL, messageID, roomID, orderingKey, msg.Payload)
	if err != nil {
		return false, fmt.Errorf("admit webhook inbox row %q: %w", messageID, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *InboxRepository) Claim(ctx context.Context, claimToken string, lease time.Duration) (*InboxClaim, error) {
	if err := ensurePool(r.pool); err != nil {
		return nil, err
	}

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return nil, err
	}
	leaseMS, err := leaseMilliseconds(lease)
	if err != nil {
		return nil, err
	}

	var claim InboxClaim
	row := r.pool.QueryRow(ctx, inboxClaimSQL, token, leaseMS)
	err = row.Scan(&claim.MessageID, &claim.RoomID, &claim.OrderingKey, &claim.Payload, &claim.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim webhook inbox row: %w", err)
	}

	return &claim, nil
}

func (r *InboxRepository) Complete(ctx context.Context, messageID, claimToken string) (bool, error) {
	return r.settle(ctx, inboxCompleteSQL, messageID, claimToken)
}

func (r *InboxRepository) Release(
	ctx context.Context,
	messageID, claimToken string,
	maxAttempts int32,
	retryAfter time.Duration,
	lastError string,
) (InboxReleaseOutcome, error) {
	if err := ensurePool(r.pool); err != nil {
		return InboxReleaseNotOwned, err
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return InboxReleaseNotOwned, err
	}
	if maxAttempts <= 0 {
		return InboxReleaseNotOwned, errors.Join(ErrInvalidArgument, errors.New("max attempts must be positive"))
	}
	retryMS, err := leaseMilliseconds(retryAfter)
	if err != nil {
		return InboxReleaseNotOwned, err
	}

	var status string
	err = r.pool.QueryRow(ctx, inboxReleaseSQL, id, token, retryMS,
		clampColumnText(lastError, lastErrorByteLimit), maxAttempts).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return InboxReleaseNotOwned, nil
	}
	if err != nil {
		return InboxReleaseNotOwned, fmt.Errorf("release webhook inbox row %q: %w", id, err)
	}
	if status == inboxStatusDead {
		return InboxReleaseAbandoned, nil
	}

	return InboxReleaseRetried, nil
}

func (r *InboxRepository) Abandon(ctx context.Context, messageID, claimToken, reason string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return false, err
	}
	terminalReason, err := requireIdentity("terminal reason", reason)
	if err != nil {
		return false, err
	}

	tag, err := r.pool.Exec(ctx, inboxAbandonSQL, id, token,
		clampColumnText(terminalReason, terminalReasonByteLimit))
	if err != nil {
		return false, fmt.Errorf("abandon webhook inbox row %q: %w", id, err)
	}

	return tag.RowsAffected() == 1, nil
}

type InboxReclaim struct {
	Requeued  int64
	Abandoned int64
}

// 만료 lease를 되돌리면서 claim_token을 비우므로, lease를 잃은 워커의 Complete/Release는 token 대조에서
// 0 rows가 된다 — 전이 쿼리에 lease_until 술어를 따로 두지 않는 이유다.
func (r *InboxRepository) ReclaimExpired(ctx context.Context, maxAttempts, batchSize int32) (InboxReclaim, error) {
	if err := ensurePool(r.pool); err != nil {
		return InboxReclaim{}, err
	}
	if maxAttempts <= 0 {
		return InboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("max attempts must be positive"))
	}
	if batchSize <= 0 {
		return InboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	var reclaim InboxReclaim
	err := r.pool.QueryRow(ctx, inboxReclaimExpiredSQL, maxAttempts, batchSize).
		Scan(&reclaim.Requeued, &reclaim.Abandoned)
	if err != nil {
		return InboxReclaim{}, fmt.Errorf("reclaim expired webhook inbox leases: %w", err)
	}

	return reclaim, nil
}

func (r *InboxRepository) settle(ctx context.Context, query, messageID, claimToken string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return false, err
	}

	tag, err := r.pool.Exec(ctx, query, id, token)
	if err != nil {
		return false, fmt.Errorf("settle webhook inbox row %q: %w", id, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *InboxRepository) fenceArgs(messageID, claimToken string) (id, token string, err error) {
	id, err = requireMessageIdentity(messageID)
	if err != nil {
		return "", "", err
	}
	token, err = requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return "", "", err
	}

	return id, token, nil
}
