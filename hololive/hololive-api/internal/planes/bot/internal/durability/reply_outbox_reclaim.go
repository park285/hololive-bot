package durability

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var replyOutboxReclaimCandidatesSQL = mustSQL("reply_outbox_reclaim_candidates.sql")

const replyOutboxStatusPending = "pending"

type replyReclaimCandidate struct {
	ID                   int64
	Status               string
	Attempts             int32
	OperatorReplayGrants int32
	HorizonExpired       bool
}

// ReclaimExpired는 잠긴 만료 행의 사실로 Go에서 전이를 결정합니다.
// Accepted는 재발송하지 않으며, 재시도 지평이 저장 전에 끝나면 배치 전체를 롤백합니다.
func (r *ReplyOutboxRepository) ReclaimExpired(ctx context.Context, batchSize int32) (ReplyOutboxReclaim, error) {
	if err := ensurePool(r.pool); err != nil {
		return ReplyOutboxReclaim{}, fmt.Errorf("ensure pool: %w", err)
	}

	if batchSize <= 0 {
		return ReplyOutboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	horizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return ReplyOutboxReclaim{}, fmt.Errorf("lease milliseconds: %w", err)
	}

	var result ReplyOutboxReclaim

	if err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		candidates, err := lockedReplyReclaimCandidates(ctx, tx, batchSize, horizonMS)
		if err != nil {
			return err
		}

		result, err = applyReplyReclaim(ctx, tx, candidates, r.maxAttempts, horizonMS)

		return err
	}); err != nil {
		return ReplyOutboxReclaim{}, fmt.Errorf("reclaim expired reply outbox leases: %w", err)
	}

	return result, nil
}

func lockedReplyReclaimCandidates(ctx context.Context, tx pgx.Tx, batchSize int32, horizonMS int64) ([]replyReclaimCandidate, error) {
	rows, err := tx.Query(ctx, replyOutboxReclaimCandidatesSQL, batchSize, horizonMS)
	if err != nil {
		return nil, fmt.Errorf("lock reply reclaim candidates: %w", err)
	}

	candidates, err := pgx.CollectRows(rows, pgx.RowToStructByPos[replyReclaimCandidate])
	if err != nil {
		return nil, fmt.Errorf("read reply reclaim candidates: %w", err)
	}

	return candidates, nil
}

type replyReclaimMutation struct {
	ids                      []int64
	sources, targets, errors []string
	retry                    []bool
	result                   ReplyOutboxReclaim
}

func newReplyReclaimMutation(candidates []replyReclaimCandidate, maxAttempts int32) (replyReclaimMutation, error) {
	mutation := replyReclaimMutation{
		ids: make([]int64, 0, len(candidates)), sources: make([]string, 0, len(candidates)),
		targets: make([]string, 0, len(candidates)), errors: make([]string, 0, len(candidates)),
		retry: make([]bool, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		retry := candidate.Status == replyOutboxStatusSubmitting && !candidate.HorizonExpired &&
			int64(candidate.Attempts) < int64(maxAttempts)+int64(candidate.OperatorReplayGrants)
		target, reason := ReplyOutboxManualReview, "automatic replay safety boundary reached"

		switch {
		case candidate.Status == replyOutboxStatusAccepted:
			mutation.result.AcceptedManualReview++
		case retry:
			target, reason = replyOutboxStatusPending, "submit lease expired"
			mutation.result.Requeued++
		case candidate.Status == replyOutboxStatusSubmitting:
			mutation.result.SafetyManualReview++
		default:
			return replyReclaimMutation{}, errors.New("unexpected reply reclaim source state")
		}

		mutation.ids = append(mutation.ids, candidate.ID)
		mutation.sources = append(mutation.sources, candidate.Status)
		mutation.targets = append(mutation.targets, target)
		mutation.errors = append(mutation.errors, reason)
		mutation.retry = append(mutation.retry, retry)
	}

	return mutation, nil
}

// 호출자의 트랜잭션이 후보 행의 잠금을 유지해야 합니다.
func applyReplyReclaim(ctx context.Context, tx pgx.Tx, candidates []replyReclaimCandidate, maxAttempts int32, horizonMS int64) (ReplyOutboxReclaim, error) {
	if len(candidates) == 0 {
		return ReplyOutboxReclaim{}, nil
	}

	mutation, err := newReplyReclaimMutation(candidates, maxAttempts)
	if err != nil {
		return ReplyOutboxReclaim{}, err
	}

	tag, err := tx.Exec(ctx, replyOutboxReclaimExpiredSQL, mutation.ids, mutation.sources,
		mutation.targets, mutation.errors, mutation.retry, horizonMS)
	if err != nil {
		return ReplyOutboxReclaim{}, fmt.Errorf("apply reply reclaim: %w", err)
	}

	if tag.RowsAffected() != int64(len(candidates)) {
		return ReplyOutboxReclaim{}, errors.New("reply reclaim snapshot no longer eligible")
	}

	return mutation.result, nil
}
