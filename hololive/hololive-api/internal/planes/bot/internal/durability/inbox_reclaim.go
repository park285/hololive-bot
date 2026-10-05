package durability

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type InboxReclaim struct {
	Requeued  int64
	Abandoned int64
}

// 만료 lease를 되돌리면서 claim_token을 비우므로, lease를 잃은 워커의 Complete/Release는 token 대조에서
// 0 rows가 된다 — 전이 쿼리에 lease_until 술어를 따로 두지 않는 이유다.
func (r *InboxRepository) ReclaimExpired(ctx context.Context, maxAttempts, batchSize int32) (reclaim InboxReclaim, err error) {
	if poolErr := ensurePool(r.pool); poolErr != nil {
		return InboxReclaim{}, fmt.Errorf("ensure pool: %w", poolErr)
	}

	if maxAttempts <= 0 {
		return InboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("max attempts must be positive"))
	}

	if batchSize <= 0 {
		return InboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InboxReclaim{}, fmt.Errorf("begin webhook reclaim: %w", err)
	}

	defer func() { err = errors.Join(err, rollbackInboxTx(ctx, tx)) }()

	reclaim, err = reclaimExpiredInboxTx(ctx, tx, maxAttempts, batchSize)
	if err != nil {
		return InboxReclaim{}, fmt.Errorf("reclaim expired inbox tx: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return InboxReclaim{}, fmt.Errorf("commit webhook reclaim: %w", err)
	}

	return reclaim, nil
}

func reclaimExpiredInboxTx(ctx context.Context, tx pgx.Tx, maxAttempts, batchSize int32) (InboxReclaim, error) {
	keys, err := expiredInboxOrderingKeys(ctx, tx, batchSize)
	if err != nil {
		return InboxReclaim{}, fmt.Errorf("expired inbox ordering keys: %w", err)
	}

	if len(keys) == 0 {
		return InboxReclaim{}, nil
	}

	if lockErr := lockInboxOrderingKeys(ctx, tx, keys); lockErr != nil {
		return InboxReclaim{}, fmt.Errorf("lock inbox ordering keys: %w", lockErr)
	}

	candidates, err := lockedExpiredInboxCandidates(ctx, tx, batchSize, keys)
	if err != nil {
		return InboxReclaim{}, err
	}

	if len(candidates) == 0 {
		return InboxReclaim{}, nil
	}

	mutation := newInboxReclaimMutation(candidates, maxAttempts)

	var reclaim InboxReclaim

	err = tx.QueryRow(ctx, inboxReclaimExpiredSQL, mutation.ids, mutation.attempts, mutation.statuses, mutation.terminal).
		Scan(&reclaim.Requeued, &reclaim.Abandoned)
	if err != nil {
		return InboxReclaim{}, fmt.Errorf("reclaim expired webhook inbox leases: %w", err)
	}

	if reclaim.Requeued+reclaim.Abandoned != int64(len(candidates)) {
		return InboxReclaim{}, errors.New("reclaim expired webhook inbox: locked candidate count changed")
	}

	return reclaim, nil
}

type inboxReclaimCandidate struct {
	ID       int64
	Attempts int32
}

var inboxReclaimCandidatesSQL = mustSQL("inbox_reclaim_candidates.sql")

func lockedExpiredInboxCandidates(ctx context.Context, tx pgx.Tx, batchSize int32, keys []string) ([]inboxReclaimCandidate, error) {
	rows, err := tx.Query(ctx, inboxReclaimCandidatesSQL, batchSize, keys)
	if err != nil {
		return nil, fmt.Errorf("lock expired inbox candidates: %w", err)
	}

	candidates, err := pgx.CollectRows(rows, pgx.RowToStructByPos[inboxReclaimCandidate])
	if err != nil {
		return nil, fmt.Errorf("read expired inbox candidates: %w", err)
	}

	return candidates, nil
}

func (r *InboxRepository) settle(ctx context.Context, query, messageID, claimToken string) (applied bool, err error) {
	if poolErr := ensurePool(r.pool); poolErr != nil {
		return false, fmt.Errorf("ensure pool: %w", poolErr)
	}

	id, token, err := r.fenceArgs(messageID, claimToken)
	if err != nil {
		return false, fmt.Errorf("fence args: %w", err)
	}

	tx, err := r.beginLockedMessageTx(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("safe message repository error: %w", safeMessageRepositoryError("begin webhook settlement", id, err))
	}

	defer func() { err = errors.Join(err, rollbackInboxTx(ctx, tx)) }()

	applied, err = settleInboxTx(ctx, tx, query, id, token)
	if err != nil {
		return applied, fmt.Errorf("%w", err)
	}

	return applied, nil
}

func settleInboxTx(ctx context.Context, tx pgx.Tx, query, id, token string) (applied bool, err error) {
	err = tx.QueryRow(ctx, query, id, token).Scan(&applied)
	if err != nil {
		return false, fmt.Errorf("safe message repository error: %w", safeMessageRepositoryError("settle webhook inbox row", id, err))
	}

	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("safe message repository error: %w", safeMessageRepositoryError("commit webhook settlement", id, err))
	}

	return applied, nil
}

func (r *InboxRepository) fenceArgs(messageID, claimToken string) (id, token string, err error) {
	id, err = requireMessageIdentity(messageID)
	if err != nil {
		return "", "", fmt.Errorf("require message identity: %w", err)
	}

	token, err = requireBoundedIdentity("claim token", claimToken, claimTokenRuneLimit)
	if err != nil {
		return "", "", fmt.Errorf("require bounded identity: %w", err)
	}

	return id, token, nil
}

type inboxReclaimMutation struct {
	ids      []int64
	attempts []int32
	statuses []string
	terminal []bool
}

func newInboxReclaimMutation(candidates []inboxReclaimCandidate, maxAttempts int32) inboxReclaimMutation {
	var mutation inboxReclaimMutation

	mutation.ids = make([]int64, 0, len(candidates))
	mutation.attempts = make([]int32, 0, len(candidates))
	mutation.statuses = make([]string, 0, len(candidates))
	mutation.terminal = make([]bool, 0, len(candidates))

	for _, candidate := range candidates {
		status := "retry"
		exhausted := candidate.Attempts >= maxAttempts

		if exhausted {
			status = inboxStatusDead
		}

		mutation.ids = append(mutation.ids, candidate.ID)
		mutation.attempts = append(mutation.attempts, candidate.Attempts)
		mutation.statuses = append(mutation.statuses, status)
		mutation.terminal = append(mutation.terminal, exhausted)
	}

	return mutation
}
