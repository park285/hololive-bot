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
	"github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

// handoff 정산의 fence 미스 Action이자 정산 트랜잭션 이름이다.
const (
	handoffActionComplete   = "handoff_complete"
	handoffActionReschedule = "handoff_reschedule"
	handoffActionReissue    = "handoff_reissue"
	handoffActionExhaust    = "handoff_exhaust"
	handoffActionQuarantine = "handoff_quarantine"
)

// ErrHandoffCheckClosed는 확인 소유(claim 토큰·고정 요청)는 남았지만 고정 멤버나 그 논리 단위가 admission 때와 달라
// 정산이 멤버·원장을 바꾸지 않고 그 요청의 handoff 확인을 영구히 닫았다는 뜻이다. 소유 상실 미스와 달리 다음 확인이
// 없으므로 운영자가 바뀐 멤버·원장을 확인해야 한다. 함께 감싼 *sendfence.MissError가 멤버별 원인이다.
var ErrHandoffCheckClosed = errors.New("youtube handoff check closed without transition")

// HandoffAdmission은 Iris 202가 durable admission을 증명한 고정 요청 발송이다. CompleteSent는 SENT 전이와 같은
// 트랜잭션에서 이 요청에 Iris requestId와 첫 handoff 확인 시각(admission + min(handoffcheck.FirstCheckDelay, Horizon))을
// 기록한다. RequestBaseID·RequestGeneration은 보낸 고정 요청이고, Horizon은 handoff reconciler와 같은 youtube_delivery
// executor attempt_timeout이다.
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

