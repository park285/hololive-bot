package store

import (
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const testHandoffHorizon = time.Minute

// handoffTestUnit은 202로 보낸 고정 요청 하나다. 소유 행은 고정 멤버이며 follower는 같은 논리 키의 고정 멤버 밖 중복
// 물리행이다. CompleteSent가 함께 SENT로 미러링했고, 두 행의 부모 outbox 집계도 SENT다.
type handoffTestUnit struct {
	contentID  string
	roomID     string
	ownerID    int64
	followerID int64
	admission  HandoffAdmission
}

func sendHandoffTestUnit(
	t *testing.T,
	pool *pgxpool.Pool,
	transition *TransitionStore,
	contentID string,
	horizon time.Duration,
) handoffTestUnit {
	t.Helper()

	roomID := "room-" + contentID
	ownerID, followerID := seedTransitionLogicalGroup(t, pool, contentID, roomID)

	claimed, err := transition.ClaimPending(t.Context(), 10)
	require.NoError(t, err)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(t.Context(), claimed, outboxes)
	require.NoError(t, err)
	require.Len(t, prepared.ActiveRows, 1)
	require.Equal(t, ownerID, prepared.ActiveRows[0].ID)

	admission := freezeTestAdmission(t, transition, prepared.ActiveRows, "youtube:"+contentID, horizon)

	operation, begin, err := transition.BeginSending(t.Context(), prepared.ActiveRows, outboxes)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, begin.Outcome)

	completed, err := transition.CompleteSent(t.Context(), operation, DeliveryModePerRoom, admission)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, completed.Outcome)

	// dispatcher가 발송 정산 뒤 하듯 부모 outbox 집계를 SENT로 맞춘다. 실패 정산은 이 SENT 부모를 되돌려야 한다.
	require.NoError(t, NewDeliveryRepository(pool, nil).UpdateOutboxAggregateStatuses(t.Context(), completed.TouchedOutboxIDs))
	requireHandoffOutboxStatuses(t, pool, completed.TouchedOutboxIDs, domain.OutboxStatusSent)

	return handoffTestUnit{contentID: contentID, roomID: roomID, ownerID: ownerID, followerID: followerID, admission: admission}
}

// claimDueHandoff는 요청의 다음 확인을 지금 이전으로 당긴 뒤 확인 claim으로 고른다.
func claimDueHandoff(t *testing.T, pool *pgxpool.Pool, transition *TransitionStore, baseID string) HandoffClaim {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		UPDATE youtube_notification_send_request
		SET handoff_next_check_at = clock_timestamp() - INTERVAL '1 second'
		WHERE base_id = $1 AND handoff_next_check_at IS NOT NULL
	`, baseID)
	require.NoError(t, err)

	claims, err := transition.ClaimHandoffDue(t.Context(), 32, 15*time.Second)
	require.NoError(t, err)

	for i := range claims {
		if claims[i].RequestBaseID == baseID {
			return claims[i]
		}
	}

	require.FailNow(t, "handoff check was not claimed", baseID)

	return HandoffClaim{}
}

func requireHandoffRequest(t *testing.T, pool *pgxpool.Pool, baseID string) handoffRequestState {
	t.Helper()

	state, found, err := loadHandoffRequestState(t.Context(), pool, baseID)
	require.NoError(t, err)
	require.True(t, found)

	return state
}

type handoffTestDelivery struct {
	status  domain.OutboxStatus
	attempt int
	sentAt  *time.Time
	err     string
}

func loadHandoffTestDelivery(t *testing.T, pool *pgxpool.Pool, deliveryID int64) handoffTestDelivery {
	t.Helper()

	var row handoffTestDelivery

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT status, attempt_count, sent_at, COALESCE(error, '')
		FROM youtube_notification_delivery
		WHERE id = $1
	`, deliveryID).Scan(&row.status, &row.attempt, &row.sentAt, &row.err))

	return row
}

