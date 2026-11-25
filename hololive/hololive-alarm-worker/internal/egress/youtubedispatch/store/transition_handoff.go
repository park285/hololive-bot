package store

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/iris-client-go/v6/iris"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
)

const (
	handoffActionComplete   = "handoff_complete"
	handoffActionReschedule = "handoff_reschedule"
	handoffActionReissue    = "handoff_reissue"
	handoffActionExhaust    = "handoff_exhaust"
	handoffActionQuarantine = "handoff_quarantine"
	handoffActionRoomPause  = "handoff_room_pause"
)

var ErrHandoffCheckClosed = errors.New("youtube handoff check closed without transition")

type HandoffAdmission struct {
	RequestBaseID     string
	RequestGeneration int
	IrisRequestID     string
	Horizon           time.Duration
}

func (a HandoffAdmission) validate() error {
	switch {
	case strings.TrimSpace(a.RequestBaseID) == "":
		return errors.New("handoff admission: frozen request is required")
	case a.IrisRequestID == "":
		return errors.New("handoff admission: iris request id is required")
	case a.Horizon < time.Millisecond:
		return fmt.Errorf("handoff admission: horizon %s must be at least 1ms", a.Horizon)
	}

	return nil
}

func lockAdmissionRequest(ctx context.Context, tx pgxdb.Querier, operation StartedOperation, admission HandoffAdmission) error {
	rows, err := pgxdb.Query(ctx, tx, handoffAdmitLockSQL,
		admission.RequestBaseID, admission.RequestGeneration, startedOwnerIDs(operation))
	if err != nil {
		return fmt.Errorf("lock admitted request: %w", err)
	}

	locked, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("lock admitted request: scan: %w", err)
	}

	if len(locked) != 1 {
		return &transitionConflictError{
			operation: "complete sent", detail: "frozen request generation or membership differs from the started owners",
		}
	}

	return nil
}

func recordAdmission(ctx context.Context, tx pgxdb.Querier, admission HandoffAdmission, sentAt time.Time) error {
	firstCheck := min(handoffcheck.FirstCheckDelay, admission.Horizon)

	tag, err := pgxdb.Exec(ctx, tx, handoffAdmitSQL,
		admission.RequestBaseID, admission.IrisRequestID, sentAt, firstCheck.Milliseconds())
	if err != nil {
		return fmt.Errorf("record handoff admission: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("record handoff admission: updated %d frozen requests, want 1", tag.RowsAffected())
	}

	return nil
}

func validateAdmissionPostState(ctx context.Context, db pgxdb.Querier, admission HandoffAdmission) error {
	state, found, err := loadHandoffRequestState(ctx, db, admission.RequestBaseID)
	if err != nil {
		return fmt.Errorf("read-back handoff admission: %w", err)
	}

	if !found || !state.admitted(admission) {
		return &AtomicityBreachError{Operation: "complete sent", Detail: "handoff admission is absent from the frozen request"}
	}

	return nil
}

type handoffRequestState struct {
	IrisRequestID *string
	Generation    int
	NextCheckAt   *time.Time
	CheckedAt     *time.Time
	claimXID      uint32
}

func loadHandoffRequestState(ctx context.Context, db pgxdb.Querier, baseID string) (handoffRequestState, bool, error) {
	var state handoffRequestState

	err := pgxdb.QueryRow(ctx, db, handoffRequestStateSQL, baseID).
		Scan(&state.IrisRequestID, &state.Generation, &state.NextCheckAt, &state.CheckedAt, &state.claimXID)
	if errors.Is(err, pgx.ErrNoRows) {
		return handoffRequestState{}, false, nil
	}

	if err != nil {
		return handoffRequestState{}, false, fmt.Errorf("load handoff request state: %w", err)
	}

	return state, true, nil
}

func (s handoffRequestState) requestIs(id string) bool {
	return s.IrisRequestID != nil && *s.IrisRequestID == id
}

func (s handoffRequestState) admitted(admission HandoffAdmission) bool {
	return s.requestIs(admission.IrisRequestID) && s.Generation == admission.RequestGeneration &&
		(s.NextCheckAt != nil || s.CheckedAt != nil)
}

func (s handoffRequestState) heldBy(claim *HandoffClaim) bool {
	return s.claimXID == claim.claimXID && s.requestIs(claim.IrisRequestID) && s.Generation == claim.Generation &&
		s.NextCheckAt != nil && s.NextCheckAt.Equal(claim.CheckToken) && s.CheckedAt == nil
}

func (s handoffRequestState) closedBy(claim *HandoffClaim) bool {
	return s.requestIs(claim.IrisRequestID) && s.Generation == claim.Generation &&
		s.NextCheckAt == nil && s.CheckedAt != nil
}

func (s handoffRequestState) reissuedFrom(claim *HandoffClaim) bool {
	return s.IrisRequestID == nil && s.Generation == claim.Generation+1 && s.NextCheckAt == nil && s.CheckedAt == nil
}

type HandoffClaim struct {
	RequestBaseID string
	IrisRequestID string
	Generation    int
	MessageHash   string
	MemberIDs     []int64
	SentMembers   map[int64]int64
	CheckToken    time.Time
	Elapsed       time.Duration
	AttemptCount  int
	claimXID      uint32
}

func (c *HandoffClaim) validate() error {
	switch {
	case c == nil:
		return errors.New("handoff claim is required")
	case c.RequestBaseID == "" || c.IrisRequestID == "":
		return errors.New("handoff claim has no admitted request")
	case len(c.MemberIDs) == 0:
		return errors.New("handoff claim has no frozen members")
	}

	return nil
}

func (c *HandoffClaim) fenceArgs() []any {
	return []any{c.RequestBaseID, c.CheckToken, c.IrisRequestID, c.Generation, c.MessageHash, c.MemberIDs, c.claimXID}
}

func (c *HandoffClaim) sentMemberVectors() ([]int64, []int64) {
	ids := slices.Sorted(maps.Keys(c.SentMembers))
	versions := make([]int64, 0, len(ids))

	for _, id := range ids {
		versions = append(versions, c.SentMembers[id])
	}

	return ids, versions
}

func (s *TransitionStore) ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("claim youtube handoff checks: limit must be positive: %d", limit)
	}

	if softClaim < time.Millisecond {
		return nil, fmt.Errorf("claim youtube handoff checks: soft claim %s must be at least 1ms", softClaim)
	}

	rows, err := pgxdb.Query(ctx, s.db, handoffClaimDueSQL, limit, softClaim.Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("claim youtube handoff checks: %w", err)
	}

	claims, err := pgx.CollectRows(rows, scanHandoffClaim)
	if err != nil {
		return nil, fmt.Errorf("read youtube handoff checks: %w", err)
	}

	return claims, nil
}

