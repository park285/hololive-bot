package durability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type replyReclaimCandidate struct {
	ID                   int64
	Attempts             int32
	OperatorReplayGrants int32
	HorizonExpired       bool
}

var errReplyReclaimSnapshotChanged = errors.New("reply reclaim snapshot no longer eligible")

func (r *ReplyOutboxRepository) ReclaimExpired(ctx context.Context, batchSize int32) (ReplyOutboxReclaim, error) {
	if err := ensureStore(r.store); err != nil {
		return ReplyOutboxReclaim{}, err
	}

	if batchSize <= 0 {
		return ReplyOutboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	horizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return ReplyOutboxReclaim{}, err
	}

	var result ReplyOutboxReclaim

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		candidates, candidateErr := expiredReplyReclaimCandidates(ctx, tx, now, batchSize, horizonMS)
		if candidateErr != nil {
			return candidateErr
		}

		result, candidateErr = applyReplyReclaim(ctx, tx, candidates, r.maxAttempts, now, horizonMS)

		return candidateErr
	})
	if err != nil {
		return ReplyOutboxReclaim{}, fmt.Errorf("reclaim expired reply outbox leases: %w", err)
	}

	return result, nil
}

func expiredReplyReclaimCandidates(ctx context.Context, tx *sql.Tx, now int64, batchSize int32, horizonMS int64) ([]replyReclaimCandidate, error) {
	rows, err := tx.QueryContext(ctx, replyOutboxReclaimCandidatesSQL, now, batchSize, horizonMS)
	if err != nil {
		return nil, fmt.Errorf("select reply reclaim candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]replyReclaimCandidate, 0, batchSize)

	for rows.Next() {
		var candidate replyReclaimCandidate

		if err := rows.Scan(&candidate.ID, &candidate.Attempts, &candidate.OperatorReplayGrants, &candidate.HorizonExpired); err != nil {
			return nil, fmt.Errorf("scan reply reclaim candidate: %w", err)
		}

		candidates = append(candidates, candidate)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read reply reclaim candidates: %w", err)
	}

	return candidates, nil
}

func applyReplyReclaim(ctx context.Context, tx *sql.Tx, candidates []replyReclaimCandidate, maxAttempts int32, now, horizonMS int64) (ReplyOutboxReclaim, error) {
	var result ReplyOutboxReclaim

	for _, candidate := range candidates {
		retry := !candidate.HorizonExpired &&
			int64(candidate.Attempts) < int64(maxAttempts)+int64(candidate.OperatorReplayGrants)

		query, args := replyOutboxReclaimReviewSQL, []any{candidate.ID, now}

		if retry {
			query, args = replyOutboxReclaimRequeueSQL, []any{candidate.ID, now, horizonMS}
			result.Requeued++
		} else {
			result.SafetyManualReview++
		}

		outcome, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return ReplyOutboxReclaim{}, fmt.Errorf("apply reply reclaim to row %d: %w", candidate.ID, err)
		}

		if affected, err := rowsAffected(outcome); err != nil || affected != 1 {
			return ReplyOutboxReclaim{}, errors.Join(errReplyReclaimSnapshotChanged, err)
		}
	}

	return result, nil
}