// loadHandoffTestLedger는 단위 논리 키의 원장 상태와 출처를 읽는다. 행이 없으면 빈 상태다.
func loadHandoffTestLedger(t *testing.T, pool *pgxpool.Pool, unit handoffTestUnit) (LedgerStatus, int64) {
	t.Helper()

	var (
		status LedgerStatus
		source int64
	)

	err := pool.QueryRow(t.Context(), `
		SELECT status, source_delivery_id
		FROM youtube_notification_delivery_ledger
		WHERE kind = $1 AND logical_id = $2 AND room_id = $3
	`, domain.OutboxKindNewVideo, unit.contentID, unit.roomID).Scan(&status, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0
	}

	require.NoError(t, err)

	return status, source
}

// CompleteSent는 SENT 전이와 같은 트랜잭션에서 Iris requestId와 admission(owner sent_at) + min(5s, 지평)의 첫 확인
// 시각을 고정 요청에 남긴다.
func TestCompleteSentRecordsHandoffAdmission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		horizon time.Duration
		wantMS  int64
	}{
		{name: "horizon shorter than first check", horizon: 1500 * time.Millisecond, wantMS: 1500},
		{name: "first check before horizon", horizon: testHandoffHorizon, wantMS: handoffcheck.FirstCheckDelay.Milliseconds()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			transition := newTestTransitionStore(t, pool)
			unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-admission", tc.horizon)

			var (
				requestID string
				delayMS   int64
				checkedAt *time.Time
			)

			require.NoError(t, pool.QueryRow(t.Context(), `
				SELECT r.handoff_request_id,
				       (EXTRACT(EPOCH FROM (r.handoff_next_check_at - d.sent_at)) * 1000)::BIGINT,
				       r.handoff_checked_at
				FROM youtube_notification_send_request r
				JOIN youtube_notification_delivery d ON d.id = $2
				WHERE r.base_id = $1
			`, unit.admission.RequestBaseID, unit.ownerID).Scan(&requestID, &delayMS, &checkedAt))
			require.Equal(t, unit.admission.IrisRequestID, requestID)
			require.Equal(t, tc.wantMS, delayMS)
			require.Nil(t, checkedAt)
		})
	}
}

// 고정 요청의 세대가 이번 발송과 다르면 admission을 남길 수 없으므로 SENT 전이도 원장도 남기지 않는다.
func TestCompleteSentRejectsAdmissionForAnotherGeneration(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	ownerID, _ := seedTransitionLogicalGroup(t, pool, "video-handoff-generation", "room-handoff-generation")
	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)

	admission := freezeTestAdmission(t, transition, prepared.ActiveRows, "youtube:video-handoff-generation", testHandoffHorizon)
	admission.RequestGeneration++

	operation, _, err := transition.BeginSending(ctx, prepared.ActiveRows, outboxes)
	require.NoError(t, err)

	completed, err := transition.CompleteSent(ctx, operation, DeliveryModePerRoom, admission)
	require.NoError(t, err)
	require.Equal(t, ApplyConflict, completed.Outcome)

	require.Equal(t, DeliveryStatusSending, loadHandoffTestDelivery(t, pool, ownerID).status)

	state := requireHandoffRequest(t, pool, admission.RequestBaseID)
	require.Nil(t, state.IrisRequestID)
	require.Nil(t, state.NextCheckAt)

	status, _ := loadHandoffTestLedger(t, pool, handoffTestUnit{contentID: "video-handoff-generation", roomID: "room-handoff-generation"})
	require.Empty(t, status)
}

// handoff_completed는 확인을 닫고 delivery·원장의 SENT를 그대로 둔다.
func TestHandoffCompleteClosesCheckAndKeepsSent(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-complete", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.CompleteHandoff(t.Context(), &claim)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	state := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.Nil(t, state.NextCheckAt)
	require.NotNil(t, state.CheckedAt)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.ownerID).status)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.followerID).status)

	status, source := loadHandoffTestLedger(t, pool, unit)
	require.Equal(t, LedgerStatusSent, status)
	require.Equal(t, unit.ownerID, source)
}

