package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/iris-client-go/v3/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
)

// handoff 적용 문장의 fence 미스 Action이자 지표 action 라벨이다.
const (
	fenceActionHandoffComplete   = "handoff_complete"
	fenceActionHandoffReschedule = "handoff_reschedule"
	fenceActionHandoffReissue    = "handoff_reissue"
	fenceActionHandoffDLQ        = "handoff_dlq"
	fenceActionHandoffQuarantine = "handoff_quarantine"
)

// handoffClaimKeyReleaseTimeout은 dlq commit 뒤 dedup claim 키 해제의 예산이다. 정산 예산과 따로 두며 runner 상태
// 기록 예산(5s)과 같다.
const handoffClaimKeyReleaseTimeout = 5 * time.Second

// HandoffClaim은 handoff 확인을 위해 soft claim한 send unit이다. CheckToken은 claim이 밀어 둔
// handoff_next_check_at이며 이후 적용 문장의 CAS 토큰이다. Elapsed는 DB 시계로 잰 admission(멤버 sent_at) 뒤
// 경과이고 sent 멤버가 없으면 0이다. AttemptCount는 sent 멤버의 attempt_count 최댓값, ClaimKeys는 sent 멤버의
// dedup claim 키다.
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
}

// HandoffRepository는 send unit handoff 확인의 claim과 적용을 저장한다.
type HandoffRepository interface {
	HandoffClaimer
	HandoffSettler
}

// HandoffClaimer는 확인 시각이 된 send unit을 soft claim한다.
type HandoffClaimer interface {
	ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error)
}

// HandoffSettler는 handoff 관측을 적용한다. 적용은 단위 하나의 단일 문장이고, fence 미스는 멤버를 바꾸지 않는
// *sendfence.MissError다. 확인 소유(토큰·요청)는 남았지만 멤버가 더 이상 모두 sent가 아니면 같은 문장이 확인을
// 닫고, 그 미스는 ErrHandoffCheckClosed로도 판별된다.
type HandoffSettler interface {
	CompleteHandoff(ctx context.Context, claim *HandoffClaim) error
	RescheduleHandoff(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) error
	ReissueAfterHandoff(ctx context.Context, claim *HandoffClaim, retryAfter time.Duration, code, detail string) error
	DLQAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error
	QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error
}

var _ HandoffRepository = (*PgxRepository)(nil)

// ErrHandoffCheckClosed는 확인 소유(토큰·요청)는 남았지만 고정 멤버가 더 이상 모두 sent가 아니어서, 적용 문장이
// 멤버를 바꾸지 않고 그 단위의 handoff 확인을 영구히 닫았다는 뜻이다. 소유 상실 미스와 달리 다음 확인이 없으므로
// 운영 SQL·관리 조치로 바뀐 멤버를 운영자가 확인해야 한다. 함께 감싼 *sendfence.MissError가 멤버별 원인이다.
var ErrHandoffCheckClosed = errors.New("handoff check closed without transition")

// ErrHandoffClaimKeysNotReleased는 재발급 예산이 없는 failed의 dlq 전이는 commit됐지만 미발송 멤버의 dedup claim 키
// 해제가 실패했다는 뜻이다. 확인이 닫혀 해제를 다시 시도할 경로가 없으므로 키는 TTL까지 남는다.
var ErrHandoffClaimKeysNotReleased = errors.New("handoff dlq committed but claim keys not released")

// ClaimHandoffDue는 확인 시각이 된 send unit을 limit개까지 고르고 softClaim 뒤로 다음 확인을 미룬다. 확인 도중
// 프로세스가 멈추면 단위는 softClaim 뒤에 다시 고를 수 있다.
func (r *PgxRepository) ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("claim handoff checks: limit must be positive: %d", limit)
	}

	softClaimMS, err := leaseMilliseconds(softClaim)
	if err != nil {
		return nil, fmt.Errorf("claim handoff checks: soft claim: %w", err)
	}

	rows, err := r.pool.Query(ctx, sqlHandoffClaimDue, limit, softClaimMS)
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
		&claim.DeliveryIDs, &claim.CheckToken, &elapsedMS, &claim.AttemptCount, &claim.ClaimKeys); err != nil {
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

// CompleteHandoff는 handoff 완료를 확인으로 기록한다. 멤버는 sent 그대로다.
func (r *PgxRepository) CompleteHandoff(ctx context.Context, claim *HandoffClaim) error {
	return r.applyHandoff(ctx, fenceActionHandoffComplete, sqlHandoffComplete, claim)
}

// RescheduleHandoff는 전이 없이 다음 확인을 DB 시계 기준 step 뒤로 예약하되 admission + horizon을 넘기지 않는다.
func (r *PgxRepository) RescheduleHandoff(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) error {
	stepMS, err := leaseMilliseconds(step)
	if err != nil {
		return fmt.Errorf("%s: step: %w", fenceActionHandoffReschedule, err)
	}

	horizonMS, err := leaseMilliseconds(horizon)
	if err != nil {
		return fmt.Errorf("%s: horizon: %w", fenceActionHandoffReschedule, err)
	}

	return r.applyHandoff(ctx, fenceActionHandoffReschedule, sqlHandoffReschedule, claim, stepMS, horizonMS)
}

// ReissueAfterHandoff는 Iris failed를 멤버 전원의 retry(attempt + 1, DB 시계 + retryAfter)와 다음 요청 세대로 한
// 문장에 기록한다. 세대를 모두 썼으면 sendfence.ErrGenerationExhausted다.
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

	return r.applyHandoff(ctx, fenceActionHandoffReissue, sqlHandoffReissue, claim,
		retryAfter.Milliseconds(), sanitizeStoredError(detail), code, nextID)
}