func scanHandoffClaim(row pgx.CollectableRow) (HandoffClaim, error) {
	var (
		claim             HandoffClaim
		elapsedMS         *int64
		sentIDs, versions []int64
	)

	if err := row.Scan(&claim.RequestBaseID, &claim.IrisRequestID, &claim.Generation, &claim.MessageHash,
		&claim.MemberIDs, &claim.CheckToken, &claim.claimXID, &elapsedMS, &claim.AttemptCount, &sentIDs, &versions); err != nil {
		return HandoffClaim{}, fmt.Errorf("scan youtube handoff claim: %w", err)
	}

	if len(sentIDs) != len(versions) {
		return HandoffClaim{}, fmt.Errorf("scan youtube handoff claim: %d sent members with %d versions", len(sentIDs), len(versions))
	}

	claim.SentMembers = make(map[int64]int64, len(sentIDs))
	for i, id := range sentIDs {
		claim.SentMembers[id] = versions[i]
	}

	if elapsedMS != nil {
		claim.Elapsed = time.Duration(*elapsedMS) * time.Millisecond
	}

	return claim, nil
}

func (s *TransitionStore) CompleteHandoff(ctx context.Context, claim *HandoffClaim) (ApplyResult, error) {
	return s.applyHandoffRequest(ctx, handoffActionComplete, handoffCompleteSQL, claim)
}

func (s *TransitionStore) RescheduleHandoff(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) (ApplyResult, error) {
	if step < time.Millisecond || horizon < time.Millisecond {
		return newApplyResult(ApplyConflict, nil),
			fmt.Errorf("%s: step %s and horizon %s must be at least 1ms", handoffActionReschedule, step, horizon)
	}

	if err := claim.validate(); err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("%s: %w", handoffActionReschedule, err)
	}

	ids, versions := claim.sentMemberVectors()

	return s.applyHandoffRequest(ctx, handoffActionReschedule, handoffRescheduleSQL, claim,
		ids, versions, step.Milliseconds(), horizon.Milliseconds())
}

func (s *TransitionStore) ReissueAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) (ApplyResult, error) {
	if claim != nil && claim.Generation >= iris.ReplyReissueMaxGenerations {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("%s: %w", handoffActionReissue, sendfence.ErrGenerationExhausted)
	}

	return s.settleHandoffFailure(ctx, claim, handoffFailure{
		action: handoffActionReissue, status: lifecycle.StatusPending,
		due: lifecycle.DelayedDue(s.config.RetryBackoff, time.Time{}), reissue: true,
	}, handoffErrorText(code, detail))
}

