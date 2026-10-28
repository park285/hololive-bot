package notificationdelivery

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
)

// handoff 적용 문장의 fence 미스 Action이자 fence 미스 지표의 action label이다.
const (
	actionHandoffComplete   = "handoff_complete"
	actionHandoffReschedule = "handoff_reschedule"
	actionHandoffReissue    = "handoff_reissue"
	actionHandoffExhaust    = "handoff_exhaust"
	actionHandoffQuarantine = "handoff_quarantine"
)

// HandoffClaim은 handoff 확인을 위해 soft claim한 확인 대기 SENT 행이다. CheckToken은 claim이 밀어 둔
// handoff_next_check_at이며 이후 적용 문장의 CAS 토큰이다. Elapsed는 DB 시계로 잰 admission(sent_at) 뒤 경과다.
// Generation은 고정 요청의 세대이고 AttemptCount는 claim 때의 시도 횟수다.
type HandoffClaim struct {
	ID            int64
	IrisRequestID string
	Generation    int
	AttemptCount  int
	CheckToken    time.Time
	Elapsed       time.Duration
}

// ClaimHandoffDue는 확인 시각이 된 확인 대기 행을 limit개까지 고르고 softClaim 뒤로 다음 확인을 미룬다. 확인 도중
// 프로세스가 멈추면 행은 softClaim 뒤에 다시 고를 수 있다.
func (r *Store) ClaimHandoffDue(ctx context.Context, limit int, softClaim time.Duration) ([]HandoffClaim, error) {
	if err := r.ensurePool(); err != nil {
		return nil, fmt.Errorf("claim notification handoff checks: %w", err)
	}

	if limit <= 0 {
		return nil, fmt.Errorf("claim notification handoff checks: limit must be positive: %d", limit)
	}

	softClaimMS, err := positiveMilliseconds("handoff soft claim", softClaim)
	if err != nil {
		return nil, fmt.Errorf("claim notification handoff checks: %w", err)
	}

	rows, err := r.pool.Query(ctx, handoffClaimDueSQL, limit, softClaimMS)
	if err != nil {
		return nil, fmt.Errorf("claim notification handoff checks: %w", err)
	}

	claims, err := pgx.CollectRows(rows, scanHandoffClaim)
	if err != nil {
		return nil, fmt.Errorf("read notification handoff checks: %w", err)
	}

	return claims, nil
}

// scanHandoffClaim은 claim 결과 한 행을 읽는다. 확인 대기 행은 Iris requestId와 고정 요청 세대를 항상 가지므로 둘이
// NULL이면 문장 계약 위반이라 오류다.
func scanHandoffClaim(row pgx.CollectableRow) (HandoffClaim, error) {
	var (
		claim     HandoffClaim
		elapsedMS *int64
	)

	if err := row.Scan(&claim.ID, &claim.IrisRequestID, &claim.Generation, &claim.AttemptCount, &claim.CheckToken, &elapsedMS); err != nil {
		return HandoffClaim{}, fmt.Errorf("scan notification handoff claim: %w", err)
	}

	if elapsedMS != nil {
		claim.Elapsed = time.Duration(*elapsedMS) * time.Millisecond
	}

	return claim, nil
}

// CompleteHandoff는 handoff 완료를 확인으로 기록한다. 행은 SENT 그대로다.
func (r *Store) CompleteHandoff(ctx context.Context, claim *HandoffClaim) error {
	return r.applyHandoff(ctx, actionHandoffComplete, handoffCompleteSQL, claim)
}

// RescheduleHandoff는 전이 없이 다음 확인을 DB 시계 기준 step 뒤로 예약하되 admission + horizon을 넘기지 않는다.
func (r *Store) RescheduleHandoff(ctx context.Context, claim *HandoffClaim, step, horizon time.Duration) error {
	stepMS, err := positiveMilliseconds("handoff check step", step)
	if err != nil {
		return fmt.Errorf("%s: %w", actionHandoffReschedule, err)
	}

	horizonMS, err := positiveMilliseconds("handoff horizon", horizon)
	if err != nil {
		return fmt.Errorf("%s: %w", actionHandoffReschedule, err)
	}

	return r.applyHandoff(ctx, actionHandoffReschedule, handoffRescheduleSQL, claim, stepMS, horizonMS)
}

// ReissueAfterHandoff는 Iris failed를 다음 요청 세대의 PENDING(attempt + 1, DB 시계 + retryAfter)으로 한 문장에
// 기록한다. 다음 claim은 같은 본문을 새 세대 ID로 보낸다. 세대를 모두 썼으면 sendfence.ErrGenerationExhausted이고 행은
// 그대로다. 시도 예산은 호출자가 판정한다.
func (r *Store) ReissueAfterHandoff(ctx context.Context, claim *HandoffClaim, retryAfter time.Duration, code, detail string) error {
	if claim == nil {
		return fmt.Errorf("%s: claim is required", actionHandoffReissue)
	}

	if claim.Generation >= iris.ReplyReissueMaxGenerations {
		return sendfence.ErrGenerationExhausted
	}

	if retryAfter < 0 {
		return fmt.Errorf("%s: negative retry delay %s", actionHandoffReissue, retryAfter)
	}

	return r.applyHandoff(ctx, actionHandoffReissue, handoffReissueSQL, claim,
		durationMilliseconds(retryAfter), handoffErrorText(code, detail))
}

// ExhaustAfterHandoff는 세대나 시도 예산이 없는 Iris failed를 저장된 요청에 exhausted를 세운 종료 FAILED(attempt + 1)로
// 끝낸다. 그래서 producer rearm이 이 행을 되살리지 않는다.
func (r *Store) ExhaustAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	return r.applyHandoff(ctx, actionHandoffExhaust, handoffExhaustSQL, claim, handoffErrorText(code, detail))
}

// QuarantineAfterHandoff는 handoff 결과 불명을 QUARANTINED로 끝낸다.
func (r *Store) QuarantineAfterHandoff(ctx context.Context, claim *HandoffClaim, code, detail string) error {
	return r.applyHandoff(ctx, actionHandoffQuarantine, handoffQuarantineSQL, claim, handoffErrorText(code, detail))
}

// applyHandoff는 handoff fence 인자(행·토큰·Iris requestId·요청 세대) 뒤에 전이별 인자를 붙여 단일 문장으로 적용한다.
// 적용되지 않았으면 행을 바꾸지 않은 *sendfence.MissError다.
func (r *Store) applyHandoff(ctx context.Context, action, query string, claim *HandoffClaim, extra ...any) error {
	if claim == nil {
		return fmt.Errorf("%s: claim is required", action)
	}

	args := append([]any{claim.ID, claim.CheckToken, claim.IrisRequestID, claim.Generation}, extra...)

	return r.applyFenced(ctx, action, claim.ID, query, args...)
}

// handoffErrorText는 handoff 결과 코드를 error 열 앞에 붙인다. 이 원장에는 last_error_code 열이 없으므로 운영자는
// 접두어로 alarm dispatch와 같은 코드 어휘를 읽는다.
func handoffErrorText(code, detail string) string {
	return code + ": " + detail
}
