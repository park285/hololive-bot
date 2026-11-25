package store

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v6/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	dbtest "github.com/kapu/hololive-dbtest"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const testHandoffHorizon = time.Minute

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

	completed, err := transition.CompleteSent(t.Context(), operation, admission)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, completed.Outcome)

	require.NoError(t, NewDeliveryRepository(pool, nil).UpdateOutboxAggregateStatuses(t.Context(), completed.TouchedOutboxIDs))
	requireHandoffOutboxStatuses(t, pool, completed.TouchedOutboxIDs, domain.OutboxStatusSent)

	return handoffTestUnit{contentID: contentID, roomID: roomID, ownerID: ownerID, followerID: followerID, admission: admission}
}

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

	completed, err := transition.CompleteSent(ctx, operation, admission)
	require.NoError(t, err)
	require.Equal(t, ApplyConflict, completed.Outcome)

	require.Equal(t, DeliveryStatusSending, loadHandoffTestDelivery(t, pool, ownerID).status)

	state := requireHandoffRequest(t, pool, admission.RequestBaseID)
	require.Nil(t, state.IrisRequestID)
	require.Nil(t, state.NextCheckAt)

	status, _ := loadHandoffTestLedger(t, pool, handoffTestUnit{contentID: "video-handoff-generation", roomID: "room-handoff-generation"})
	require.Empty(t, status)
}

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

const handoffClaimStep = 15 * time.Second

func claimScheduledHandoff(t *testing.T, pool *pgxpool.Pool, transition *TransitionStore, baseID string) HandoffClaim {
	t.Helper()

	ctx := t.Context()
	freezeTransitionClock(t, pool)

	_, err := pool.Exec(ctx, `UPDATE transition_clock SET at = r.handoff_next_check_at
		FROM youtube_notification_send_request r WHERE r.base_id = $1`, baseID)
	require.NoError(t, err)

	claims, err := transition.ClaimHandoffDue(ctx, 1, handoffClaimStep)
	require.NoError(t, err)
	require.Len(t, claims, 1)

	return claims[0]
}

func TestHandoffRescheduleRevokesClaimAtSameDeadline(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-same-deadline", testHandoffHorizon)
	claim := claimScheduledHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.RescheduleHandoff(ctx, &claim, handoffClaimStep, testHandoffHorizon)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	state := requireHandoffRequest(t, pool, claim.RequestBaseID)
	require.Equal(t, &claim.CheckToken, state.NextCheckAt)

	result, err = transition.CompleteHandoff(ctx, &claim)

	miss, ok := errors.AsType[*sendfence.MissError](err)
	require.True(t, ok, "재예약한 확인에 이전 정산이 적용됨: %v, %v", result, err)
	require.Equal(t, []sendfence.RowMiss{{ID: unit.ownerID, Reason: sendfence.ReasonVersionChanged}}, miss.Rows)
	require.Equal(t, ApplyConflict, result.Outcome)

	state = requireHandoffRequest(t, pool, claim.RequestBaseID)
	require.Equal(t, &claim.CheckToken, state.NextCheckAt)
	require.Nil(t, state.CheckedAt)

	_, err = pool.Exec(ctx, `UPDATE transition_clock SET at = $1`, claim.CheckToken)
	require.NoError(t, err)

	fresh, err := transition.ClaimHandoffDue(ctx, 1, handoffClaimStep)
	require.NoError(t, err)
	require.Len(t, fresh, 1)
	require.Equal(t, claim.RequestBaseID, fresh[0].RequestBaseID)

	result, err = transition.CompleteHandoff(ctx, &fresh[0])
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	state = requireHandoffRequest(t, pool, claim.RequestBaseID)
	require.Nil(t, state.NextCheckAt)
	require.Equal(t, &claim.CheckToken, state.CheckedAt)
}

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

