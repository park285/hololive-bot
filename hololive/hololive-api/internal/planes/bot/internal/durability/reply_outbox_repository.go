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
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	replyOutboxInsertSQL         = mustSQL("reply_outbox_insert.sql")
	replyOutboxConflictSQL       = mustSQL("reply_outbox_conflict.sql")
	replyOutboxClaimSQL          = mustSQL("reply_outbox_claim.sql")
	replyOutboxMarkAcceptedSQL   = mustSQL("reply_outbox_mark_accepted.sql")
	replyOutboxSettleSQL         = mustSQL("reply_outbox_settle.sql")
	replyOutboxReclaimExpiredSQL = mustSQL("reply_outbox_reclaim_expired.sql")
)

type ReplyOutboxInsertOutcome int

const (
	ReplyOutboxInserted ReplyOutboxInsertOutcome = iota
	ReplyOutboxAlreadyRecorded
	ReplyOutboxPayloadDiverged
)

func (o ReplyOutboxInsertOutcome) String() string {
	switch o {
	case ReplyOutboxInserted:
		return "inserted"
	case ReplyOutboxAlreadyRecorded:
		return "already_recorded"
	case ReplyOutboxPayloadDiverged:
		return "payload_diverged"
	default:
		return "unknown"
	}
}

const (
	replyOutboxStatusSubmitting = "submitting"
	replyOutboxStatusAccepted   = "accepted"
)

const (
	ReplyOutboxHandoffCompleted     = "handoff_completed"
	ReplyOutboxRetryablePreDispatch = "retryable_pre_dispatch"
	ReplyOutboxOutcomeUnknown       = "outcome_unknown"
	ReplyOutboxDead                 = "dead"
	ReplyOutboxPermanentConflict    = "permanent_conflict"
	ReplyOutboxManualReview         = "manual_review"
)

