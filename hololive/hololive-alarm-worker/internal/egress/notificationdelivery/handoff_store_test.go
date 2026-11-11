package notificationdelivery

import (
	"context"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const testHandoffSoftClaim = 15 * time.Second

func sendForHandoff(t *testing.T, repo *outboxFixture) int64 {
	t.Helper()

	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, testFirstPeriod, testRoomA, testDeliveryMessage("message")))

	items := claimItems(ctx, t, repo, testWorkerA, 1)
	require.Len(t, items, 1)
	require.Equal(t, testFirstPeriod, items[0].PeriodKey)

	request := &deliverypayload.Request{Body: testPinnedBody, Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	require.NoError(t, repo.saveRequest(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, request))
	markOutboxSending(ctx, t, repo, &items[0])
	require.NoError(t, repo.MarkSent(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, testIrisRequestID, testNotificationHandoffHorizon))

	return items[0].ID
}

func claimDueHandoff(t *testing.T, repo *outboxFixture, id int64) HandoffClaim {
	t.Helper()

	_, err := repo.pool.Exec(t.Context(), `UPDATE notification_delivery_outbox
		SET handoff_next_check_at = clock_timestamp() - INTERVAL '1 millisecond' WHERE id = $1`, id)
	require.NoError(t, err)

	claims, err := repo.ClaimHandoffDue(t.Context(), 10, testHandoffSoftClaim)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.Equal(t, id, claims[0].ID)

	return claims[0]
}

type handoffRowState struct {
	status     string
	attempts   int
	generation int
	exhausted  bool
	errText    *string
	sentAt     *time.Time
	requestID  *string
	nextCheck  *time.Time
	checked    bool
}

func loadHandoffRow(t *testing.T, repo *outboxFixture, id int64) handoffRowState {
	t.Helper()

	var state handoffRowState

	require.NoError(t, repo.pool.QueryRow(t.Context(), `SELECT status, attempt_count, (payload->'request'->>'generation')::INT,
			COALESCE((payload->'request'->>'exhausted')::BOOLEAN, FALSE), error, sent_at, handoff_request_id,
			handoff_next_check_at, handoff_checked_at IS NOT NULL
		FROM notification_delivery_outbox WHERE id = $1`, id).
		Scan(&state.status, &state.attempts, &state.generation, &state.exhausted, &state.errText, &state.sentAt,
			&state.requestID, &state.nextCheck, &state.checked))

	return state
}