// 재예약은 다음 확인을 admission + 지평 이후로 미루지 않는다. 고정 멤버가 claim 뒤 바뀌었으면 멤버를 바꾸지 않고
// 확인을 닫는다.
func TestHandoffRescheduleClampsToHorizonAndClosesChangedMembers(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	horizon := 1500 * time.Millisecond
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-reschedule", horizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.RescheduleHandoff(t.Context(), &claim, time.Minute, horizon)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	var delayMS int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT (EXTRACT(EPOCH FROM (r.handoff_next_check_at - d.sent_at)) * 1000)::BIGINT
		FROM youtube_notification_send_request r
		JOIN youtube_notification_delivery d ON d.id = $2
		WHERE r.base_id = $1
	`, unit.admission.RequestBaseID, unit.ownerID).Scan(&delayMS))
	require.Equal(t, horizon.Milliseconds(), delayMS)

	claim = claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	_, err = pool.Exec(t.Context(), `UPDATE youtube_notification_delivery SET row_version = row_version + 1 WHERE id = $1`, unit.ownerID)
	require.NoError(t, err)

	_, err = transition.RescheduleHandoff(t.Context(), &claim, time.Second, horizon)
	require.ErrorIs(t, err, ErrHandoffCheckClosed)

	state := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.Nil(t, state.NextCheckAt)
	require.NotNil(t, state.CheckedAt)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.ownerID).status)
}

// Iris failed 재발급은 고정 멤버 밖 follower까지 논리 단위를 PENDING으로 되돌리고 이 발송의 SENT 원장을 지우며, 같은
// 트랜잭션에서 SENT였던 부모 outbox 집계를 PENDING으로 되돌린다. 다음 claim은 같은 고정 본문을 다음 세대 ID로 다시
// 보내고, 원장과 outbox 집계는 SENT로 수렴한다.
func TestHandoffReissueRevertsLogicalUnitAndResendsFrozenBody(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-reissue", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.ReissueAfterHandoff(ctx, &claim, handoffcheck.CodeFailed, "iris reply status failed")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Len(t, result.TouchedOutboxIDs, 2)

	owner := loadHandoffTestDelivery(t, pool, unit.ownerID)
	require.Equal(t, domain.OutboxStatusPending, owner.status)
	require.Equal(t, 1, owner.attempt)
	require.Nil(t, owner.sentAt)
	require.Equal(t, handoffcheck.CodeFailed+": iris reply status failed", owner.err)

	follower := loadHandoffTestDelivery(t, pool, unit.followerID)
	require.Equal(t, domain.OutboxStatusPending, follower.status)
	require.Zero(t, follower.attempt)

	status, _ := loadHandoffTestLedger(t, pool, unit)
	require.Empty(t, status)

	state := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.Equal(t, 1, state.Generation)
	require.Nil(t, state.IrisRequestID)
	require.Nil(t, state.NextCheckAt)
	require.Nil(t, state.CheckedAt)

	requireHandoffOutboxStatuses(t, pool, result.TouchedOutboxIDs, domain.OutboxStatusPending)

	resendHandoffTestUnit(t, pool, transition, unit, result.TouchedOutboxIDs)
}

func resendHandoffTestUnit(t *testing.T, pool *pgxpool.Pool, transition *TransitionStore, unit handoffTestUnit, outboxIDs []int64) {
	t.Helper()

	ctx := t.Context()
	aggregates := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))

	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery SET next_attempt_at = clock_timestamp() - INTERVAL '1 second'
		WHERE id = ANY($1::BIGINT[])
	`, []int64{unit.ownerID, unit.followerID})
	require.NoError(t, err)

	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)
	prepared, err := transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)
	require.Len(t, prepared.ActiveRows, 1)
	require.Equal(t, unit.ownerID, prepared.ActiveRows[0].ID)

	frozen, err := transition.FreezeRequest(ctx, prepared.ActiveRows, FrozenRequest{
		BaseID: "youtube:other-template", RoomID: unit.roomID, Message: "re-rendered body", Route: "text", DedupeKeys: []string{"other"},
	})
	require.NoError(t, err)
	require.Equal(t, unit.admission.RequestBaseID, frozen.BaseID)
	require.Equal(t, "frozen body "+unit.admission.RequestBaseID, frozen.Message)
	require.Equal(t, 1, frozen.Generation)

	requestID, err := frozen.ClientRequestID()
	require.NoError(t, err)

	wantRequestID, err := iris.ReissuedClientRequestID(unit.admission.RequestBaseID, 1)
	require.NoError(t, err)
	require.Equal(t, wantRequestID, requestID)

	operation, begin, err := transition.BeginSending(ctx, prepared.ActiveRows, outboxes)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, begin.Outcome)

	resent, err := transition.CompleteSent(ctx, operation, DeliveryModePerRoom, HandoffAdmission{
		RequestBaseID: frozen.BaseID, RequestGeneration: frozen.Generation, IrisRequestID: "iris:resent", Horizon: testHandoffHorizon,
	})
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, resent.Outcome)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.ownerID).status)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.followerID).status)

	status, source := loadHandoffTestLedger(t, pool, unit)
	require.Equal(t, LedgerStatusSent, status)
	require.Equal(t, unit.ownerID, source)
	require.NoError(t, aggregates.UpdateOutboxAggregateStatuses(ctx, resent.TouchedOutboxIDs))
	requireHandoffOutboxStatuses(t, pool, outboxIDs, domain.OutboxStatusSent)
}

