package notificationdelivery

import (
	"context"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const testHandoffSoftClaim = 15 * time.Second

// sendForHandoff는 testRoomA의 행 하나를 운영 경로대로 요청 고정·발송 진입·발송 완료까지 옮겨 확인 대기 SENT 행으로
// 만든다.
func sendForHandoff(t *testing.T, repo *outboxFixture) int64 {
	t.Helper()

	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, testFirstPeriod, testRoomA, "message"))

	items := claimItems(ctx, t, repo, testWorkerA, 1)
	require.Len(t, items, 1)
	require.Equal(t, testFirstPeriod, items[0].PeriodKey)

	request := &deliverypayload.Request{Body: testPinnedBody, Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	require.NoError(t, repo.saveRequest(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, request))
	markOutboxSending(ctx, t, repo, &items[0])
	require.NoError(t, repo.MarkSent(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, testIrisRequestID, testNotificationHandoffHorizon))

	return items[0].ID
}

// claimDueHandoff는 행의 다음 확인 시각을 DB 시각 직전으로 당긴 뒤 handoff 확인 claim이 그 행 하나를 고르는지 확인한다.
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

// 지평이 첫 확인 간격보다 길면 발송 완료는 같은 문장의 DB 시각으로 admission 5초 뒤 첫 확인을 예약하고, 그 전에는 확인
// claim이 행을 고르지 않는다. 확인할 requestId 없는 발송 완료는 전이하지 않는다.
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

	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "no-request-id", "room-without-id", "message"))

	items := claimItems(ctx, t, repo, testWorkerA, 1)
	require.Len(t, items, 1)
	markOutboxSending(ctx, t, repo, &items[0])
	require.Error(t, repo.MarkSent(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, "", testNotificationHandoffHorizon))
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, deliveryStatusSending))
}

// 확인을 끝내는 세 결과는 확인 시각을 지우고 같은 방의 후행 행을 풀어 준다. 종료 실패는 저장된 요청을 소진으로 표시해
// producer rearm이 되살리지 않고, 격리는 자동 재발송 가능한 FAILED가 아니다.
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

			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "second", testRoomA, "message"))

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
			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, testFirstPeriod, testRoomA, "message again"))
			require.Equal(t, state.status, loadHandoffRow(t, repo, id).status)

			released, err := repo.fetchReadyAndLock(ctx, testWorkerB, 10, config.NotificationDeliveryLease, nil, nil)
			require.NoError(t, err)
			require.Len(t, released, 1)
			require.Equal(t, "second", released[0].PeriodKey)
		})
	}
}

// Iris failed 재발급은 행을 다음 세대의 PENDING으로 되돌리고 handoff 확인 상태를 비운다. 다음 claim은 같은 고정 본문을
// 새 세대 ID(:r1)로 보내고 그 발송의 requestId로 다시 확인을 예약한다.
func TestNotificationHandoffReissueResendsPinnedBodyWithNextGeneration(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "reissue", testRoomA, "source"))

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

// soft claim 토큰이 바뀐 이전 확인과 재발급 뒤의 늦은 확인은 행을 바꾸지 않는 fence 미스다. 새 토큰의 확인은 그대로
// 적용된다.
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

// 다음 확인은 DB 시계로 잰 admission + 지평을 넘기지 않는다. 확인 claim이 돌려준 경과도 같은 DB 시계다.
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
