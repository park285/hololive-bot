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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	replyOutboxInsertSQL       = mustSQL("reply_outbox_insert.sql")
	replyOutboxClaimSQL        = mustSQL("reply_outbox_claim.sql")
	replyOutboxMarkAcceptedSQL = mustSQL("reply_outbox_mark_accepted.sql")
	replyOutboxSettleSQL       = mustSQL("reply_outbox_settle.sql")
)

const (
	ReplyOutboxHandoffCompleted     = "handoff_completed"
	ReplyOutboxRetryablePreDispatch = "retryable_pre_dispatch"
	ReplyOutboxOutcomeUnknown       = "outcome_unknown"
	ReplyOutboxDead                 = "dead"
	ReplyOutboxPermanentConflict    = "permanent_conflict"
	ReplyOutboxManualReview         = "manual_review"
)

var replyOutboxSettleStatuses = map[string]struct{}{
	ReplyOutboxHandoffCompleted:     {},
	ReplyOutboxRetryablePreDispatch: {},
	ReplyOutboxOutcomeUnknown:       {},
	ReplyOutboxDead:                 {},
	ReplyOutboxPermanentConflict:    {},
	ReplyOutboxManualReview:         {},
}

type ReplyOutboxEntry struct {
	MessageID       string
	Phase           string
	Ordinal         uint64
	RoomID          string
	Payload         []byte
	ClientRequestID string
}

type ReplyOutboxClaim struct {
	ID              int64
	MessageID       string
	Phase           string
	Ordinal         int64
	RoomID          string
	Payload         []byte
	ClientRequestID string
	Attempts        int32
}

type ReplyOutboxSettlement struct {
	ID         int64
	ClaimToken string
	Status     string
	LastError  string
}

type ReplyOutboxRepository struct {
	pool *pgxpool.Pool
}

func NewReplyOutboxRepository(pool *pgxpool.Pool) *ReplyOutboxRepository {
	return &ReplyOutboxRepository{pool: pool}
}

func (r *ReplyOutboxRepository) Insert(ctx context.Context, entry *ReplyOutboxEntry) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	if entry == nil {
		return false, errors.Join(ErrInvalidArgument, errors.New("entry must not be nil"))
	}

	normalized, err := normalizeReplyOutboxEntry(entry)
	if err != nil {
		return false, err
	}

	digest := sha256.Sum256(normalized.Payload)
	tag, err := r.pool.Exec(ctx, replyOutboxInsertSQL,
		normalized.MessageID,
		normalized.Phase,
		normalized.Ordinal,
		normalized.RoomID,
		normalized.Payload,
		hex.EncodeToString(digest[:]),
		normalized.ClientRequestID,
	)
	if err != nil {
		return false, fmt.Errorf("insert reply outbox row %q: %w", normalized.ClientRequestID, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *ReplyOutboxRepository) Claim(ctx context.Context, claimToken string, lease time.Duration) (*ReplyOutboxClaim, error) {
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

	var claim ReplyOutboxClaim
	row := r.pool.QueryRow(ctx, replyOutboxClaimSQL, token, leaseMS)
	err = row.Scan(&claim.ID, &claim.MessageID, &claim.Phase, &claim.Ordinal,
		&claim.RoomID, &claim.Payload, &claim.ClientRequestID, &claim.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim reply outbox row: %w", err)
	}

	return &claim, nil
}

func (r *ReplyOutboxRepository) MarkAccepted(ctx context.Context, id int64, claimToken, irisRequestID string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	token, err := requireIdentity("claim token", claimToken)
	if err != nil {
		return false, err
	}
	requestID, err := requireIdentity("iris request id", irisRequestID)
	if err != nil {
		return false, err
	}

	tag, err := r.pool.Exec(ctx, replyOutboxMarkAcceptedSQL, id, token, requestID)
	if err != nil {
		return false, fmt.Errorf("mark reply outbox row %d accepted: %w", id, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *ReplyOutboxRepository) Settle(ctx context.Context, settlement ReplyOutboxSettlement) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	token, err := requireIdentity("claim token", settlement.ClaimToken)
	if err != nil {
		return false, err
	}
	if _, ok := replyOutboxSettleStatuses[settlement.Status]; !ok {
		return false, errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported reply outbox settle status %q", settlement.Status))
	}

	tag, err := r.pool.Exec(ctx, replyOutboxSettleSQL, settlement.ID, token, settlement.Status, settlement.LastError)
	if err != nil {
		return false, fmt.Errorf("settle reply outbox row %d: %w", settlement.ID, err)
	}

	return tag.RowsAffected() == 1, nil
}

func normalizeReplyOutboxEntry(entry *ReplyOutboxEntry) (ReplyOutboxEntry, error) {
	messageID, err := requireIdentity("message id", entry.MessageID)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}
	phase, err := requireIdentity("phase", entry.Phase)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}
	roomID, err := requireIdentity("room id", entry.RoomID)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}
	clientRequestID, err := requireIdentity("client request id", entry.ClientRequestID)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}
	if len(entry.Payload) == 0 {
		return ReplyOutboxEntry{}, errors.Join(ErrInvalidArgument, errors.New("payload must not be empty"))
	}

	return ReplyOutboxEntry{
		MessageID:       messageID,
		Phase:           phase,
		Ordinal:         entry.Ordinal,
		RoomID:          roomID,
		Payload:         entry.Payload,
		ClientRequestID: clientRequestID,
	}, nil
}
