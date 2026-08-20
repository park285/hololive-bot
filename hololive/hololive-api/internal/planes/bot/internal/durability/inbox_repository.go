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
	"github.com/park285/shared-go/pkg/workercontract"
)

var (
	inboxAdmitSQL                = mustSQL("inbox_admit.sql")
	inboxClaimSQL                = mustSQL("inbox_claim.sql")
	inboxCompleteSQL             = mustSQL("inbox_complete.sql")
	inboxReleaseSQL              = mustSQL("inbox_release.sql")
	inboxAbandonSQL              = mustSQL("inbox_abandon.sql")
	inboxHeartbeatSQL            = mustSQL("inbox_heartbeat.sql")
	inboxReclaimExpiredSQL       = mustSQL("inbox_reclaim_expired.sql")
	inboxReclaimExpiredKeysSQL   = mustSQL("inbox_reclaim_expired_keys.sql")
	inboxOrderingKeyLockSQL      = mustSQL("inbox_ordering_key_lock.sql")
	inboxOrderingKeyByMessageSQL = mustSQL("inbox_ordering_key_by_message.sql")
	inboxReadySnapshotSQL        = mustSQL("inbox_ready_snapshot.sql")
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
	pool *pgxpool.Pool
}

type ReadyQueueSnapshot struct {
	Depth     int64
	OldestAge time.Duration
}

func NewInboxRepository(pool *pgxpool.Pool) *InboxRepository {
	return &InboxRepository{pool: pool}
}

func (r *InboxRepository) ReadySnapshot(ctx context.Context) (ReadyQueueSnapshot, error) {
	if err := ensurePool(r.pool); err != nil {
		return ReadyQueueSnapshot{}, err
	}
	var snapshot ReadyQueueSnapshot
	var oldestAgeSeconds float64
	if err := r.pool.QueryRow(ctx, inboxReadySnapshotSQL).Scan(&snapshot.Depth, &oldestAgeSeconds); err != nil {
		return ReadyQueueSnapshot{}, fmt.Errorf("snapshot webhook inbox ready queue: %w", err)
	}
	snapshot.OldestAge = time.Duration(oldestAgeSeconds * float64(time.Second))
	return snapshot, nil
}

func (r *InboxRepository) Admit(ctx context.Context, msg InboxMessage) (admitted bool, err error) {
	result, err := r.AdmitResult(ctx, msg)
	return result == workercontract.AdmissionAccepted, err
}

func (r *InboxRepository) AdmitResult(ctx context.Context, msg InboxMessage) (result workercontract.AdmissionResult, err error) {
	if err := ensurePool(r.pool); err != nil {
		return workercontract.AdmissionFailed, err
	}
	normalized, err := normalizeInboxMessage(msg)
	if err != nil {
		return workercontract.AdmissionRejected, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return workercontract.AdmissionFailed, safeMessageRepositoryError("begin webhook admission", normalized.MessageID, err)
	}
	defer func() { err = errors.Join(err, rollbackInboxTx(ctx, tx)) }()
	if err := lockInboxOrderingKey(ctx, tx, normalized.OrderingKey); err != nil {
		return workercontract.AdmissionFailed, safeMessageRepositoryError("lock webhook admission ordering", normalized.MessageID, err)
	}
	var admitted bool
	err = tx.QueryRow(ctx, inboxAdmitSQL, normalized.MessageID, normalized.RoomID, normalized.OrderingKey, jsonbParam(normalized.Payload)).Scan(&admitted)
	if err != nil {
		return workercontract.AdmissionFailed, safeMessageRepositoryError("admit webhook inbox row", normalized.MessageID, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return workercontract.AdmissionOutcomeUnknown, safeMessageRepositoryError("commit webhook admission", normalized.MessageID, err)
	}
	if !admitted {
		return workercontract.AdmissionDuplicate, nil
	}
	return workercontract.AdmissionAccepted, nil
}

func normalizeInboxMessage(msg InboxMessage) (InboxMessage, error) {
	messageID, err := requireMessageIdentity(msg.MessageID)
	if err != nil {
		return InboxMessage{}, err
	}
	roomID, err := requireRoomID(msg.RoomID)
	if err != nil {
		return InboxMessage{}, err
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
	err = row.Scan(&claim.MessageID, &claim.RoomID, &claim.OrderingKey, &claim.Payload, &claim.Attempts, &claim.LeaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, safeRepositoryError("claim webhook inbox row", err)
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
	retryMS, err := validateInboxRelease(maxAttempts, retryAfter)
	if err != nil {
		return InboxReleaseNotOwned, err
	}
	status, err := r.releaseLocked(ctx, id, token, retryMS, maxAttempts, lastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return InboxReleaseNotOwned, nil
	}
	if err != nil {
		return InboxReleaseNotOwned, err
	}
	if status == inboxStatusDead {
		return InboxReleaseAbandoned, nil
	}
	return InboxReleaseRetried, nil
}

func (r *InboxRepository) releaseLocked(ctx context.Context, id, token string, retryMS int64, maxAttempts int32, lastError string) (status string, err error) {
	tx, err := r.beginLockedMessageTx(ctx, id)
	if err != nil {
		return "", safeMessageRepositoryError("begin webhook release", id, err)
	}
	defer func() { err = errors.Join(err, rollbackInboxTx(ctx, tx)) }()
	err = tx.QueryRow(ctx, inboxReleaseSQL, id, token, retryMS,
		normalizeInboxFailureReason(lastError), maxAttempts).Scan(&status)
	if err != nil {
		return "", safeMessageRepositoryError("release webhook inbox row", id, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return "", safeMessageRepositoryError("commit webhook release", id, err)
	}
	return status, nil
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
	return leaseMilliseconds(retryAfter)
}

func (r *InboxRepository) Abandon(ctx context.Context, messageID, claimToken, reason string) (applied bool, err error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return false, safeMessageRepositoryError("begin webhook abandon", id, err)
	}
	terminalReason, err := requireIdentity("terminal reason", reason)
	if err != nil {
		return false, err
	}

	tx, err := r.beginLockedMessageTx(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, rollbackInboxTx(ctx, tx)) }()
	err = tx.QueryRow(ctx, inboxAbandonSQL, id, token,
		clampColumnText(terminalReason, terminalReasonByteLimit)).Scan(&applied)
	if err != nil {
		return false, safeMessageRepositoryError("abandon webhook inbox row", id, err)
	}

	if err = tx.Commit(ctx); err != nil {
		return false, safeMessageRepositoryError("commit webhook abandon", id, err)
	}
	return applied, nil
}

func (r *InboxRepository) Heartbeat(ctx context.Context, messageID, claimToken string, lease time.Duration) (time.Time, bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return time.Time{}, false, err
	}
	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return time.Time{}, false, err
	}
	leaseMS, err := leaseMilliseconds(lease)
	if err != nil {
		return time.Time{}, false, err
	}
	var leaseUntil time.Time
	err = r.pool.QueryRow(ctx, inboxHeartbeatSQL, id, token, leaseMS).Scan(&leaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, safeMessageRepositoryError("heartbeat webhook inbox row", id, err)
	}
	return leaseUntil, true, nil
}
