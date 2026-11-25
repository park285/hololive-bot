package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
)

const (
	fenceActionHandoffComplete   = "handoff_complete"
	fenceActionHandoffReschedule = "handoff_reschedule"
	fenceActionHandoffReissue    = "handoff_reissue"
	fenceActionHandoffDLQ        = "handoff_dlq"
	fenceActionHandoffQuarantine = "handoff_quarantine"
)

const handoffClaimKeyReleaseTimeout = 5 * time.Second

type HandoffClaim struct {
	UnitID              int64
	IrisRequestID       string
	ClientRequestID     string
	BaseClientRequestID string
	Generation          int
	DeliveryIDs         []int64
	CheckToken          time.Time
	Elapsed             time.Duration
	AttemptCount        int
	ClaimKeys           []string
	claimXID            uint32
}

type HandoffRepository interface {
	HandoffClaimer
	HandoffSettler
}

type HandoffClaimer interface {
	ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error)
}

type HandoffSettler interface {
	CompleteHandoff(ctx context.Context, claim *HandoffClaim) error
	RescheduleHandoff(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) error
	ReissueAfterHandoff(ctx context.Context, claim *HandoffClaim, retryAfter time.Duration, code, detail string) error
	DLQAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error
	QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error
}

var _ HandoffRepository = (*PgxRepository)(nil)

var ErrHandoffCheckClosed = errors.New("handoff check closed without transition")

var ErrHandoffClaimKeysNotReleased = errors.New("handoff dlq committed but claim keys not released")

func (r *PgxRepository) ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("claim handoff checks: limit must be positive: %d", limit)
	}

	softClaimMS, err := leaseMilliseconds(softClaim)
	if err != nil {
		return nil, fmt.Errorf("claim handoff checks: soft claim: %w", err)
	}

	rows, err := pgxdb.Query(ctx, r.pool, handoffClaimDueSQL, limit, softClaimMS)
	if err != nil {
		return nil, fmt.Errorf("claim handoff checks: %w", err)
	}

	claims, err := pgx.CollectRows(rows, scanHandoffClaim)
	if err != nil {
		return nil, fmt.Errorf("read handoff checks: %w", err)
	}

	return claims, nil
}

func scanHandoffClaim(row pgx.CollectableRow) (HandoffClaim, error) {
	var (
		claim     HandoffClaim
		baseID    *string
		elapsedMS *int64
	)

	if err := row.Scan(&claim.UnitID, &claim.IrisRequestID, &claim.ClientRequestID, &baseID, &claim.Generation,
		&claim.DeliveryIDs, &claim.CheckToken, &claim.claimXID, &elapsedMS, &claim.AttemptCount, &claim.ClaimKeys); err != nil {
		return HandoffClaim{}, fmt.Errorf("scan handoff claim: %w", err)
	}

	if baseID != nil {
		claim.BaseClientRequestID = *baseID
	}

	if elapsedMS != nil {
		claim.Elapsed = time.Duration(*elapsedMS) * time.Millisecond
	}

	return claim, nil
}

func (r *PgxRepository) CompleteHandoff(ctx context.Context, claim *HandoffClaim) error {
	return r.applyHandoff(ctx, fenceActionHandoffComplete, handoffCompleteSQL, claim)
}

func (r *PgxRepository) RescheduleHandoff(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) error {
	stepMS, err := leaseMilliseconds(step)
	if err != nil {
		return fmt.Errorf("%s: step: %w", fenceActionHandoffReschedule, err)
	}

	horizonMS, err := leaseMilliseconds(horizon)
	if err != nil {
		return fmt.Errorf("%s: horizon: %w", fenceActionHandoffReschedule, err)
	}

	return r.applyHandoff(ctx, fenceActionHandoffReschedule, handoffRescheduleSQL, claim, stepMS, horizonMS)
}

func (r *PgxRepository) ReissueAfterHandoff(ctx context.Context, claim *HandoffClaim, retryAfter time.Duration, code, detail string) error {
	if claim.Generation >= iris.ReplyReissueMaxGenerations {
		return sendfence.ErrGenerationExhausted
	}

	if retryAfter < 0 {
		return fmt.Errorf("%s: negative retry delay %s", fenceActionHandoffReissue, retryAfter)
	}

	nextID, err := iris.ReissuedClientRequestID(claim.BaseClientRequestID, claim.Generation+1)
	if err != nil {
		return fmt.Errorf("%s: derive reissue request: %w", fenceActionHandoffReissue, err)
	}

	return r.applyHandoff(ctx, fenceActionHandoffReissue, handoffReissueSQL, claim,
		retryAfter.Milliseconds(), sanitizeStoredError(detail), code, nextID)
}

func (r *PgxRepository) DLQAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	return r.applyHandoff(ctx, fenceActionHandoffDLQ, handoffDLQSQL, claim, sanitizeStoredError(detail), code)
}

func (r *PgxRepository) QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	return r.applyHandoff(ctx, fenceActionHandoffQuarantine, handoffQuarantineSQL, claim, sanitizeStoredError(detail), code)
}

