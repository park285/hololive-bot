package notificationdelivery

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testNotificationHandoffBackoff = time.Minute
	testNotificationMaxRetries     = 3
	testHandoffLaterPeriod         = "later"
)

type notificationStatusClient struct {
	mu       sync.Mutex
	queried  []string
	snapshot func(requestID string) (*iris.ReplyStatusSnapshot, error)
}

func (c *notificationStatusClient) GetReplyStatus(_ context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	c.mu.Lock()

	c.queried = append(c.queried, requestID)
	c.mu.Unlock()

	return c.snapshot(requestID)
}

func notificationStatus(state string) func(string) (*iris.ReplyStatusSnapshot, error) {
	return func(requestID string) (*iris.ReplyStatusSnapshot, error) {
		return &iris.ReplyStatusSnapshot{RequestID: requestID, State: state}, nil
	}
}

func newNotificationHandoffReconciler(t *testing.T, store HandoffStore, client ReplyStatusClient) *HandoffReconciler {
	t.Helper()

	reconciler, err := NewHandoffReconciler(store, client, HandoffReconcilerConfig{
		Horizon:      testNotificationHandoffHorizon,
		PollInterval: time.Second,
		MaxRetries:   testNotificationMaxRetries,
		RetryBackoff: testNotificationHandoffBackoff,
	}, dispatcherLogger())
	require.NoError(t, err)

	return reconciler
}

// sentWithLaterRow는 testRoomA에 확인 대기 SENT 행과 그 뒤에 적재된 같은 방 PENDING 행을 만들고, 확인 대기 행이 후행
// 행을 막는지 확인한다. 확인 대기 행은 admission을 age만큼 과거로 옮기고 다음 확인 시각을 DB 시각 직전으로 당긴다.
func sentWithLaterRow(t *testing.T, repo *outboxFixture, age time.Duration) int64 {
	t.Helper()

	id := sendForHandoff(t, repo)
	require.NoError(t, repo.Enqueue(t.Context(), domain.DeliveryKindMemberNewsWeekly, testHandoffLaterPeriod, testRoomA, "message"))
	require.Empty(t, readyPeriods(t, repo))

	_, err := repo.pool.Exec(t.Context(), `UPDATE notification_delivery_outbox
		SET sent_at = sent_at - $2::BIGINT * INTERVAL '1 millisecond',
		    handoff_next_check_at = clock_timestamp() - INTERVAL '1 millisecond'
		WHERE id = $1`, id, age.Milliseconds())
	require.NoError(t, err)

	return id
}

// readyPeriods는 다른 worker가 지금 claim할 수 있는 행의 기간 키다.
func readyPeriods(t *testing.T, repo *outboxFixture) []string {
	t.Helper()

	items, err := repo.fetchReadyAndLock(t.Context(), testWorkerB, 10, config.NotificationDeliveryLease, nil, nil)
	require.NoError(t, err)

	periods := make([]string, 0, len(items))

	for i := range items {
		periods = append(periods, items[i].PeriodKey)
	}

	return periods
}

// 실제 원장에서 확인 결과가 같은 방 순서에 남기는 효과다. 지평 전의 미확인(queued)은 행을 확인 대기 SENT로 두어 후행
// 행을 계속 막고, 다음 확인을 admission + 지평 안에 둔다. 지평에 닿은 미확인은 격리로 확인을 끝내 후행 행을 푼다. Iris
// failed 재발급은 행을 DB 시각 + retry backoff의 PENDING으로 되돌린다. 방 순서는 생성 순서가 아니라 기존
// next_attempt_at 순서라 이미 due인 후행 행이 먼저 나간다.
func TestNotificationHandoffReconcilerSettlesRoomOrderOnLedger(t *testing.T) {
	cases := []struct {
		name      string
		state     string
		age       time.Duration
		wantReady []string
		check     func(*testing.T, handoffRowState)
	}{
		{
			name: "queued before horizon keeps the room held", state: "queued", wantReady: []string{},
			check: func(t *testing.T, state handoffRowState) {
				t.Helper()
				require.Equal(t, string(domain.DeliveryStatusSent), state.status)
				require.False(t, state.checked)
				require.Equal(t, new(testIrisRequestID), state.requestID)
				require.NotNil(t, state.nextCheck)
				require.False(t, state.nextCheck.After(state.sentAt.Add(testNotificationHandoffHorizon)))
			},
		},
		{
			name: "queued at horizon quarantines and releases the room", state: "queued", age: testNotificationHandoffHorizon,
			wantReady: []string{testHandoffLaterPeriod}, check: func(t *testing.T, state handoffRowState) {
				t.Helper()
				require.Equal(t, string(deliveryStatusQuarantined), state.status)
				require.True(t, state.checked)
				require.Nil(t, state.nextCheck)
				require.NotNil(t, state.errText)
				require.True(t, strings.HasPrefix(*state.errText, handoffcheck.CodeUnconfirmed+": "), *state.errText)
			},
		},
		{
			name: "failed reissue re-enters by next attempt", state: "failed",
			wantReady: []string{testHandoffLaterPeriod}, check: func(t *testing.T, state handoffRowState) {
				t.Helper()
				require.Equal(t, string(domain.DeliveryStatusPending), state.status)
				require.Equal(t, 1, state.generation)
				require.Equal(t, 1, state.attempts)
				require.Nil(t, state.requestID)
				require.Nil(t, state.nextCheck)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := testRepository(t)
			id := sentWithLaterRow(t, repo, tc.age)
			client := &notificationStatusClient{snapshot: notificationStatus(tc.state)}

			claimed, err := newNotificationHandoffReconciler(t, repo.Store, client).RunOnce(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, claimed)
			require.Equal(t, []string{testIrisRequestID}, client.queried)

			tc.check(t, loadHandoffRow(t, repo, id))
			require.Equal(t, tc.wantReady, readyPeriods(t, repo))
		})
	}
}

// 종료로 끊긴 상태 조회는 관측이 아니므로 정산하지 않는다. 행은 soft claim만 잡힌 확인 대기 SENT로 남아 같은 방 후행
// 행을 계속 막고, soft claim이 끝나기 전에는 다른 확인도 고르지 않는다.
func TestNotificationHandoffReconcilerCanceledQueryLeavesLedgerUnsettled(t *testing.T) {
	repo := testRepository(t)
	id := sentWithLaterRow(t, repo, 0)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	client := &notificationStatusClient{snapshot: func(string) (*iris.ReplyStatusSnapshot, error) {
		cancel()

		return nil, context.Canceled
	}}

	claimed, err := newNotificationHandoffReconciler(t, repo.Store, client).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, claimed)

	state := loadHandoffRow(t, repo, id)
	require.Equal(t, string(domain.DeliveryStatusSent), state.status)
	require.False(t, state.checked)
	require.Equal(t, new(testIrisRequestID), state.requestID)
	require.Zero(t, state.attempts)
	require.Zero(t, state.generation)
	require.Nil(t, state.errText)

	if state.sentAt == nil || state.nextCheck == nil {
		t.Fatal("취소된 조회 뒤 admission 시각과 soft claim 토큰이 남아 있어야 한다")
	}

	require.True(t, state.nextCheck.After(state.sentAt.Add(testNotificationHandoffHorizon)),
		"조회가 취소되면 지평으로 재예약하지 않고 soft claim 토큰을 유지해야 한다")

	again, err := repo.ClaimHandoffDue(t.Context(), 10, testHandoffSoftClaim)
	require.NoError(t, err)
	require.Empty(t, again)
	require.Empty(t, readyPeriods(t, repo))
}
