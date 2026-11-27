package durability

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
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
	store                  *Store
	maxAttempts            int32
	automaticReplayHorizon time.Duration
}

func NewReplyOutboxRepositoryWithPolicy(store *Store, maxAttempts int32, automaticReplayHorizon time.Duration) (*ReplyOutboxRepository, error) {
	if maxAttempts < 1 {
		return nil, errors.Join(ErrInvalidArgument, errors.New("reply outbox max attempts must be positive"))
	}

	if _, err := leaseMilliseconds(automaticReplayHorizon); err != nil {
		return nil, errors.Join(ErrInvalidArgument, fmt.Errorf("reply outbox automatic replay horizon: %w", err))
	}

	return &ReplyOutboxRepository{store: store, maxAttempts: maxAttempts, automaticReplayHorizon: automaticReplayHorizon}, nil
}

func (r *ReplyOutboxRepository) ReadySnapshot(ctx context.Context) (ReadyQueueSnapshot, error) {
	if err := ensureStore(r.store); err != nil {
		return ReadyQueueSnapshot{}, err
	}

	replayHorizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return ReadyQueueSnapshot{}, err
	}

	var (
		snapshot    ReadyQueueSnapshot
		oldestAgeMS int64
	)

	if err := r.store.read().QueryRowContext(ctx, replyOutboxReadySnapshotSQL, r.store.nowMS(), r.maxAttempts, replayHorizonMS).
		Scan(&snapshot.Depth, &oldestAgeMS); err != nil {
		return ReadyQueueSnapshot{}, fmt.Errorf("snapshot reply outbox ready queue: %w", err)
	}

	snapshot.OldestAge = time.Duration(oldestAgeMS) * time.Millisecond

	return snapshot, nil
}

func (r *ReplyOutboxRepository) Insert(ctx context.Context, entry *ReplyOutboxEntry) (ReplyOutboxInsertOutcome, error) {
	if err := ensureStore(r.store); err != nil {
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

	var outcome ReplyOutboxInsertOutcome

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		result, execErr := tx.ExecContext(ctx, replyOutboxInsertSQL,
			normalized.MessageID,
			normalized.Phase,
			normalized.Ordinal,
			normalized.RoomID,
			string(normalized.Payload),
			payloadHash,
			normalized.ClientRequestID,
			now,
		)
		if execErr != nil {
			return fmt.Errorf("insert reply outbox row: %w", execErr)
		}

		inserted, execErr := rowsAffected(result)
		if execErr != nil || inserted == 1 {
			outcome = ReplyOutboxInserted

			return execErr
		}

		outcome, execErr = classifyRecordedPayload(ctx, tx, &normalized, payloadHash)

		return execErr
	})
	if err != nil {
		return ReplyOutboxInserted, safeMessageRepositoryError("insert reply outbox row", normalized.MessageID, err)
	}

	return outcome, nil
}

func classifyRecordedPayload(ctx context.Context, tx *sql.Tx, entry *ReplyOutboxEntry, payloadHash string) (ReplyOutboxInsertOutcome, error) {
	var recordedHash, recordedClientRequestID string

	err := tx.QueryRowContext(ctx, replyOutboxConflictSQL, entry.MessageID, entry.Phase, entry.Ordinal).
		Scan(&recordedHash, &recordedClientRequestID)
	if err != nil {
		return ReplyOutboxAlreadyRecorded, fmt.Errorf("inspect recorded reply outbox row: %w", err)
	}

	if recordedHash != payloadHash || recordedClientRequestID != entry.ClientRequestID {
		return ReplyOutboxPayloadDiverged, nil
	}

	return ReplyOutboxAlreadyRecorded, nil
}

func (r *ReplyOutboxRepository) Claim(ctx context.Context, claimToken string, lease time.Duration) (*ReplyOutboxClaim, error) {
	if err := ensureStore(r.store); err != nil {
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

	replayHorizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return nil, err
	}

	var claim *ReplyOutboxClaim

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		var (
			id            int64
			safetyExpired bool
		)

		scanErr := tx.QueryRowContext(ctx, replyOutboxClaimCandidateSQL, now, r.maxAttempts, replayHorizonMS).Scan(&id, &safetyExpired)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil
		}

		if scanErr != nil {
			return fmt.Errorf("select reply outbox claim candidate: %w", scanErr)
		}

		if safetyExpired {
			if _, execErr := tx.ExecContext(ctx, replyOutboxClaimSafetyReviewSQL, id, now); execErr != nil {
				return fmt.Errorf("move expired reply outbox row to manual review: %w", execErr)
			}

			return nil
		}

		var row ReplyOutboxClaim

		if scanErr := tx.QueryRowContext(ctx, replyOutboxClaimSQL, id, token, now, leaseMS).
			Scan(&row.ID, &row.MessageID, &row.Phase, &row.Ordinal, &row.RoomID, &row.Payload, &row.ClientRequestID, &row.Attempts); scanErr != nil {
			return fmt.Errorf("lease reply outbox row: %w", scanErr)
		}

		claim = &row

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claim reply outbox row: %w", err)
	}

	return claim, nil
}

func (r *ReplyOutboxRepository) Settle(ctx context.Context, settlement ReplyOutboxSettlement) (bool, error) {
	if err := ensureStore(r.store); err != nil {
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

	var retryMS int64

	if policy.retry {
		retryMS, err = replyOutboxRetryMilliseconds(settlement.RetryAfter)
		if err != nil {
			return false, fmt.Errorf("reply outbox retry milliseconds: %w", err)
		}
	}

	applied, err := r.execOne(ctx, policy.sql, func(now int64) []any {
		if policy.retry {
			return append(args, now, retryMS)
		}

		return append(args, now)
	})
	if err != nil {
		return false, fmt.Errorf("settle reply outbox row %d: %w", settlement.ID, err)
	}

	return applied, nil
}

func (r *ReplyOutboxRepository) execOne(ctx context.Context, query string, args func(now int64) []any) (bool, error) {
	var applied bool

	err := r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		result, execErr := tx.ExecContext(ctx, query, args(now)...)
		if execErr != nil {
			return fmt.Errorf("execute reply outbox update: %w", execErr)
		}

		affected, execErr := rowsAffected(result)

		applied = affected == 1

		return execErr
	})

	return applied, err
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
