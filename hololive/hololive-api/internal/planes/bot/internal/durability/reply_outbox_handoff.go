package durability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
)

var replyHandoffSettlementPolicies = map[string]replyOutboxSettlementPolicy{
	ReplyOutboxHandoffCompleted:     {sql: replyOutboxHandoffTerminalSQL},
	ReplyOutboxDead:                 {sql: replyOutboxHandoffTerminalSQL},
	ReplyOutboxManualReview:         {sql: replyOutboxHandoffSettleSQL},
	ReplyOutboxRetryablePreDispatch: {sql: replyOutboxHandoffRequeueSQL, retry: true},
}

type ReplyHandoffClaim struct {
	ID                   int64
	IrisRequestID        string
	CheckToken           time.Time
	Elapsed              time.Duration
	Attempts             int32
	OperatorReplayGrants int32
	claimXID             uint32
	ReplayHorizonExpired bool
}

type ReplyHandoffSettlement struct {
	Claim      ReplyHandoffClaim
	Status     string
	LastError  string
	RetryAfter time.Duration
}

func (r *ReplyOutboxRepository) SettleAccepted(ctx context.Context, id int64, claimToken, irisRequestID string, firstCheck time.Duration) (bool, error) {
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

	firstCheckMS, err := leaseMilliseconds(firstCheck)
	if err != nil {
		return false, fmt.Errorf("first handoff check delay: %w", err)
	}

	tag, err := pgxdb.Exec(ctx, r.pool, replyOutboxSettleAcceptedSQL, id, token, requestID, firstCheckMS)
	if err != nil {
		return false, fmt.Errorf("settle reply outbox row %d accepted: %w", id, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *ReplyOutboxRepository) ClaimHandoffDue(ctx context.Context, batchSize int32, softClaim time.Duration) ([]ReplyHandoffClaim, error) {
	if err := ensurePool(r.pool); err != nil {
		return nil, err
	}

	if batchSize <= 0 {
		return nil, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	softClaimMS, err := leaseMilliseconds(softClaim)
	if err != nil {
		return nil, fmt.Errorf("handoff soft claim: %w", err)
	}

	replayHorizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return nil, err
	}

	rows, err := pgxdb.Query(ctx, r.pool, replyOutboxHandoffClaimSQL, batchSize, softClaimMS, replayHorizonMS)
	if err != nil {
		return nil, fmt.Errorf("claim reply handoff checks: %w", err)
	}

	claims, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReplyHandoffClaim, error) {
		var (
			claim     ReplyHandoffClaim
			elapsedMS int64
		)

		scanErr := row.Scan(&claim.ID, &claim.IrisRequestID, &claim.CheckToken, &claim.claimXID, &elapsedMS,
			&claim.Attempts, &claim.OperatorReplayGrants, &claim.ReplayHorizonExpired)

		claim.Elapsed = time.Duration(elapsedMS) * time.Millisecond

		return claim, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("read reply handoff checks: %w", err)
	}

	return claims, nil
}

func (r *ReplyOutboxRepository) SettleHandoff(ctx context.Context, settlement ReplyHandoffSettlement) (bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return false, err
	}

	policy, ok := replyHandoffSettlementPolicies[settlement.Status]
	if !ok {
		return false, errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported reply handoff settle status %q", settlement.Status))
	}

	claim := settlement.Claim
	args := []any{
		claim.ID, claim.CheckToken, claim.IrisRequestID, claim.claimXID, settlement.Status,
		clampColumnText(settlement.LastError, lastErrorByteLimit),
	}

	if policy.retry {
		retryMS, err := replyOutboxRetryMilliseconds(settlement.RetryAfter)
		if err != nil {
			return false, fmt.Errorf("reply handoff retry milliseconds: %w", err)
		}

		args = append(args, retryMS)
	}

	tag, err := pgxdb.Exec(ctx, r.pool, policy.sql, args...)
	if err != nil {
		return false, fmt.Errorf("settle reply handoff row %d: %w", claim.ID, err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *ReplyOutboxRepository) RescheduleHandoff(ctx context.Context, claim ReplyHandoffClaim, step, horizon time.Duration) (time.Duration, bool, error) {
	if err := ensurePool(r.pool); err != nil {
		return 0, false, err
	}

	stepMS, err := leaseMilliseconds(step)
	if err != nil {
		return 0, false, fmt.Errorf("handoff check step: %w", err)
	}

	horizonMS, err := leaseMilliseconds(horizon)
	if err != nil {
		return 0, false, fmt.Errorf("handoff horizon: %w", err)
	}

	var delayMS int64

	err = pgxdb.QueryRow(ctx, r.pool, replyOutboxHandoffRescheduleSQL,
		claim.ID, claim.CheckToken, claim.IrisRequestID, claim.claimXID, stepMS, horizonMS).Scan(&delayMS)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, fmt.Errorf("reschedule reply handoff row %d: %w", claim.ID, err)
	}

	return time.Duration(delayMS) * time.Millisecond, true, nil
}