func TestHandoffReissueRevertsLogicalUnitAndResendsFrozenBody(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-reissue", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.ReissueAfterHandoff(ctx, &claim, contractsalarm.HandoffCodeFailed, "iris reply status failed")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Len(t, result.TouchedOutboxIDs, 2)

	owner := loadHandoffTestDelivery(t, pool, unit.ownerID)
	require.Equal(t, domain.OutboxStatusPending, owner.status)
	require.Equal(t, 1, owner.attempt)
	require.Nil(t, owner.sentAt)
	require.Equal(t, contractsalarm.HandoffCodeFailed+": iris reply status failed", owner.err)

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

	resent, err := transition.CompleteSent(ctx, operation, HandoffAdmission{
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

func TestHandoffExhaustFailsLogicalUnitWithoutLedger(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-exhaust", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.ExhaustAfterHandoff(t.Context(), &claim, contractsalarm.HandoffCodeFailed, "iris reply status failed")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	owner := loadHandoffTestDelivery(t, pool, unit.ownerID)
	require.Equal(t, domain.OutboxStatusFailed, owner.status)
	require.Equal(t, 1, owner.attempt)
	require.Equal(t, domain.OutboxStatusFailed, loadHandoffTestDelivery(t, pool, unit.followerID).status)

	status, _ := loadHandoffTestLedger(t, pool, unit)
	require.Empty(t, status)

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

func TestHandoffQuarantineReplacesSentLedger(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-quarantine", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.QuarantineAfterHandoff(t.Context(), &claim, contractsalarm.HandoffCodeOutcomeUnknown, "iris reply status outcome_unknown")
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

func TestHandoffSettlementRejectsStaleClaimToken(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-stale", testHandoffHorizon)
	stale := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)
	claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	result, err := transition.ReissueAfterHandoff(t.Context(), &stale, contractsalarm.HandoffCodeFailed, "iris reply status failed")

	miss, ok := errors.AsType[*sendfence.MissError](err)
	require.True(t, ok, "error %v is not a send fence miss", err)
	require.Equal(t, []sendfence.RowMiss{{ID: unit.ownerID, Reason: sendfence.ReasonVersionChanged}}, miss.Rows)
	require.Equal(t, ApplyConflict, result.Outcome)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.ownerID).status)

	status, _ := loadHandoffTestLedger(t, pool, unit)
	require.Equal(t, LedgerStatusSent, status)
}

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

	result, err := transition.QuarantineAfterHandoff(t.Context(), &claim, contractsalarm.HandoffCodeOutcomeUnknown, "iris reply status outcome_unknown")
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

		results[0], errs[0] = transition.ReissueAfterHandoff(t.Context(), &reissueClaim, contractsalarm.HandoffCodeFailed, "iris reply status failed")
	})
	wg.Go(func() {
		quarantineClaim := claim

		results[1], errs[1] = transition.QuarantineAfterHandoff(t.Context(), &quarantineClaim, contractsalarm.HandoffCodeOutcomeUnknown, "iris reply status outcome_unknown")
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

type rollbackFirstHandoffDB struct {
	*pgxpool.Pool

	afterRollback func()
	attempts      int
}

func (db *rollbackFirstHandoffDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	db.attempts++

	tx, err := db.Pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}

	if db.attempts == 1 {
		return rollbackHandoffTx{Tx: tx, afterRollback: db.afterRollback}, nil
	}

	return tx, nil
}

type rollbackHandoffTx struct {
	pgx.Tx

	afterRollback func()
}

func (tx rollbackHandoffTx) Commit(ctx context.Context) error {
	if err := tx.Rollback(ctx); err != nil {
		return err
	}

	if tx.afterRollback != nil {
		tx.afterRollback()
	}

	return pgx.ErrTxCommitRollback
}

func setupHandoffRollback(t *testing.T) (*TransitionStore, *rollbackFirstHandoffDB, HandoffClaim) {
	t.Helper()

	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-rollback", testHandoffHorizon)
	claim := claimScheduledHandoff(t, pool, transition, unit.admission.RequestBaseID)
	db := &rollbackFirstHandoffDB{Pool: pool}

	transition.db = db

	return transition, db, claim
}

func TestHandoffReissueAfterRolledBackCommit(t *testing.T) {
	t.Run("original claim retries", func(t *testing.T) {
		transition, db, claim := setupHandoffRollback(t)

		result, err := transition.ReissueAfterHandoff(t.Context(), &claim, contractsalarm.HandoffCodeFailed, "iris reply status failed")
		require.NoError(t, err)
		require.Equal(t, CommitConfirmedPre, result.CommitAdjudication)
		require.Equal(t, ApplyApplied, result.Outcome)
		require.Equal(t, 2, db.attempts)
		require.Equal(t, claim.Generation+1, requireHandoffRequest(t, db.Pool, claim.RequestBaseID).Generation)
	})

	t.Run("same deadline reschedule conflicts without retry", func(t *testing.T) {
		transition, db, claim := setupHandoffRollback(t)

		db.afterRollback = func() {
			result, err := transition.RescheduleHandoff(t.Context(), &claim, handoffClaimStep, testHandoffHorizon)
			require.NoError(t, err)
			require.Equal(t, ApplyApplied, result.Outcome)
		}

		result, err := transition.ReissueAfterHandoff(t.Context(), &claim, contractsalarm.HandoffCodeFailed, "iris reply status failed")
		require.Equal(t, 1, db.attempts)
		require.ErrorIs(t, err, pgx.ErrTxCommitRollback)
		require.Equal(t, CommitConflict, result.CommitAdjudication)
		require.Equal(t, ApplyIndeterminate, result.Outcome)

		state := requireHandoffRequest(t, db.Pool, claim.RequestBaseID)
		require.Equal(t, &claim.IrisRequestID, state.IrisRequestID)
		require.Equal(t, claim.Generation, state.Generation)
		require.Equal(t, &claim.CheckToken, state.NextCheckAt)
		require.Nil(t, state.CheckedAt)
	})
}

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

	result, err := transition.ReissueAfterHandoff(t.Context(), &claim, contractsalarm.HandoffCodeFailed, "iris reply status failed")
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