func (r *PgxRepository) applyHandoff(ctx context.Context, action string, query pgxdb.Statement, claim *HandoffClaim, extra ...any) error {
	if claim == nil || len(claim.DeliveryIDs) == 0 {
		return fmt.Errorf("%s: claim has no pinned deliveries", action)
	}

	args := append([]any{claim.UnitID, claim.CheckToken, claim.ClientRequestID, claim.IrisRequestID, claim.claimXID}, extra...)

	rows, err := pgxdb.Query(ctx, r.pool, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	results, err := pgx.CollectRows(rows, scanHandoffRow)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	observed := make([]sendfence.Observation, 0, len(results))
	held := false

	for i := range results {
		held = results[i].checkHeld

		if results[i].member {
			observed = append(observed, results[i].observation)
		}
	}

	err = r.checkFenced(action, claim.DeliveryIDs, observed)
	if err != nil && held {
		return fmt.Errorf("%w: %w", ErrHandoffCheckClosed, err)
	}

	return err
}

type handoffRow struct {
	observation sendfence.Observation
	member      bool
	checkHeld   bool
}

func scanHandoffRow(row pgx.CollectableRow) (handoffRow, error) {
	var (
		result                                    handoffRow
		id                                        *int64
		status                                    *string
		statusMatches, versionMatches, membership *bool
		membersNonTerminal, requestMatches        *bool
	)

	o := &result.observation
	if err := row.Scan(&id, &status, &statusMatches, &o.OwnerMatches, &versionMatches, &o.LeaseValid,
		&membership, &membersNonTerminal, &requestMatches, &o.Applied, &result.checkHeld); err != nil {
		return handoffRow{}, fmt.Errorf("scan handoff observation: %w", err)
	}

	if id == nil {
		return handoffRow{checkHeld: result.checkHeld}, nil
	}

	if status == nil || statusMatches == nil || versionMatches == nil || membership == nil ||
		membersNonTerminal == nil || requestMatches == nil {
		return handoffRow{}, fmt.Errorf("scan handoff observation: delivery %d: member row has NULL observation columns", *id)
	}

	result.member = true
	o.ID, o.Status = *id, *status
	o.StatusMatches, o.VersionMatches, o.MembershipMatches = *statusMatches, *versionMatches, *membership
	o.MembersNonTerminal, o.RequestMatches = *membersNonTerminal, *requestMatches

	return result, nil
}

type HandoffConsumer struct {
	repository    HandoffRepository
	claimReleaser ClaimKeyReleaser
}

func NewHandoffConsumer(repository HandoffRepository, claimReleaser ClaimKeyReleaser) (*HandoffConsumer, error) {
	if repository == nil {
		return nil, errors.New("new alarm handoff consumer: repository is required")
	}

	return &HandoffConsumer{repository: repository, claimReleaser: claimReleaser}, nil
}

func (c *HandoffConsumer) ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error) {
	claims, err := c.repository.ClaimHandoffDue(ctx, limit, softClaim)
	if err != nil {
		return nil, fmt.Errorf("claim alarm handoff checks: %w", err)
	}

	return claims, nil
}

func (c *HandoffConsumer) CompleteHandoff(ctx context.Context, claim *HandoffClaim) error {
	if err := c.repository.CompleteHandoff(ctx, claim); err != nil {
		return fmt.Errorf("complete alarm handoff check: %w", err)
	}

	return nil
}

func (c *HandoffConsumer) RescheduleHandoff(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) error {
	if err := c.repository.RescheduleHandoff(ctx, claim, step, horizon); err != nil {
		return fmt.Errorf("reschedule alarm handoff check: %w", err)
	}

	return nil
}

func (c *HandoffConsumer) ReissueAfterHandoff(ctx context.Context, claim *HandoffClaim, retryAfter time.Duration, code, detail string) error {
	if err := c.repository.ReissueAfterHandoff(ctx, claim, retryAfter, code, detail); err != nil {
		return fmt.Errorf("reissue alarm request after handoff failure: %w", err)
	}

	observePGRetryScheduled(len(claim.DeliveryIDs))

	return nil
}

func (c *HandoffConsumer) DLQAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	if err := c.repository.DLQAfterHandoff(ctx, claim, code, detail); err != nil {
		return fmt.Errorf("move alarm handoff failure to dlq: %w", err)
	}

	observePGDLQ(len(claim.DeliveryIDs))

	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), handoffClaimKeyReleaseTimeout)
	defer cancel()

	if err := releaseClaimKeys(releaseCtx, c.claimReleaser, claim.ClaimKeys); err != nil {
		return fmt.Errorf("release alarm handoff dlq claim keys: %w: %w", ErrHandoffClaimKeysNotReleased, err)
	}

	return nil
}

func (c *HandoffConsumer) QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	if err := c.repository.QuarantineAfterHandoff(ctx, claim, code, detail); err != nil {
		return fmt.Errorf("quarantine alarm handoff outcome: %w", err)
	}

	observePGQuarantined(len(claim.DeliveryIDs))

	return nil
}