// accepted는 Iris가 이미 수리한 뒤라 그 행을 claim 큐로 되돌리면 admission idempotency TTL이 지난
// 시점에 중복 발화가 된다. accepted에서 갈 수 있는 곳은 재발송 불가한 종단뿐이고, 정산이 없으면
// reclaim_expired가 흡수한다.
var replyOutboxSettleSources = map[string][]string{
	ReplyOutboxHandoffCompleted:     {replyOutboxStatusSubmitting, replyOutboxStatusAccepted},
	ReplyOutboxRetryablePreDispatch: {replyOutboxStatusSubmitting},
	ReplyOutboxOutcomeUnknown:       {replyOutboxStatusSubmitting},
	ReplyOutboxDead:                 {replyOutboxStatusSubmitting, replyOutboxStatusAccepted},
	ReplyOutboxPermanentConflict:    {replyOutboxStatusSubmitting, replyOutboxStatusAccepted},
	ReplyOutboxManualReview:         {replyOutboxStatusSubmitting, replyOutboxStatusAccepted},
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
	Ordinal         uint64
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

func (r *ReplyOutboxRepository) Insert(ctx context.Context, entry *ReplyOutboxEntry) (ReplyOutboxInsertOutcome, error) {
	if err := ensurePool(r.pool); err != nil {
		return ReplyOutboxInserted, err
	}

	if entry == nil {
		return ReplyOutboxInserted, errors.Join(ErrInvalidArgument, errors.New("entry must not be nil"))
	}

	normalized, err := normalizeReplyOutboxEntry(entry)
	if err != nil {
		return ReplyOutboxInserted, err
	}

	digest := sha256.Sum256(normalized.Payload)
	payloadHash := hex.EncodeToString(digest[:])
	tag, err := r.pool.Exec(ctx, replyOutboxInsertSQL,
		normalized.MessageID,
		normalized.Phase,
		normalized.Ordinal,
		normalized.RoomID,
		normalized.Payload,
		payloadHash,
		normalized.ClientRequestID,
	)
	if err != nil {
		return ReplyOutboxInserted, fmt.Errorf("insert reply outbox row %q: %w", normalized.ClientRequestID, err)
	}
	if tag.RowsAffected() == 1 {
		return ReplyOutboxInserted, nil
	}

	return r.classifyRecordedPayload(ctx, &normalized, payloadHash)
}

// 재처리는 LLM 출력과 가변 상태에서 바이트를 다시 만들어 내므로 같은 슬롯에 같은 바이트가 온다는
// 보장이 없다. 불일치를 실패로 돌려주면 저장본 재발송까지 막혀 아무것도 전달되지 않으므로,
// 여기서는 관측 신호로만 강등하고 발송 권한은 계속 저장본이 갖는다.
func (r *ReplyOutboxRepository) classifyRecordedPayload(ctx context.Context, entry *ReplyOutboxEntry, payloadHash string) (ReplyOutboxInsertOutcome, error) {
	var recordedHash, recordedClientRequestID string
	err := r.pool.QueryRow(ctx, replyOutboxConflictSQL, entry.MessageID, entry.Phase, entry.Ordinal).
		Scan(&recordedHash, &recordedClientRequestID)
	if err != nil {
		return ReplyOutboxAlreadyRecorded,
			fmt.Errorf("inspect reply outbox row %q: %w", entry.ClientRequestID, err)
	}

	if recordedHash != payloadHash || recordedClientRequestID != entry.ClientRequestID {
		return ReplyOutboxPayloadDiverged, nil
	}

	return ReplyOutboxAlreadyRecorded, nil
}

func (r *ReplyOutboxRepository) Claim(ctx context.Context, claimToken string, lease time.Duration) (*ReplyOutboxClaim, error) {
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

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
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

	token, err := requireBoundedIdentity("claim token", settlement.ClaimToken, claimTokenRuneLimit)
	if err != nil {
		return false, err
	}
	sources, ok := replyOutboxSettleSources[settlement.Status]
	if !ok {
		return false, errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported reply outbox settle status %q", settlement.Status))
	}

	tag, err := r.pool.Exec(ctx, replyOutboxSettleSQL, settlement.ID, token, settlement.Status,
		clampColumnText(settlement.LastError, lastErrorByteLimit), sources)
	if err != nil {
		return false, fmt.Errorf("settle reply outbox row %d: %w", settlement.ID, err)
	}

	return tag.RowsAffected() == 1, nil
}

type ReplyOutboxReclaim struct {
	Requeued int64
	Absorbed int64
}

func (r *ReplyOutboxRepository) ReclaimExpired(ctx context.Context, batchSize int32) (ReplyOutboxReclaim, error) {
	if err := ensurePool(r.pool); err != nil {
		return ReplyOutboxReclaim{}, err
	}
	if batchSize <= 0 {
		return ReplyOutboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	var reclaim ReplyOutboxReclaim
	err := r.pool.QueryRow(ctx, replyOutboxReclaimExpiredSQL, batchSize).
		Scan(&reclaim.Requeued, &reclaim.Absorbed)
	if err != nil {
		return ReplyOutboxReclaim{}, fmt.Errorf("reclaim expired reply outbox leases: %w", err)
	}

	return reclaim, nil
}

func normalizeReplyOutboxEntry(entry *ReplyOutboxEntry) (ReplyOutboxEntry, error) {
	messageID, err := requireMessageIdentity(entry.MessageID)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}
	phase, err := requireBoundedIdentity("phase", entry.Phase, phaseRuneLimit)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}
	roomID, err := requireRoomID(entry.RoomID)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}
	clientRequestID, err := requireClientRequestID(entry.ClientRequestID)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}
	if entry.Ordinal > math.MaxInt64 {
		return ReplyOutboxEntry{}, errors.Join(ErrInvalidArgument,
			fmt.Errorf("ordinal %d exceeds the BIGINT ledger domain", entry.Ordinal))
	}
	if len(entry.Payload) == 0 {
		return ReplyOutboxEntry{}, errors.Join(ErrInvalidArgument, errors.New("payload must not be empty"))
	}

	return ReplyOutboxEntry{
		MessageID:       messageID,
		Phase:           phase,
		Ordinal:         entry.Ordinal,
		RoomID:          roomID,
		Payload:         slices.Clone(entry.Payload),
		ClientRequestID: clientRequestID,
	}, nil
}