// lockAdmissionRequest는 CompleteSent 트랜잭션에서 멤버 delivery보다 먼저 고정 요청을 잠근다. 요청의 세대와 고정 멤버가
// 이번 발송의 세대·owner 전원과 다르면 SENT 전이 없이 conflict다.
func lockAdmissionRequest(ctx context.Context, tx dbx.Querier, operation StartedOperation, admission HandoffAdmission) error {
	rows, err := tx.Query(ctx, mustSQL("handoff_admit_lock.sql"),
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

// recordAdmission은 SENT 전이 문장의 DB 시계(sentAt)를 admission으로 삼아 Iris requestId와 첫 handoff 확인 시각을
// 기록한다.
func recordAdmission(ctx context.Context, tx dbx.Querier, admission HandoffAdmission, sentAt time.Time) error {
	firstCheck := min(handoffcheck.FirstCheckDelay, admission.Horizon)

	tag, err := tx.Exec(ctx, mustSQL("handoff_admit.sql"),
		admission.RequestBaseID, admission.IrisRequestID, sentAt, firstCheck.Milliseconds())
	if err != nil {
		return fmt.Errorf("record handoff admission: %w", err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("record handoff admission: updated %d frozen requests, want 1", tag.RowsAffected())
	}

	return nil
}

// validateAdmissionPostState는 commit 응답 유실 뒤 요청에 이번 admission의 requestId와 handoff 확인 상태가 남았는지
// 읽는다. 없으면 같은 트랜잭션이 기록했어야 할 결과가 빠진 것이다.
func validateAdmissionPostState(ctx context.Context, db dbx.Querier, admission HandoffAdmission) error {
	state, found, err := loadHandoffRequestState(ctx, db, admission.RequestBaseID)
	if err != nil {
		return fmt.Errorf("read-back handoff admission: %w", err)
	}

	if !found || !state.admitted(admission) {
		return &AtomicityBreachError{Operation: "complete sent", Detail: "handoff admission is absent from the frozen request"}
	}

	return nil
}

// handoffRequestState는 고정 요청의 handoff 상태이며 commit 응답 유실 판정에만 쓴다.
type handoffRequestState struct {
	IrisRequestID *string
	Generation    int
	NextCheckAt   *time.Time
	CheckedAt     *time.Time
}

func loadHandoffRequestState(ctx context.Context, db dbx.Querier, baseID string) (handoffRequestState, bool, error) {
	var state handoffRequestState

	err := db.QueryRow(ctx, mustSQL("handoff_request_state.sql"), baseID).
		Scan(&state.IrisRequestID, &state.Generation, &state.NextCheckAt, &state.CheckedAt)
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

// admitted는 admission 기록이 남았는지다. 이미 reconciler가 확인을 닫았어도 같은 requestId·세대면 기록된 것이다.
func (s handoffRequestState) admitted(admission HandoffAdmission) bool {
	return s.requestIs(admission.IrisRequestID) && s.Generation == admission.RequestGeneration &&
		(s.NextCheckAt != nil || s.CheckedAt != nil)
}

// heldBy는 claim 토큰이 그대로라 정산 트랜잭션이 commit되지 않았다는 뜻이다.
func (s handoffRequestState) heldBy(claim *HandoffClaim) bool {
	return s.requestIs(claim.IrisRequestID) && s.Generation == claim.Generation &&
		s.NextCheckAt != nil && s.NextCheckAt.Equal(claim.CheckToken) && s.CheckedAt == nil
}

// closedBy는 같은 요청·세대의 확인이 닫혔다는 뜻이다(소진·격리·멤버 변경 종료).
func (s handoffRequestState) closedBy(claim *HandoffClaim) bool {
	return s.requestIs(claim.IrisRequestID) && s.Generation == claim.Generation &&
		s.NextCheckAt == nil && s.CheckedAt != nil
}

// reissuedFrom은 claim 세대 다음 세대로 재발급되어 handoff 상태가 비었다는 뜻이다.
func (s handoffRequestState) reissuedFrom(claim *HandoffClaim) bool {
	return s.IrisRequestID == nil && s.Generation == claim.Generation+1 && s.NextCheckAt == nil && s.CheckedAt == nil
}

// HandoffClaim은 handoff 확인을 위해 soft claim한 확인 대기 고정 요청이다. CheckToken은 claim이 밀어 둔
// handoff_next_check_at이며 이후 정산의 CAS 토큰이다. IrisRequestID·Generation·MessageHash·MemberIDs는 claim 때의 요청
// 값이고 정산이 그대로인지 확인한다. SentMembers는 claim 때 이 요청에 묶인 SENT 멤버의 row_version이며 멤버를 바꾸거나
// 예약하는 정산의 행 버전 fence다. Elapsed는 DB 시계로 잰 admission(SENT 멤버의 가장 이른 sent_at) 뒤 경과이고 SENT
// 멤버가 없으면 0이다. AttemptCount는 SENT 멤버 attempt_count의 최댓값이다.
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

// fenceArgs는 handoff_request_fence.sql의 $1..$6이다.
func (c *HandoffClaim) fenceArgs() []any {
	return []any{c.RequestBaseID, c.CheckToken, c.IrisRequestID, c.Generation, c.MessageHash, c.MemberIDs}
}

// sentMemberVectors는 claim 때 SENT였던 멤버 id와 row_version을 id 순서의 두 배열로 돌려준다.
func (c *HandoffClaim) sentMemberVectors() ([]int64, []int64) {
	ids := slices.Sorted(maps.Keys(c.SentMembers))
	versions := make([]int64, 0, len(ids))

	for _, id := range ids {
		versions = append(versions, c.SentMembers[id])
	}

	return ids, versions
}

// ClaimHandoffDue는 확인 시각이 된 확인 대기 고정 요청을 limit개까지 고르고 softClaim 뒤로 다음 확인을 미룬다. 확인
// 도중 프로세스가 멈추면 요청은 softClaim 뒤에 다시 고를 수 있다. 멤버 delivery는 잠그지 않는다.
func (s *TransitionStore) ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("claim youtube handoff checks: limit must be positive: %d", limit)
	}

	if softClaim < time.Millisecond {
		return nil, fmt.Errorf("claim youtube handoff checks: soft claim %s must be at least 1ms", softClaim)
	}

	rows, err := s.db.Query(ctx, mustSQL("handoff_claim_due.sql"), limit, softClaim.Milliseconds())
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
		&claim.MemberIDs, &claim.CheckToken, &elapsedMS, &claim.AttemptCount, &sentIDs, &versions); err != nil {
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

// CompleteHandoff는 handoff 완료를 확인으로 기록한다. 멤버 delivery와 SENT 원장은 그대로다.
func (s *TransitionStore) CompleteHandoff(ctx context.Context, claim *HandoffClaim) (ApplyResult, error) {
	return s.applyHandoffRequest(ctx, handoffActionComplete, handoffCompleteSQL, claim)
}

// RescheduleHandoff는 전이 없이 다음 확인을 DB 시계 기준 step 뒤로 예약하되 admission + horizon을 넘기지 않는다. 다만
// claim 때 SENT였던 멤버가 고정 멤버 전원이 아니거나 그 뒤 바뀌었으면 확인을 닫고 ErrHandoffCheckClosed다.
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

// ReissueAfterHandoff는 Iris failed를 다음 요청 세대의 재시도로 되돌린다. 한 트랜잭션에서 고정 멤버와 그 논리 그룹의
// SENT delivery 전원을 PENDING(DB 시계 + retry backoff, owner attempt + 1)으로, 이 발송이 남긴 SENT 원장을 삭제로,
// 요청을 세대 + 1과 빈 handoff 상태로 기록하고 바뀐 delivery의 부모 outbox 집계를 다시 계산한다. 다음 claim은 같은
// 본문을 새 세대 ID로 보낸다. 세대를 모두 썼으면 sendfence.ErrGenerationExhausted이고 아무것도 바꾸지 않는다. 시도
// 예산은 호출자가 판정한다.
func (s *TransitionStore) ReissueAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) (ApplyResult, error) {
	if claim != nil && claim.Generation >= iris.ReplyReissueMaxGenerations {
		return newApplyResult(ApplyConflict, nil), fmt.Errorf("%s: %w", handoffActionReissue, sendfence.ErrGenerationExhausted)
	}

	return s.settleHandoffFailure(ctx, claim, handoffFailure{
		action: handoffActionReissue, status: lifecycle.StatusPending,
		due: lifecycle.DelayedDue(s.config.RetryBackoff, time.Time{}), reissue: true,
	}, handoffErrorText(code, detail))
}

// ExhaustAfterHandoff는 세대나 시도 예산이 없는 Iris failed를 끝낸다. 한 트랜잭션에서 고정 멤버와 그 논리 그룹의 SENT
// delivery 전원을 FAILED(owner attempt + 1)로, 이 발송이 남긴 SENT 원장을 삭제로 기록하고 확인을 닫은 뒤 부모 outbox
// 집계를 다시 계산한다. FAILED는 provider 미전달 확정이라 원장에 남기지 않는다.
func (s *TransitionStore) ExhaustAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) (ApplyResult, error) {
	return s.settleHandoffFailure(ctx, claim, handoffFailure{
		action: handoffActionExhaust, status: lifecycle.StatusFailed,
	}, handoffErrorText(code, detail))
}

// QuarantineAfterHandoff는 handoff 결과 불명(outcome_unknown·모르는 상태·404·지평 미확인)을 끝낸다. 한 트랜잭션에서
// 고정 멤버와 그 논리 그룹의 SENT delivery 전원을 QUARANTINED로, 이 발송이 남긴 SENT 원장을 같은 출처의 QUARANTINED
// 원장으로 바꾸고 확인을 닫은 뒤 부모 outbox 집계를 다시 계산한다. 자동 재발송하지 않는다.
func (s *TransitionStore) QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) (ApplyResult, error) {
	return s.settleHandoffFailure(ctx, claim, handoffFailure{
		action: handoffActionQuarantine, status: lifecycle.StatusQuarantined, quarantine: true,
	}, handoffErrorText(code, detail))
}

