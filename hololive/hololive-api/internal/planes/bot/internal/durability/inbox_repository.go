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
	inboxAdmitSQL    = mustSQL("inbox_admit.sql")
	inboxClaimSQL    = mustSQL("inbox_claim.sql")
	inboxCompleteSQL = mustSQL("inbox_complete.sql")
	inboxReleaseSQL  = mustSQL("inbox_release.sql")
)

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

	messageID, err := requireIdentity("message id", msg.MessageID)
	if err != nil {
		return false, err
	}
	roomID, err := requireIdentity("room id", msg.RoomID)
	if err != nil {
		return false, err
	}
	orderingKey, err := requireIdentity("ordering key", msg.OrderingKey)
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

	token, err := requireIdentity("claim token", claimToken)
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

func (r *InboxRepository) Release(ctx context.Context, messageID, claimToken string, retryAfter time.Duration, lastError string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return false, err
	}
	retryMS, err := leaseMilliseconds(retryAfter)
	if err != nil {
		return false, err
	}

	tag, err := r.pool.Exec(ctx, inboxReleaseSQL, id, token, retryMS, lastError)
	if err != nil {
		return false, fmt.Errorf("release webhook inbox row %q: %w", id, err)
	}

	return tag.RowsAffected() == 1, nil
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
	id, err = requireIdentity("message id", messageID)
	if err != nil {
		return "", "", err
	}
	token, err = requireIdentity("claim token", claimToken)
	if err != nil {
		return "", "", err
	}

	return id, token, nil
}
