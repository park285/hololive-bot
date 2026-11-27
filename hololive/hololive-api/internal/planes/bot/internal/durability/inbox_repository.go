package durability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/sqlite/sqlitedb"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

type InboxReleaseOutcome int

const (
	InboxReleaseNotOwned InboxReleaseOutcome = iota
	InboxReleaseRetried
	InboxReleaseAbandoned
)

const (
	InboxFailureProcessingFailed            = "processing_failed"
	InboxFailureCommandAlreadyClaimed       = "command_already_claimed"
	InboxFailureCommandClaimFailed          = "command_claim_failed"
	InboxFailureCommandClaimContextCanceled = "command_claim_context_canceled"
	InboxFailureCommandClaimContextDeadline = "command_claim_context_deadline_exceeded"
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
	LeaseUntil  time.Time
}

type InboxRepository struct {
	store *Store
}

type ReadyQueueSnapshot struct {
	Depth     int64
	OldestAge time.Duration
}

func NewInboxRepository(store *Store) *InboxRepository {
	return &InboxRepository{store: store}
}

func (r *InboxRepository) ReadySnapshot(ctx context.Context) (ReadyQueueSnapshot, error) {
	if err := ensureStore(r.store); err != nil {
		return ReadyQueueSnapshot{}, err
	}

	var (
		snapshot    ReadyQueueSnapshot
		oldestAgeMS int64
	)

	if err := r.store.read().QueryRowContext(ctx, inboxReadySnapshotSQL, r.store.nowMS()).Scan(&snapshot.Depth, &oldestAgeMS); err != nil {
		return ReadyQueueSnapshot{}, fmt.Errorf("snapshot webhook inbox ready queue: %w", err)
	}

	snapshot.OldestAge = time.Duration(oldestAgeMS) * time.Millisecond

	return snapshot, nil
}

func (r *InboxRepository) AdmitResult(ctx context.Context, msg InboxMessage) (workercontract.AdmissionResult, error) {
	if err := ensureStore(r.store); err != nil {
		return workercontract.AdmissionFailed, err
	}

	normalized, err := normalizeInboxMessage(msg)
	if err != nil {
		return workercontract.AdmissionRejected, fmt.Errorf("normalize inbox message: %w", err)
	}

	var admitted bool

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		var admitErr error

		admitted, admitErr = admitInboxTx(ctx, tx, normalized, now)

		return admitErr
	})

	switch {
	case errors.Is(err, sqlitedb.ErrCommit):
		return workercontract.AdmissionOutcomeUnknown, safeMessageRepositoryError("commit webhook admission", normalized.MessageID, err)
	case err != nil:
		return workercontract.AdmissionFailed, safeMessageRepositoryError("admit webhook inbox row", normalized.MessageID, err)
	case !admitted:
		return workercontract.AdmissionDuplicate, nil
	default:
		return workercontract.AdmissionAccepted, nil
	}
}

func admitInboxTx(ctx context.Context, tx *sql.Tx, msg InboxMessage, now int64) (bool, error) {
	result, err := tx.ExecContext(ctx, inboxAdmitSQL, msg.MessageID, msg.RoomID, msg.OrderingKey, string(msg.Payload), now)
	if err != nil {
		return false, fmt.Errorf("insert webhook inbox row: %w", err)
	}

	inserted, err := rowsAffected(result)
	if err != nil || inserted == 0 {
		return false, err
	}

	if _, err := tx.ExecContext(ctx, inboxInstallHeadSQL, msg.OrderingKey, msg.MessageID, now); err != nil {
		return false, fmt.Errorf("install webhook ordering head: %w", err)
	}

	return true, nil
}

func normalizeInboxMessage(msg InboxMessage) (InboxMessage, error) {
	messageID, err := requireMessageIdentity(msg.MessageID)
	if err != nil {
		return InboxMessage{}, err
	}

	roomID, err := requireRoomID(msg.RoomID)
	if err != nil {
		return InboxMessage{}, fmt.Errorf("require room ID: %w", err)
	}

	orderingKey, err := requireBoundedIdentity("ordering key", msg.OrderingKey, orderingKeyRuneLimit)
	if err != nil {
		return InboxMessage{}, err
	}

	if len(msg.Payload) == 0 {
		return InboxMessage{}, errors.Join(ErrInvalidArgument, errors.New("payload must not be empty"))
	}

	return InboxMessage{MessageID: messageID, RoomID: roomID, OrderingKey: orderingKey, Payload: msg.Payload}, nil
}

func (r *InboxRepository) Claim(ctx context.Context, claimToken string, lease time.Duration) (*InboxClaim, error) {
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

	var claim *InboxClaim

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		var (
			row          InboxClaim
			leaseUntilMS int64
		)

		scanErr := tx.QueryRowContext(ctx, inboxClaimSQL, now, token, leaseMS).
			Scan(&row.MessageID, &row.RoomID, &row.OrderingKey, &row.Payload, &row.Attempts, &leaseUntilMS)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil
		}

		if scanErr != nil {
			return fmt.Errorf("lease inbox message: %w", scanErr)
		}

		row.LeaseUntil = timeFromMS(leaseUntilMS)
		claim = &row

		return nil
	})
	if err != nil {
		return nil, safeRepositoryError("claim webhook inbox row", err)
	}

	return claim, nil
}