// handoffErrorText는 handoff 결과 코드를 error 열 앞에 붙인다. 이 원장에는 last_error_code 열이 없으므로 운영자는
// 접두어로 alarm dispatch와 같은 코드 어휘를 읽는다.
func handoffErrorText(code, detail string) string {
	return code + ": " + detail
}

// handoffFence는 handoff 적용 문장이 잠근 고정 요청에서 계산한 조건값이며 present는 요청 행이 있는지다.
type handoffFence struct {
	present        bool
	tokenMatches   bool
	requestMatches bool
	membersMatch   bool
	applied        bool
}

func queryHandoffFence(ctx context.Context, db dbx.Querier, query string, args ...any) (handoffFence, error) {
	rows, err := db.Query(ctx, query, args...)
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

// claimMiss는 claim 소유(요청 행, 토큰, 고정 요청 값)를 잃었으면 고정 멤버 전원을 담은 *sendfence.MissError를 감싸
// 돌려준다. 요청이 없으면 missing, 토큰이 바뀌었으면 version_changed, 요청 값이 바뀌었으면 request_changed로 분류한다.
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

// applyHandoffRequest는 멤버를 바꾸지 않는 정산(완료·재예약)을 요청 행 하나의 단일 문장으로 적용한다.
func (s *TransitionStore) applyHandoffRequest(ctx context.Context, action, query string, claim *HandoffClaim, extra ...any) (ApplyResult, error) {
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