func (s *TransitionStore) ExhaustAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) (ApplyResult, error) {
	return s.settleHandoffFailure(ctx, claim, handoffFailure{
		action: handoffActionExhaust, status: lifecycle.StatusFailed,
	}, handoffErrorText(code, detail))
}

func (s *TransitionStore) QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) (ApplyResult, error) {
	return s.settleHandoffFailure(ctx, claim, handoffFailure{
		action: handoffActionQuarantine, status: lifecycle.StatusQuarantined, quarantine: true,
	}, handoffErrorText(code, detail))
}

func (s *TransitionStore) PauseRoomAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) (ApplyResult, error) {
	return s.settleHandoffFailure(ctx, claim, handoffFailure{
		action: handoffActionRoomPause, status: lifecycle.StatusFailed, pauseRoom: true,
	}, handoffErrorText(code, detail))
}

func handoffErrorText(code, detail string) string {
	return code + ": " + detail
}

type handoffFence struct {
	present        bool
	tokenMatches   bool
	requestMatches bool
	membersMatch   bool
	applied        bool
}

func queryHandoffFence(ctx context.Context, db pgxdb.Querier, query pgxdb.Statement, args ...any) (handoffFence, error) {
	rows, err := pgxdb.Query(ctx, db, query, args...)
	if err != nil {
		return handoffFence{}, fmt.Errorf("query handoff fence: %w", err)
	}

	fences, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (handoffFence, error) {
		fence := handoffFence{present: true}
		scanErr := row.Scan(&fence.tokenMatches, &fence.requestMatches, &fence.membersMatch, &fence.applied)

		return fence, scanErr
	})
	if err != nil {
		return handoffFence{}, fmt.Errorf("scan handoff fence: %w", err)
	}

	switch len(fences) {
	case 0:
		return handoffFence{}, nil
	case 1:
		return fences[0], nil
	default:
		return handoffFence{}, fmt.Errorf("scan handoff fence: %d request rows", len(fences))
	}
}

func (f handoffFence) claimMiss(action string, claim *HandoffClaim) error {
	if f.present && f.tokenMatches && f.requestMatches {
		return nil
	}

	observed := make([]sendfence.Observation, 0, len(claim.MemberIDs))

	if f.present {
		for _, id := range claim.MemberIDs {
			observed = append(observed, sendfence.Observation{
				ID: id, StatusMatches: true, OwnerMatches: true, VersionMatches: f.tokenMatches, LeaseValid: true,
				MembershipMatches: true, MembersNonTerminal: true, RequestMatches: f.requestMatches,
			})
		}
	}

	if err := sendfence.Check(action, claim.MemberIDs, observed); err != nil {
		return fmt.Errorf("frozen request claim: %w", err)
	}

	return nil
}

func (s *TransitionStore) applyHandoffRequest(ctx context.Context, action string, query pgxdb.Statement, claim *HandoffClaim, extra ...any) (ApplyResult, error) {
	if err := claim.validate(); err != nil {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("%s: %w", action, err)
	}

	fence, err := queryHandoffFence(ctx, s.db, query, append(claim.fenceArgs(), extra...)...)
	if err != nil {
		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("%s: %w", action, err)
	}

	if miss := fence.claimMiss(action, claim); miss != nil {
		return handoffMissResult(miss), fmt.Errorf("%s: %w", action, miss)
	}

	if !fence.membersMatch {
		observed := make([]sendfence.Observation, 0, len(claim.MemberIDs))
		for _, id := range claim.MemberIDs {
			observed = append(observed, sendfence.Observation{
				ID: id, StatusMatches: true, OwnerMatches: true, VersionMatches: true, LeaseValid: true,
				MembershipMatches: true, MembersNonTerminal: false, RequestMatches: true,
			})
		}

		return newApplyResult(ApplyConflict, nil),
			fmt.Errorf("%s: %w: %w", action, ErrHandoffCheckClosed, sendfence.Check(action, claim.MemberIDs, observed))
	}

	if !fence.applied {
		return newApplyResult(ApplyIndeterminate, nil), fmt.Errorf("%s: fenced request was not updated", action)
	}

	return newApplyResult(ApplyApplied, nil), nil
}

func handoffMissResult(miss error) ApplyResult {
	if fenceMiss, ok := errors.AsType[*sendfence.MissError](miss); ok {
		return newApplyResult(fenceMissOutcome(fenceMiss), nil)
	}

	return newApplyResult(ApplyIndeterminate, nil)
}