func (r *InboxRepository) Complete(ctx context.Context, messageID, claimToken string) (bool, error) {
	if err := ensureStore(r.store); err != nil {
		return false, err
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return false, fmt.Errorf("fence args: %w", err)
	}

	applied, err := r.settleTerminal(ctx, id, func(tx *sql.Tx, now int64) *sql.Row {
		return tx.QueryRowContext(ctx, inboxCompleteSQL, id, token, now)
	})
	if err != nil {
		return false, safeMessageRepositoryError("complete webhook inbox row", id, err)
	}

	return applied, nil
}

func (r *InboxRepository) Release(
	ctx context.Context,
	messageID, claimToken string,
	attempts, maxAttempts int32,
	retryAfter time.Duration,
	lastError string,
) (InboxReleaseOutcome, error) {
	if err := ensureStore(r.store); err != nil {
		return InboxReleaseNotOwned, err
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return InboxReleaseNotOwned, fmt.Errorf("fence args: %w", err)
	}

	if attempts <= 0 {
		return InboxReleaseNotOwned, errors.Join(ErrInvalidArgument, errors.New("claimed attempts must be positive"))
	}

	retryMS, err := validateInboxRelease(maxAttempts, retryAfter)
	if err != nil {
		return InboxReleaseNotOwned, fmt.Errorf("validate inbox release: %w", err)
	}

	reason := normalizeInboxFailureReason(lastError)

	if attempts < maxAttempts {
		retried, retryErr := r.releaseForRetry(ctx, id, token, retryMS, reason, attempts)
		if retryErr != nil || !retried {
			return InboxReleaseNotOwned, retryErr
		}

		return InboxReleaseRetried, nil
	}

	abandoned, err := r.settleTerminal(ctx, id, func(tx *sql.Tx, now int64) *sql.Row {
		return tx.QueryRowContext(ctx, inboxReleaseSQL, id, token, now, retryMS, reason, attempts)
	})
	if err != nil {
		return InboxReleaseNotOwned, safeMessageRepositoryError("release webhook inbox row", id, err)
	}

	if !abandoned {
		return InboxReleaseNotOwned, nil
	}

	return InboxReleaseAbandoned, nil
}

func (r *InboxRepository) releaseForRetry(ctx context.Context, id, token string, retryMS int64, reason string, attempts int32) (bool, error) {
	var retried bool

	err := r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		var orderingKey string

		scanErr := tx.QueryRowContext(ctx, inboxReleaseRetrySQL, id, token, now, retryMS, reason, attempts).Scan(&orderingKey)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil
		}

		if scanErr != nil {
			return fmt.Errorf("release inbox message for retry: %w", scanErr)
		}

		retried = true

		return nil
	})
	if err != nil {
		return false, safeMessageRepositoryError("release webhook inbox row", id, err)
	}

	return retried, nil
}

func normalizeInboxFailureReason(reason string) string {
	switch reason {
	case InboxFailureCommandAlreadyClaimed,
		InboxFailureCommandClaimFailed,
		InboxFailureCommandClaimContextCanceled,
		InboxFailureCommandClaimContextDeadline:
		return reason
	default:
		return InboxFailureProcessingFailed
	}
}

func validateInboxRelease(maxAttempts int32, retryAfter time.Duration) (int64, error) {
	if maxAttempts <= 0 {
		return 0, errors.Join(ErrInvalidArgument, errors.New("max attempts must be positive"))
	}

	out, err := leaseMilliseconds(retryAfter)
	if err != nil {
		return out, err
	}

	return out, nil
}

func (r *InboxRepository) Abandon(ctx context.Context, messageID, claimToken, reason string) (bool, error) {
	if err := ensureStore(r.store); err != nil {
		return false, err
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return false, fmt.Errorf("fence args: %w", err)
	}

	terminalReason, err := requireIdentity("terminal reason", reason)
	if err != nil {
		return false, err
	}

	terminalReason = clampColumnText(terminalReason, terminalReasonByteLimit)

	applied, err := r.settleTerminal(ctx, id, func(tx *sql.Tx, now int64) *sql.Row {
		return tx.QueryRowContext(ctx, inboxAbandonSQL, id, token, now, terminalReason)
	})
	if err != nil {
		return false, safeMessageRepositoryError("abandon webhook inbox row", id, err)
	}

	return applied, nil
}

func (r *InboxRepository) Heartbeat(ctx context.Context, messageID, claimToken string, lease time.Duration) (time.Time, bool, error) {
	if err := ensureStore(r.store); err != nil {
		return time.Time{}, false, err
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("fence args: %w", err)
	}

	leaseMS, err := leaseMilliseconds(lease)
	if err != nil {
		return time.Time{}, false, err
	}

	var (
		leaseUntilMS int64
		renewed      bool
	)

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		scanErr := tx.QueryRowContext(ctx, inboxHeartbeatSQL, id, token, now, leaseMS).Scan(&leaseUntilMS)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil
		}

		if scanErr != nil {
			return fmt.Errorf("renew inbox lease: %w", scanErr)
		}

		renewed = true

		return nil
	})
	if err != nil {
		return time.Time{}, false, safeMessageRepositoryError("heartbeat webhook inbox row", id, err)
	}

	if !renewed {
		return time.Time{}, false, nil
	}

	return timeFromMS(leaseUntilMS), true, nil
}
