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
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	replyOutboxInsertSQL             = mustSQL("reply_outbox_insert.sql")
	replyOutboxConflictSQL           = mustSQL("reply_outbox_conflict.sql")
	replyOutboxClaimSQL              = mustSQL("reply_outbox_claim.sql")
	replyOutboxMarkAcceptedSQL       = mustSQL("reply_outbox_mark_accepted.sql")
	replyOutboxSettleSQL             = mustSQL("reply_outbox_settle.sql")
	replyOutboxReclaimExpiredSQL     = mustSQL("reply_outbox_reclaim_expired.sql")
	replyOutboxManualReviewStatsSQL  = mustSQL("reply_outbox_manual_review_stats.sql")
	replyOutboxReadySnapshotSQL      = mustSQL("reply_outbox_ready_snapshot.sql")
	replyOutboxReplayManualReviewSQL = strings.NewReplacer(
		":'outbox_id'", "$1",
		":'operator_actor'", "$2",
		":'operator_reason'", "$3",
	).Replace(mustSQL("reply_outbox_replay_manual_review.sql"))
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

	ReplyOutboxMaxAttempts            = int32(5)
	ReplyOutboxAutomaticReplayHorizon = 144 * time.Hour
)

const (
	ReplyOutboxHandoffCompleted     = "handoff_completed"
	ReplyOutboxRetryablePreDispatch = "retryable_pre_dispatch"
	ReplyOutboxOutcomeUnknown       = "outcome_unknown"
	ReplyOutboxDead                 = "dead"
	ReplyOutboxPermanentConflict    = "permanent_conflict"
	ReplyOutboxManualReview         = "manual_review"
	ReplyOutboxDiscarded            = "discarded"
)

// accepted는 Iris가 이미 수리한 뒤라 그 행을 claim 큐로 되돌리면 admission idempotency TTL이 지난
// 시점에 중복 발화가 된다. 그래서 accepted에서 갈 수 있는 곳은 재발송 불가한 종단뿐이고, 정산이 없으면
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
	RetryAfter time.Duration
}

type ReplyOutboxRepository struct {
	pool                   *pgxpool.Pool
	maxAttempts            int32
	automaticReplayHorizon time.Duration
}

func NewReplyOutboxRepository(pool *pgxpool.Pool) *ReplyOutboxRepository {
	repository, err := NewReplyOutboxRepositoryWithPolicy(pool, ReplyOutboxMaxAttempts, ReplyOutboxAutomaticReplayHorizon)
	if err != nil {
		panic(err)
	}

	return repository
}

func NewReplyOutboxRepositoryWithPolicy(pool *pgxpool.Pool, maxAttempts int32, automaticReplayHorizon time.Duration) (*ReplyOutboxRepository, error) {
	if maxAttempts < 1 {
		return nil, errors.Join(ErrInvalidArgument, errors.New("reply outbox max attempts must be positive"))
	}

	if _, err := leaseMilliseconds(automaticReplayHorizon); err != nil {
		return nil, errors.Join(ErrInvalidArgument, fmt.Errorf("reply outbox automatic replay horizon: %w", err))
	}

	return &ReplyOutboxRepository{pool: pool, maxAttempts: maxAttempts, automaticReplayHorizon: automaticReplayHorizon}, nil
}

func (r *ReplyOutboxRepository) ReadySnapshot(ctx context.Context) (ReadyQueueSnapshot, error) {
	if err := ensurePool(r.pool); err != nil {
		return ReadyQueueSnapshot{}, fmt.Errorf("ensure pool: %w", err)
	}

	replayHorizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return ReadyQueueSnapshot{}, fmt.Errorf("lease milliseconds: %w", err)
	}

	var (
		snapshot         ReadyQueueSnapshot
		oldestAgeSeconds float64
	)

	if err := r.pool.QueryRow(ctx, replyOutboxReadySnapshotSQL, r.maxAttempts, replayHorizonMS).
		Scan(&snapshot.Depth, &oldestAgeSeconds); err != nil {
		return ReadyQueueSnapshot{}, fmt.Errorf("snapshot reply outbox ready queue: %w", err)
	}

	snapshot.OldestAge = time.Duration(oldestAgeSeconds * float64(time.Second))

	return snapshot, nil
}

