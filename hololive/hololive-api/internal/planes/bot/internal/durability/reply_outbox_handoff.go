package durability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
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
	rowVersion           int64
	ReplayHorizonExpired bool
}

type ReplyHandoffSettlement struct {
	Claim      ReplyHandoffClaim
	Status     string
	LastError  string
	RetryAfter time.Duration
}

func (r *ReplyOutboxRepository) SettleAccepted(ctx context.Context, id int64, claimToken, irisRequestID string, firstCheck time.Duration) (bool, error) {
	if err := ensureStore(r.store); err != nil {
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

	applied, err := r.execOne(ctx, replyOutboxSettleAcceptedSQL, func(now int64) []any {
		return []any{id, token, requestID, now, firstCheckMS}
	})
	if err != nil {
		return false, fmt.Errorf("settle reply outbox row %d accepted: %w", id, err)
	}

	return applied, nil
}

func (r *ReplyOutboxRepository) ClaimHandoffDue(ctx context.Context, batchSize int32, softClaim time.Duration) ([]ReplyHandoffClaim, error) {
	if err := ensureStore(r.store); err != nil {
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

	var claims []ReplyHandoffClaim

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		var claimErr error

		claims, claimErr = claimHandoffDueTx(ctx, tx, now, batchSize, softClaimMS, replayHorizonMS)

		return claimErr
	})
	if err != nil {
		return nil, fmt.Errorf("claim reply handoff checks: %w", err)
	}

	return claims, nil
}

func claimHandoffDueTx(ctx context.Context, tx *sql.Tx, now int64, batchSize int32, softClaimMS, replayHorizonMS int64) ([]ReplyHandoffClaim, error) {
	rows, err := tx.QueryContext(ctx, replyOutboxHandoffClaimSQL, now, batchSize, softClaimMS, replayHorizonMS)
	if err != nil {
		return nil, fmt.Errorf("select reply handoff checks: %w", err)
	}
	defer rows.Close()

	claims := make([]ReplyHandoffClaim, 0, batchSize)

	for rows.Next() {
		var (
			claim                ReplyHandoffClaim
			checkTokenMS, elapse int64
		)

		if err := rows.Scan(&claim.ID, &claim.IrisRequestID, &checkTokenMS, &claim.rowVersion, &elapse,
			&claim.Attempts, &claim.OperatorReplayGrants, &claim.ReplayHorizonExpired); err != nil {
			return nil, fmt.Errorf("scan reply handoff check: %w", err)
		}

		claim.CheckToken = timeFromMS(checkTokenMS)
		claim.Elapsed = time.Duration(elapse) * time.Millisecond
		claims = append(claims, claim)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read reply handoff checks: %w", err)
	}

	return claims, nil
}

func (r *ReplyOutboxRepository) SettleHandoff(ctx context.Context, settlement ReplyHandoffSettlement) (bool, error) {
	if err := ensureStore(r.store); err != nil {
		return false, err
	}

	policy, ok := replyHandoffSettlementPolicies[settlement.Status]
	if !ok {
		return false, errors.Join(ErrInvalidArgument, fmt.Errorf("unsupported reply handoff settle status %q", settlement.Status))
	}

	claim := settlement.Claim
	args := []any{
		claim.ID, claim.CheckToken.UnixMilli(), claim.IrisRequestID, claim.rowVersion, settlement.Status,
		clampColumnText(settlement.LastError, lastErrorByteLimit),
	}

	var retryMS int64

	if policy.retry {
		var err error

		retryMS, err = replyOutboxRetryMilliseconds(settlement.RetryAfter)
		if err != nil {
			return false, fmt.Errorf("reply handoff retry milliseconds: %w", err)
		}
	}

	applied, err := r.execOne(ctx, policy.sql, func(now int64) []any {
		if policy.retry {
			return append(args, now, retryMS)
		}

		return append(args, now)
	})
	if err != nil {
		return false, fmt.Errorf("settle reply handoff row %d: %w", claim.ID, err)
	}

	return applied, nil
}

func (r *ReplyOutboxRepository) RescheduleHandoff(ctx context.Context, claim ReplyHandoffClaim, step, horizon time.Duration) (time.Duration, bool, error) {
	if err := ensureStore(r.store); err != nil {
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

	var (
		delayMS     int64
		rescheduled bool
	)

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		scanErr := tx.QueryRowContext(ctx, replyOutboxHandoffRescheduleSQL,
			claim.ID, claim.CheckToken.UnixMilli(), claim.IrisRequestID, claim.rowVersion, now, stepMS, horizonMS).Scan(&delayMS)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil
		}

		if scanErr != nil {
			return fmt.Errorf("reschedule reply handoff check: %w", scanErr)
		}

		rescheduled = true

		return nil
	})
	if err != nil {
		return 0, false, fmt.Errorf("reschedule reply handoff row %d: %w", claim.ID, err)
	}

	if !rescheduled {
		return 0, false, nil
	}

	return time.Duration(delayMS) * time.Millisecond, true, nil
}
