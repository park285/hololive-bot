package durability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type InboxReclaim struct {
	Requeued  int64
	Abandoned int64
}

var errInboxReclaimCandidateChanged = errors.New("reclaim expired webhook inbox: candidate changed inside the write transaction")

func (r *InboxRepository) ReclaimExpired(ctx context.Context, maxAttempts, batchSize int32) (InboxReclaim, error) {
	if err := ensureStore(r.store); err != nil {
		return InboxReclaim{}, err
	}

	if maxAttempts <= 0 {
		return InboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("max attempts must be positive"))
	}

	if batchSize <= 0 {
		return InboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	var reclaim InboxReclaim

	err := r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		var reclaimErr error

		reclaim, reclaimErr = reclaimExpiredInboxTx(ctx, tx, maxAttempts, batchSize, now)

		return reclaimErr
	})
	if err != nil {
		return InboxReclaim{}, fmt.Errorf("reclaim expired webhook inbox leases: %w", err)
	}

	return reclaim, nil
}

type inboxReclaimCandidate struct {
	ID       int64
	Attempts int32
}

func reclaimExpiredInboxTx(ctx context.Context, tx *sql.Tx, maxAttempts, batchSize int32, now int64) (InboxReclaim, error) {
	candidates, err := expiredInboxCandidates(ctx, tx, batchSize, now)
	if err != nil {
		return InboxReclaim{}, err
	}

	var reclaim InboxReclaim

	for _, candidate := range candidates {
		exhausted := candidate.Attempts >= maxAttempts

		query := inboxReclaimRetrySQL

		if exhausted {
			query = inboxReclaimDeadSQL
		}

		var messageID, orderingKey string

		if err := tx.QueryRowContext(ctx, query, candidate.ID, candidate.Attempts, now).Scan(&messageID, &orderingKey); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return InboxReclaim{}, errInboxReclaimCandidateChanged
			}

			return InboxReclaim{}, fmt.Errorf("reclaim webhook inbox row %d: %w", candidate.ID, err)
		}

		if !exhausted {
			reclaim.Requeued++

			continue
		}

		if err := advanceInboxHead(ctx, tx, orderingKey, messageID, now); err != nil {
			return InboxReclaim{}, err
		}

		reclaim.Abandoned++
	}

	return reclaim, nil
}

func expiredInboxCandidates(ctx context.Context, tx *sql.Tx, batchSize int32, now int64) ([]inboxReclaimCandidate, error) {
	rows, err := tx.QueryContext(ctx, inboxReclaimCandidatesSQL, now, batchSize)
	if err != nil {
		return nil, fmt.Errorf("select expired webhook inbox candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]inboxReclaimCandidate, 0, batchSize)

	for rows.Next() {
		var candidate inboxReclaimCandidate

		if err := rows.Scan(&candidate.ID, &candidate.Attempts); err != nil {
			return nil, fmt.Errorf("scan expired webhook inbox candidate: %w", err)
		}

		candidates = append(candidates, candidate)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired webhook inbox candidates: %w", err)
	}

	return candidates, nil
}

func (r *InboxRepository) settleTerminal(ctx context.Context, messageID string, settle func(*sql.Tx, int64) *sql.Row) (bool, error) {
	var applied bool

	err := r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		var orderingKey string

		scanErr := settle(tx, now).Scan(&orderingKey)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil
		}

		if scanErr != nil {
			return fmt.Errorf("settle inbox message: %w", scanErr)
		}

		applied = true

		return advanceInboxHead(ctx, tx, orderingKey, messageID, now)
	})

	return applied, err
}

func advanceInboxHead(ctx context.Context, tx *sql.Tx, orderingKey, settledMessageID string, now int64) error {
	var successor string

	err := tx.QueryRowContext(ctx, inboxHeadSuccessorSQL, orderingKey, settledMessageID).Scan(&successor)
	if errors.Is(err, sql.ErrNoRows) {
		if _, removeErr := tx.ExecContext(ctx, inboxHeadRemoveSQL, orderingKey, settledMessageID); removeErr != nil {
			return fmt.Errorf("remove webhook ordering head: %w", removeErr)
		}

		return nil
	}

	if err != nil {
		return fmt.Errorf("select webhook ordering successor: %w", err)
	}

	if _, err := tx.ExecContext(ctx, inboxHeadAdvanceSQL, orderingKey, settledMessageID, successor, now); err != nil {
		return fmt.Errorf("advance webhook ordering head: %w", err)
	}

	return nil
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