func TestNotificationHandoffMarkSentSchedulesFirstCheck(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()

	id := sendForHandoff(t, repo)

	var delayMS int64

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT floor(EXTRACT(EPOCH FROM (handoff_next_check_at - sent_at)) * 1000)::BIGINT
		FROM notification_delivery_outbox WHERE id = $1`, id).Scan(&delayMS))
	require.Equal(t, handoffcheck.FirstCheckDelay.Milliseconds(), delayMS)

	state := loadHandoffRow(t, repo, id)
	require.Equal(t, string(domain.DeliveryStatusSent), state.status)
	require.Equal(t, new(testIrisRequestID), state.requestID)
	require.False(t, state.checked)

	early, err := repo.ClaimHandoffDue(ctx, 10, testHandoffSoftClaim)
	require.NoError(t, err)
	require.Empty(t, early)

	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "no-request-id", "room-without-id", testDeliveryMessage("message")))

	items := claimItems(ctx, t, repo, testWorkerA, 1)
	require.Len(t, items, 1)
	markOutboxSending(ctx, t, repo, &items[0])
	require.Error(t, repo.MarkSent(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, "", testNotificationHandoffHorizon))
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, deliveryStatusSending))
}

func TestNotificationHandoffClosingOutcomesReleaseRoom(t *testing.T) {
	cases := []struct {
		name   string
		settle func(context.Context, *outboxFixture, *HandoffClaim) error
		check  func(*testing.T, handoffRowState)
	}{
		{name: "completed", settle: func(ctx context.Context, repo *outboxFixture, claim *HandoffClaim) error {
			return repo.CompleteHandoff(ctx, claim)
		}, check: func(t *testing.T, state handoffRowState) {
			t.Helper()
			require.Equal(t, string(domain.DeliveryStatusSent), state.status)
			require.NotNil(t, state.sentAt)
			require.Zero(t, state.attempts)
		}},
		{name: "exhausted", settle: func(ctx context.Context, repo *outboxFixture, claim *HandoffClaim) error {
			return repo.ExhaustAfterHandoff(ctx, claim, handoffcheck.CodeFailed, "iris reply status failed; reissue budget exhausted")
		}, check: func(t *testing.T, state handoffRowState) {
			t.Helper()
			require.Equal(t, string(domain.DeliveryStatusFailed), state.status)
			require.True(t, state.exhausted)
			require.Equal(t, 1, state.attempts)
			require.Equal(t, new(handoffcheck.CodeFailed+": iris reply status failed; reissue budget exhausted"), state.errText)
		}},
		{name: "quarantined", settle: func(ctx context.Context, repo *outboxFixture, claim *HandoffClaim) error {
			return repo.QuarantineAfterHandoff(ctx, claim, handoffcheck.CodeUnconfirmed, "iris handoff unconfirmed at horizon")
		}, check: func(t *testing.T, state handoffRowState) {
			t.Helper()
			require.Equal(t, string(deliveryStatusQuarantined), state.status)
			require.Nil(t, state.sentAt)
			require.Equal(t, new(handoffcheck.CodeUnconfirmed+": iris handoff unconfirmed at horizon"), state.errText)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := testRepository(t)
			ctx := t.Context()

			id := sendForHandoff(t, repo)

			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "second", testRoomA, testDeliveryMessage("message")))

			held, err := repo.fetchReadyAndLock(ctx, testWorkerB, 10, config.NotificationDeliveryLease, nil, nil)
			require.NoError(t, err)
			require.Empty(t, held)

			claim := claimDueHandoff(t, repo, id)
			require.NoError(t, tc.settle(ctx, repo, &claim))

			state := loadHandoffRow(t, repo, id)
			require.True(t, state.checked)
			require.Nil(t, state.nextCheck)
			require.Equal(t, new(testIrisRequestID), state.requestID)
			tc.check(t, state)

			// 같은 kind·기간·방의 producer 재적재는 FAILED이고 소진이 아닌 행만 되살린다.
			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, testFirstPeriod, testRoomA, testDeliveryMessage("message again")))
			require.Equal(t, state.status, loadHandoffRow(t, repo, id).status)

			released, err := repo.fetchReadyAndLock(ctx, testWorkerB, 10, config.NotificationDeliveryLease, nil, nil)
			require.NoError(t, err)
			require.Len(t, released, 1)
			require.Equal(t, "second", released[0].PeriodKey)
		})
	}
}

func TestNotificationHandoffReissueResendsPinnedBodyWithNextGeneration(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "reissue", testRoomA, testDeliveryMessage("source")))

	sender := &preparedSenderSpy{body: testPinnedBody, route: testPreparedText}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processReady(ctx)
	require.Len(t, sender.ids, 1)

	var id int64

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT id FROM notification_delivery_outbox WHERE period_key = 'reissue'`).Scan(&id))

	claim := claimDueHandoff(t, repo, id)
	require.Equal(t, preparedSpyIrisRequestID(sender.ids[0]), claim.IrisRequestID)
	require.Zero(t, claim.Generation)
	require.NoError(t, repo.ReissueAfterHandoff(ctx, &claim, 0, handoffcheck.CodeFailed, "iris reply status failed"))

	state := loadHandoffRow(t, repo, id)
	require.Equal(t, string(domain.DeliveryStatusPending), state.status)
	require.Equal(t, 1, state.generation)
	require.Equal(t, 1, state.attempts)
	require.Nil(t, state.sentAt)
	require.Nil(t, state.requestID)
	require.Nil(t, state.nextCheck)
	require.False(t, state.checked)

	sender.body = "template changed"

	d.processReady(ctx)

	reissuedID, err := iris.ReissuedClientRequestID(sender.ids[0], 1)
	require.NoError(t, err)
	require.Equal(t, []string{sender.ids[0], reissuedID}, sender.ids)
	require.Equal(t, []string{testPinnedBody, testPinnedBody}, sender.bodies)

	state = loadHandoffRow(t, repo, id)
	require.Equal(t, string(domain.DeliveryStatusSent), state.status)
	require.Equal(t, new(preparedSpyIrisRequestID(reissuedID)), state.requestID)
	require.NotNil(t, state.nextCheck)
}

func TestNotificationHandoffStaleClaimCannotApply(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()

	id := sendForHandoff(t, repo)
	stale := claimDueHandoff(t, repo, id)
	current := claimDueHandoff(t, repo, id)

	err := repo.QuarantineAfterHandoff(ctx, &stale, handoffcheck.CodeOutcomeUnknown, "iris reply status outcome_unknown")
	requireFenceMiss(t, err, actionHandoffQuarantine, sendfence.ReasonVersionChanged)

	state := loadHandoffRow(t, repo, id)
	require.Equal(t, string(domain.DeliveryStatusSent), state.status)
	require.False(t, state.checked)
	require.NotNil(t, state.nextCheck)
	require.True(t, state.nextCheck.Equal(current.CheckToken))

	require.NoError(t, repo.ReissueAfterHandoff(ctx, &current, time.Minute, handoffcheck.CodeFailed, "iris reply status failed"))
	requireFenceMiss(t, repo.CompleteHandoff(ctx, &current), actionHandoffComplete, sendfence.ReasonStatusChanged)

	state = loadHandoffRow(t, repo, id)
	require.Equal(t, string(domain.DeliveryStatusPending), state.status)
	require.Equal(t, 1, state.generation)
	require.False(t, state.checked)
}

func TestNotificationHandoffRescheduleNeverPassesHorizon(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()

	id := sendForHandoff(t, repo)

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET sent_at = clock_timestamp() - INTERVAL '30 seconds' WHERE id = $1`, id)
	require.NoError(t, err)

	claim := claimDueHandoff(t, repo, id)
	require.GreaterOrEqual(t, claim.Elapsed, 30*time.Second)
	require.Less(t, claim.Elapsed, time.Minute)

	const horizon = 40 * time.Second

	require.NoError(t, repo.RescheduleHandoff(ctx, &claim, 5*time.Minute, horizon))

	var atHorizon bool

	require.NoError(t, repo.pool.QueryRow(ctx, `SELECT handoff_next_check_at = sent_at + INTERVAL '40 seconds'
		FROM notification_delivery_outbox WHERE id = $1`, id).Scan(&atHorizon))
	require.True(t, atHorizon)
}