// 예산이 없는 Iris failed는 논리 단위를 FAILED로 끝내고 원장을 남기지 않는다.
func TestHandoffExhaustFailsLogicalUnitWithoutLedger(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-exhaust", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.ExhaustAfterHandoff(t.Context(), &claim, handoffcheck.CodeFailed, "iris reply status failed")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	owner := loadHandoffTestDelivery(t, pool, unit.ownerID)
	require.Equal(t, domain.OutboxStatusFailed, owner.status)
	require.Equal(t, 1, owner.attempt)
	require.Equal(t, domain.OutboxStatusFailed, loadHandoffTestDelivery(t, pool, unit.followerID).status)

	status, _ := loadHandoffTestLedger(t, pool, unit)
	require.Empty(t, status)

	// 부모 집계는 같은 트랜잭션에서 FAILED가 되고, 부모 sent_at은 canonical 집계가 지우지 않으므로 앞선 SENT 시각이
	// 그대로 남는다.
	requireHandoffOutboxStatuses(t, pool, result.TouchedOutboxIDs, domain.OutboxStatusFailed)

	var keptSentAt int

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM youtube_notification_outbox WHERE id = ANY($1::BIGINT[]) AND sent_at IS NOT NULL
	`, result.TouchedOutboxIDs).Scan(&keptSentAt))
	require.Equal(t, len(result.TouchedOutboxIDs), keptSentAt)

	state := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.Zero(t, state.Generation)
	require.Nil(t, state.NextCheckAt)
	require.NotNil(t, state.CheckedAt)
}

// 결과 불명은 논리 단위를 QUARANTINED로 끝내고 이 발송의 SENT 원장을 같은 출처의 QUARANTINED 원장으로 바꾼다.
func TestHandoffQuarantineReplacesSentLedger(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-quarantine", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.QuarantineAfterHandoff(t.Context(), &claim, handoffcheck.CodeOutcomeUnknown, "iris reply status outcome_unknown")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	require.Equal(t, DeliveryStatusQuarantined, loadHandoffTestDelivery(t, pool, unit.ownerID).status)
	require.Equal(t, DeliveryStatusQuarantined, loadHandoffTestDelivery(t, pool, unit.followerID).status)

	status, source := loadHandoffTestLedger(t, pool, unit)
	require.Equal(t, LedgerStatusQuarantined, status)
	require.Equal(t, unit.ownerID, source)
	requireHandoffOutboxStatuses(t, pool, result.TouchedOutboxIDs, domain.OutboxStatusFailed)

	state := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.Nil(t, state.NextCheckAt)
	require.NotNil(t, state.CheckedAt)
}

// 다른 확인이 claim 토큰을 가져간 뒤의 정산은 아무것도 바꾸지 않은 fence 미스다.
func TestHandoffSettlementRejectsStaleClaimToken(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-stale", testHandoffHorizon)
	stale := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)
	claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.ReissueAfterHandoff(t.Context(), &stale, handoffcheck.CodeFailed, "iris reply status failed")

	miss, ok := errors.AsType[*sendfence.MissError](err)
	require.True(t, ok, "error %v is not a send fence miss", err)
	require.Equal(t, []sendfence.RowMiss{{ID: unit.ownerID, Reason: sendfence.ReasonVersionChanged}}, miss.Rows)
	require.Equal(t, ApplyConflict, result.Outcome)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.ownerID).status)

	status, _ := loadHandoffTestLedger(t, pool, unit)
	require.Equal(t, LedgerStatusSent, status)
}

// 논리 키의 원장이 다른 출처로 바뀌었으면 이 발송의 결과가 아니므로 멤버·원장을 바꾸지 않고 확인을 닫는다.
func TestHandoffSettlementClosesCheckWhenLedgerSourceChanged(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-source", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	_, err := pool.Exec(t.Context(), `
		UPDATE youtube_notification_delivery_ledger SET source_delivery_id = $1
		WHERE kind = $2 AND logical_id = $3 AND room_id = $4
	`, unit.followerID, domain.OutboxKindNewVideo, unit.contentID, unit.roomID)
	require.NoError(t, err)

	result, err := transition.QuarantineAfterHandoff(t.Context(), &claim, handoffcheck.CodeOutcomeUnknown, "iris reply status outcome_unknown")
	require.ErrorIs(t, err, ErrHandoffCheckClosed)
	require.Equal(t, ApplyConflict, result.Outcome)

	miss, ok := errors.AsType[*sendfence.MissError](err)
	require.True(t, ok)
	require.Equal(t, []sendfence.RowMiss{{
		ID: unit.ownerID, Reason: sendfence.ReasonMembershipChanged, Status: string(domain.OutboxStatusSent),
	}}, miss.Rows)

	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.ownerID).status)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.followerID).status)

	status, source := loadHandoffTestLedger(t, pool, unit)
	require.Equal(t, LedgerStatusSent, status)
	require.Equal(t, unit.followerID, source)

	state := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.Nil(t, state.NextCheckAt)
	require.NotNil(t, state.CheckedAt)
}

// 같은 claim으로 동시에 들어온 정산은 하나만 적용되고 나머지는 fence 미스다.
func TestHandoffConcurrentSettlementsApplyOnce(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-concurrent", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	var (
		wg      sync.WaitGroup
		results [2]ApplyResult
		errs    [2]error
	)

	wg.Go(func() {
		reissueClaim := claim

		results[0], errs[0] = transition.ReissueAfterHandoff(t.Context(), &reissueClaim, handoffcheck.CodeFailed, "iris reply status failed")
	})
	wg.Go(func() {
		quarantineClaim := claim

		results[1], errs[1] = transition.QuarantineAfterHandoff(t.Context(), &quarantineClaim, handoffcheck.CodeOutcomeUnknown, "iris reply status outcome_unknown")
	})
	wg.Wait()

	applied := 0

	for i := range results {
		if results[i].Outcome == ApplyApplied {
			require.NoError(t, errs[i])

			applied++

			continue
		}

		_, ok := errors.AsType[*sendfence.MissError](errs[i])
		require.True(t, ok, "error %v is not a send fence miss", errs[i])
	}

	require.Equal(t, 1, applied)
}

// 정산 commit 응답을 잃어도 고정 요청의 post-state가 기대한 재발급이면 적용으로 확인한다.
func TestHandoffReissueConfirmsLostCommitResponse(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-commit-lost", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	transition.afterCommit = func(operation string) error {
		if operation == handoffActionReissue {
			return errors.New("commit response lost")
		}

		return nil
	}

	result, err := transition.ReissueAfterHandoff(t.Context(), &claim, handoffcheck.CodeFailed, "iris reply status failed")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Equal(t, CommitConfirmedPost, result.CommitAdjudication)
	require.Equal(t, domain.OutboxStatusPending, loadHandoffTestDelivery(t, pool, unit.ownerID).status)
	require.Equal(t, 1, requireHandoffRequest(t, pool, unit.admission.RequestBaseID).Generation)
	requireHandoffOutboxStatuses(t, pool, result.TouchedOutboxIDs, domain.OutboxStatusPending)
}

func requireHandoffOutboxStatuses(t *testing.T, pool *pgxpool.Pool, outboxIDs []int64, want domain.OutboxStatus) {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT status FROM youtube_notification_outbox WHERE id = ANY($1::BIGINT[])`, outboxIDs)
	require.NoError(t, err)

	statuses, err := pgx.CollectRows(rows, pgx.RowTo[domain.OutboxStatus])
	require.NoError(t, err)
	require.Len(t, statuses, len(outboxIDs))

	for _, status := range statuses {
		require.Equal(t, want, status)
	}
}
