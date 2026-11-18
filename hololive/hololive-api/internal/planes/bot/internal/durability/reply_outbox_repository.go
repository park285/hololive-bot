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
	ReplyOutboxAccepted             = "accepted"
	ReplyOutboxHandoffCompleted     = "handoff_completed"
	ReplyOutboxRetryablePreDispatch = "retryable_pre_dispatch"
	ReplyOutboxOutcomeUnknown       = "outcome_unknown"
	ReplyOutboxDead                 = "dead"
	ReplyOutboxPermanentConflict    = "permanent_conflict"
	ReplyOutboxManualReview         = "manual_review"
	ReplyOutboxDiscarded            = "discarded"
)

type replyOutboxSettlementPolicy struct {
	sql   string
	retry bool
}

var replyOutboxSettlementPolicies = map[string]replyOutboxSettlementPolicy{
	ReplyOutboxRetryablePreDispatch: {sql: replyOutboxSettleRetrySQL, retry: true},
	ReplyOutboxOutcomeUnknown:       {sql: replyOutboxSettleRetrySQL, retry: true},
	ReplyOutboxDead:                 {sql: replyOutboxSettleTerminalSQL},
	ReplyOutboxPermanentConflict:    {sql: replyOutboxSettleTerminalSQL},
	ReplyOutboxManualReview:         {sql: replyOutboxSettleSQL},
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
		return ReadyQueueSnapshot{}, err
	}

	replayHorizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return ReadyQueueSnapshot{}, err
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
		return ReplyOutboxInserted, err
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

	replayHorizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return nil, err
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

func (r *ReplyOutboxRepository) Settle(ctx context.Context, settlement ReplyOutboxSettlement) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	token, err := requireBoundedIdentity("claim token", settlement.ClaimToken, claimTokenRuneLimit)
	if err != nil {
		return false, err
	}

	policy, ok := replyOutboxSettlementPolicies[settlement.Status]
	if !ok {
		return false, errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported reply outbox settle status %q", settlement.Status))
	}

	args := []any{settlement.ID, token, settlement.Status, clampColumnText(settlement.LastError, lastErrorByteLimit)}

	if policy.retry {
		retryMS, retryErr := replyOutboxRetryMilliseconds(settlement.RetryAfter)
		if retryErr != nil {
			return false, fmt.Errorf("reply outbox retry milliseconds: %w", retryErr)
		}

		args = append(args, retryMS)
	}

	tag, err := r.pool.Exec(ctx, policy.sql, args...)
	if err != nil {
		return false, fmt.Errorf("settle reply outbox row %d: %w", settlement.ID, err)
	}

	return tag.RowsAffected() == 1, nil
}

func replyOutboxRetryMilliseconds(retryAfter time.Duration) (int64, error) {
	if retryAfter <= 0 {
		retryAfter = time.Millisecond
	}

	out, err := leaseMilliseconds(retryAfter)
	if err != nil {
		return out, err
	}

	return out, nil
}
