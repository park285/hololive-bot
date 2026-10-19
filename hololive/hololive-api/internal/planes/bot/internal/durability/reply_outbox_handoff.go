package durability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	replyOutboxSettleAcceptedSQL    = mustSQL("reply_outbox_settle_accepted.sql")
	replyOutboxHandoffClaimSQL      = mustSQL("reply_outbox_handoff_claim.sql")
	replyOutboxHandoffTerminalSQL   = mustSQL("reply_outbox_handoff_terminal.sql")
	replyOutboxHandoffSettleSQL     = mustSQL("reply_outbox_handoff_settle.sql")
	replyOutboxHandoffRequeueSQL    = mustSQL("reply_outbox_handoff_requeue.sql")
	replyOutboxHandoffRescheduleSQL = mustSQL("reply_outbox_handoff_reschedule.sql")
)

// accepted에서 handoff reconciler가 갈 수 있는 곳이다. Failed만 재발송 큐로 돌아가고, 나머지는 재발송하지
// 않는 종단이거나 payload를 보존한 수동 확인이다.
var replyHandoffSettlementPolicies = map[string]replyOutboxSettlementPolicy{
	ReplyOutboxHandoffCompleted:     {sql: replyOutboxHandoffTerminalSQL},
	ReplyOutboxDead:                 {sql: replyOutboxHandoffTerminalSQL},
	ReplyOutboxManualReview:         {sql: replyOutboxHandoffSettleSQL},
	ReplyOutboxRetryablePreDispatch: {sql: replyOutboxHandoffRequeueSQL, retry: true},
}

// ReplyHandoffClaim은 handoff 확인을 위해 soft claim한 accepted 행이다. CheckToken은 claim이 밀어 둔
// handoff_next_check_at이고 이후 전이의 CAS 토큰이다. Elapsed는 accepted_at부터 DB 시계로 잰 경과다.
type ReplyHandoffClaim struct {
	ID                   int64
	IrisRequestID        string
	CheckToken           time.Time
	Elapsed              time.Duration
	Attempts             int32
	OperatorReplayGrants int32
	ReplayHorizonExpired bool
}

// ReplyHandoffSettlement는 handoff 확인 결과에 따른 accepted 행의 전이다. RetryAfter는
// retryable_pre_dispatch에서만 쓴다.
type ReplyHandoffSettlement struct {
	Claim      ReplyHandoffClaim
	Status     string
	LastError  string
	RetryAfter time.Duration
}

// SettleAccepted는 Iris durable admission을 확인한 submitting 행을 lease 없는 accepted로 정산한다.
// 첫 handoff 확인 시각은 DB 시계 기준 firstCheck 뒤다.
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

	tag, err := r.pool.Exec(ctx, replyOutboxSettleAcceptedSQL, id, token, requestID, firstCheckMS)
	if err != nil {
		return false, fmt.Errorf("settle reply outbox row %d accepted: %w", id, err)
	}

	return tag.RowsAffected() == 1, nil
}

// ClaimHandoffDue는 확인 시각이 된 accepted 행을 SKIP LOCKED로 batchSize개까지 고르고 softClaim만큼
// 다음 확인을 미룬다. 프로세스가 확인 도중 멈추면 행은 softClaim 뒤에 다시 고를 수 있다.
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

	rows, err := r.pool.Query(ctx, replyOutboxHandoffClaimSQL, batchSize, softClaimMS, replayHorizonMS)
	if err != nil {
		return nil, fmt.Errorf("claim reply handoff checks: %w", err)
	}

	claims, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReplyHandoffClaim, error) {
		var (
			claim     ReplyHandoffClaim
			elapsedMS int64
		)

		scanErr := row.Scan(&claim.ID, &claim.IrisRequestID, &claim.CheckToken, &elapsedMS,
			&claim.Attempts, &claim.OperatorReplayGrants, &claim.ReplayHorizonExpired)

		claim.Elapsed = time.Duration(elapsedMS) * time.Millisecond

		return claim, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("read reply handoff checks: %w", err)
	}

	return claims, nil
}

// SettleHandoff는 soft claim 토큰과 관측한 Iris requestId가 그대로인 accepted 행만 전이한다. 토큰이
// 바뀐 행은 다른 확인이 이미 처리했으므로 false를 돌려준다.
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
		claim.ID, claim.CheckToken, claim.IrisRequestID, settlement.Status,
		clampColumnText(settlement.LastError, lastErrorByteLimit),
	}

	if policy.retry {
		retryMS, err := replyOutboxRetryMilliseconds(settlement.RetryAfter)
		if err != nil {
			return false, fmt.Errorf("reply handoff retry milliseconds: %w", err)
		}

		args = append(args, retryMS)
	}

	tag, err := r.pool.Exec(ctx, policy.sql, args...)
	if err != nil {
		return false, fmt.Errorf("settle reply handoff row %d: %w", claim.ID, err)
	}

	return tag.RowsAffected() == 1, nil
}

// RescheduleHandoff는 전이 없이 다음 확인을 now+step으로 잡되 accepted_at+horizon을 넘기지 않는다.
// 반환값은 DB 시계로 잰 다음 확인까지의 대기다.
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

	err = r.pool.QueryRow(ctx, replyOutboxHandoffRescheduleSQL,
		claim.ID, claim.CheckToken, claim.IrisRequestID, stepMS, horizonMS).Scan(&delayMS)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, fmt.Errorf("reschedule reply handoff row %d: %w", claim.ID, err)
	}

	return time.Duration(delayMS) * time.Millisecond, true, nil
}