// DLQAfterHandoff는 재발급 예산이 없는 Iris failed를 멤버 전원의 dlq(attempt + 1)로 끝낸다.
func (r *PgxRepository) DLQAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	return r.applyHandoff(ctx, fenceActionHandoffDLQ, sqlHandoffDLQ, claim, sanitizeStoredError(detail), code)
}

// QuarantineAfterHandoff는 handoff 결과 불명을 멤버 전원의 quarantined로 끝낸다.
func (r *PgxRepository) QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	return r.applyHandoff(ctx, fenceActionHandoffQuarantine, sqlHandoffQuarantine, claim, sanitizeStoredError(detail), code)
}

// applyHandoff는 handoff fence 인자(단위·토큰·요청 ID·Iris requestId) 뒤에 전이별 인자를 붙여 적용한다. 대상은 claim
// 때의 고정 멤버 전원이다. 미스인데 확인 소유가 남았으면 같은 문장이 확인을 닫았으므로 ErrHandoffCheckClosed를 함께
// 감싼다. 잠근 멤버가 없어 관측 행이 없는 미스도 문장이 돌려준 확인 소유로 판별한다.
func (r *PgxRepository) applyHandoff(ctx context.Context, action, query string, claim *HandoffClaim, extra ...any) error {
	if claim == nil || len(claim.DeliveryIDs) == 0 {
		return fmt.Errorf("%s: claim has no pinned deliveries", action)
	}

	args := append([]any{claim.UnitID, claim.CheckToken, claim.ClientRequestID, claim.IrisRequestID}, extra...)

	rows, err := r.pool.Query(ctx, query, args...)
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

// handoffRow는 handoff 적용 문장의 결과 한 행이다. 문장은 단위의 확인 소유(토큰·요청 일치) check_held를 모든 행에
// 싣고, 잠근 멤버가 없으면 관측 열이 NULL인 행 하나로 확인 소유만 돌려준다. 그 행이 멤버 관측일 때만 member가 참이다.
type handoffRow struct {
	observation sendfence.Observation
	member      bool
	checkHeld   bool
}

// scanHandoffRow는 sendfence 관측 열 순서 뒤에 check_held가 붙은 handoff 적용 결과 한 행을 읽는다.
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

	// 멤버 행의 관측 열은 SQL이 항상 채운다. NULL이면 문장 계약 위반이라 분류하지 않고 오류로 올린다.
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

// HandoffConsumer는 handoff 확인 저장소의 claim·적용에 전이 지표와 dlq 뒤 dedup claim 키 해제를 더한다.
type HandoffConsumer struct {
	repository    HandoffRepository
	claimReleaser ClaimKeyReleaser
}

// NewHandoffConsumer는 handoff 확인 저장소가 있어야 만든다. 키 삭제를 맡을 claimReleaser가 없으면 dlq 전이는
// 기록되지만 claim 키 해제가 오류가 된다(Consumer.ReleaseClaimKeys와 같다).
func NewHandoffConsumer(repository HandoffRepository, claimReleaser ClaimKeyReleaser) (*HandoffConsumer, error) {
	if repository == nil {
		return nil, errors.New("new alarm handoff consumer: repository is required")
	}

	return &HandoffConsumer{repository: repository, claimReleaser: claimReleaser}, nil
}

// ClaimHandoffDue는 확인 시각이 된 send unit을 soft claim한다.
func (c *HandoffConsumer) ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error) {
	claims, err := c.repository.ClaimHandoffDue(ctx, limit, softClaim)
	if err != nil {
		return nil, fmt.Errorf("claim alarm handoff checks: %w", err)
	}

	return claims, nil
}

// CompleteHandoff는 handoff 완료를 확인으로 기록한다.
func (c *HandoffConsumer) CompleteHandoff(ctx context.Context, claim *HandoffClaim) error {
	if err := c.repository.CompleteHandoff(ctx, claim); err != nil {
		return fmt.Errorf("complete alarm handoff check: %w", err)
	}

	return nil
}

// RescheduleHandoff는 전이 없이 다음 확인을 예약한다.
func (c *HandoffConsumer) RescheduleHandoff(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) error {
	if err := c.repository.RescheduleHandoff(ctx, claim, step, horizon); err != nil {
		return fmt.Errorf("reschedule alarm handoff check: %w", err)
	}

	return nil
}

// ReissueAfterHandoff는 Iris failed를 다음 요청 세대의 retry로 되돌린다. 세대를 모두 썼으면
// sendfence.ErrGenerationExhausted, fence 미스는 *sendfence.MissError다.
func (c *HandoffConsumer) ReissueAfterHandoff(ctx context.Context, claim *HandoffClaim, retryAfter time.Duration, code, detail string) error {
	if err := c.repository.ReissueAfterHandoff(ctx, claim, retryAfter, code, detail); err != nil {
		return fmt.Errorf("reissue alarm request after handoff failure: %w", err)
	}

	observePGRetryScheduled(len(claim.DeliveryIDs))

	return nil
}

// DLQAfterHandoff는 재발급 예산이 없는 Iris failed를 dlq로 끝내고, commit 뒤 미발송 멤버의 dedup claim 키를 해제한다.
// 확인이 닫힌 단위는 다시 claim되지 않으므로 키 해제는 호출자의 정산 예산과 떼어 별도 예산으로 한다. 해제가 실패하면
// dlq는 기록된 채 ErrHandoffClaimKeysNotReleased다.
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

// QuarantineAfterHandoff는 handoff 결과 불명을 격리한다.
func (c *HandoffConsumer) QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	if err := c.repository.QuarantineAfterHandoff(ctx, claim, code, detail); err != nil {
		return fmt.Errorf("quarantine alarm handoff outcome: %w", err)
	}

	observePGQuarantined(len(claim.DeliveryIDs))

	return nil
}