func (r *ReplyOutboxRepository) Insert(ctx context.Context, entry *ReplyOutboxEntry) (ReplyOutboxInsertOutcome, error) {
	if err := ensurePool(r.pool); err != nil {
		return ReplyOutboxInserted, fmt.Errorf("ensure pool: %w", err)
	}

	if entry == nil {
		return ReplyOutboxInserted, errors.Join(ErrInvalidArgument, errors.New("entry must not be nil"))
	}

	normalized, err := normalizeReplyOutboxEntry(entry)
	if err != nil {
		return ReplyOutboxInserted, fmt.Errorf("normalize reply outbox entry: %w", err)
	}

	digest := sha256.Sum256(normalized.Payload)
	payloadHash := hex.EncodeToString(digest[:])

	tag, err := r.pool.Exec(ctx, replyOutboxInsertSQL,
		normalized.MessageID,
		normalized.Phase,
		normalized.Ordinal,
		normalized.RoomID,
		jsonbParam(normalized.Payload),
		payloadHash,
		normalized.ClientRequestID,
	)
	if err != nil {
		return ReplyOutboxInserted, safeMessageRepositoryError("insert reply outbox row", normalized.MessageID, err)
	}

	if tag.RowsAffected() == 1 {
		return ReplyOutboxInserted, nil
	}

	out, err := r.classifyRecordedPayload(ctx, &normalized, payloadHash)
	if err != nil {
		return out, fmt.Errorf("classify recorded payload: %w", err)
	}

	return out, nil
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
			safeMessageRepositoryError("inspect reply outbox row", entry.MessageID, err)
	}

	if recordedHash != payloadHash || recordedClientRequestID != entry.ClientRequestID {
		return ReplyOutboxPayloadDiverged, nil
	}

	return ReplyOutboxAlreadyRecorded, nil
}

func (r *ReplyOutboxRepository) Claim(ctx context.Context, claimToken string, lease time.Duration) (*ReplyOutboxClaim, error) {
	if err := ensurePool(r.pool); err != nil {
		return nil, fmt.Errorf("ensure pool: %w", err)
	}

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return nil, fmt.Errorf("require bounded identity: %w", err)
	}

	leaseMS, err := leaseMilliseconds(lease)
	if err != nil {
		return nil, fmt.Errorf("lease milliseconds: %w", err)
	}

	var claim ReplyOutboxClaim

	replayHorizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return nil, fmt.Errorf("lease milliseconds: %w", err)
	}

	row := r.pool.QueryRow(ctx, replyOutboxClaimSQL, token, leaseMS, r.maxAttempts, replayHorizonMS)

	err = row.Scan(&claim.ID, &claim.MessageID, &claim.Phase, &claim.Ordinal,
		&claim.RoomID, &claim.Payload, &claim.ClientRequestID, &claim.Attempts)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // 행 없음(no row)을 알리는 계약값이다. runtime 폴링 루프가 nil 결과로 유휴를 판정하므로 sentinel 오류로 바꾸려면 이 패키지 밖 호출자를 함께 고쳐야 한다.
	}

	if err != nil {
		return nil, fmt.Errorf("claim reply outbox row: %w", err)
	}

	return &claim, nil
}

func (r *ReplyOutboxRepository) MarkAccepted(ctx context.Context, id int64, claimToken, irisRequestID string) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, fmt.Errorf("ensure pool: %w", err)
	}

	token, err := requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return false, fmt.Errorf("require bounded identity: %w", err)
	}

	requestID, err := requireIdentity("iris request id", irisRequestID)
	if err != nil {
		return false, fmt.Errorf("require identity: %w", err)
	}

	tag, err := r.pool.Exec(ctx, replyOutboxMarkAcceptedSQL, id, token, requestID)
	if err != nil {
		return false, fmt.Errorf("mark reply outbox row %d accepted: %w", id, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *ReplyOutboxRepository) Settle(ctx context.Context, settlement ReplyOutboxSettlement) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, fmt.Errorf("ensure pool: %w", err)
	}

	token, err := requireBoundedIdentity("claim token", settlement.ClaimToken, claimTokenRuneLimit)
	if err != nil {
		return false, fmt.Errorf("require bounded identity: %w", err)
	}

	sources, ok := replyOutboxSettleSources[settlement.Status]
	if !ok {
		return false, errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported reply outbox settle status %q", settlement.Status))
	}

	retryMS, err := replyOutboxRetryMilliseconds(settlement)
	if err != nil {
		return false, fmt.Errorf("reply outbox retry milliseconds: %w", err)
	}

	tag, err := r.pool.Exec(ctx, replyOutboxSettleSQL, settlement.ID, token, settlement.Status,
		clampColumnText(settlement.LastError, lastErrorByteLimit), sources, retryMS)
	if err != nil {
		return false, fmt.Errorf("settle reply outbox row %d: %w", settlement.ID, err)
	}

	return tag.RowsAffected() == 1, nil
}

func replyOutboxRetryMilliseconds(settlement ReplyOutboxSettlement) (int64, error) {
	if settlement.Status != ReplyOutboxRetryablePreDispatch && settlement.Status != ReplyOutboxOutcomeUnknown {
		return 0, nil
	}

	if settlement.RetryAfter <= 0 {
		settlement.RetryAfter = time.Millisecond
	}

	out, err := leaseMilliseconds(settlement.RetryAfter)
	if err != nil {
		return out, fmt.Errorf("lease milliseconds: %w", err)
	}

	return out, nil
}
