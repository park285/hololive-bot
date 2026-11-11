package youtubedispatch

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/stretchr/testify/require"

	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
)

// legacyRouteSender는 DB CHECK가 아직 허용하지만 발송 경로가 거절하는 구형 route로 요청을 고정한다.
type legacyRouteSender struct {
	calls atomic.Int32
}

func (s *legacyRouteSender) PrepareMessageRequest(_ context.Context, _ string, body messagedelivery.MessageVariants) (string, string, error) {
	return body.Plain, "markdown", nil
}

func (s *legacyRouteSender) SendPreparedMessage(context.Context, string, string, string, string) (string, error) {
	s.calls.Add(1)

	return "", nil
}

func TestDispatchClaimedDeliveryRow_StoredLegacyRouteFailsWithoutProvider(t *testing.T) {
	t.Parallel()

	sender := &legacyRouteSender{}
	engine := newOutcomeUnknownTestEngine(sender, nil, time.Second)
	row := domain.YouTubeNotificationDelivery{ID: 101, OutboxID: 1, RoomID: testRoom1}
	outbox := domain.YouTubeNotificationOutbox{ID: 1, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewShort, ContentID: testShortOne, Payload: testPayloadShortOne}
	result := newDeliveryDispatchResult(1)

	attempt := newDeliveryAttempt([]claimedDelivery{{row: row, outbox: outbox}})
	engine.dispatchClaimedDeliveryRow(t.Context(), &attempt, map[int64]messagedelivery.MessageVariants{1: testMessage(testMessageHello)}, map[int64]bool{}, &result)

	require.Zero(t, sender.calls.Load(), "provider must not be called")

	wantStarted := []string{startedFailureRecord(ytlifecycle.FailurePermanent, lifecycleReasonRequest, 0, store.DeliveryModePerRoom)}
	require.Equal(t, wantStarted, engineTransitionSpy(t, engine).recordedStartedFailures())
	require.Equal(t, 1, result.FailedDeliveries)
}

func TestDispatchGroupedClaimedRows_StoredLegacyRouteFailsGroupWithoutFallback(t *testing.T) {
	t.Parallel()

	sender := &legacyRouteSender{}
	engine := newOutcomeUnknownTestEngine(sender, nil, time.Second)
	group := &deliveryGroup{roomID: testRoom1, channelID: testChannelCh1, kind: domain.OutboxKindNewShort}
	attempt := newDeliveryAttempt([]claimedDelivery{
		{
			row:    domain.YouTubeNotificationDelivery{ID: 101, OutboxID: 1, RoomID: testRoom1},
			outbox: domain.YouTubeNotificationOutbox{ID: 1, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewShort, ContentID: testShortOne, Payload: testPayloadShortOne},
		},
		{
			row:    domain.YouTubeNotificationDelivery{ID: 102, OutboxID: 2, RoomID: testRoom1},
			outbox: domain.YouTubeNotificationOutbox{ID: 2, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewShort, ContentID: testShortTwo, Payload: testPayloadShortTwo},
		},
	})
	formatted := map[int64]messagedelivery.MessageVariants{1: testMessage("쇼츠1"), 2: testMessage("쇼츠2")}
	result := newDeliveryDispatchResult(2)

	engine.dispatchGroupedClaimedRows(t.Context(), group, &attempt, testMessage("쇼츠1\n쇼츠2"), formatted, map[int64]bool{}, &result)

	require.Zero(t, sender.calls.Load(), "provider must not be called")

	wantStarted := []string{startedFailureRecord(ytlifecycle.FailurePermanent, lifecycleReasonRequest, 0, store.DeliveryModeGrouped)}
	require.Equal(t, wantStarted, engineTransitionSpy(t, engine).recordedStartedFailures())
}

func TestSendEngineFallbackWithoutLeaseBudgetSkipsProvider(t *testing.T) {
	t.Parallel()

	f := newSettlementFixture(t, 2, func() {})
	// 필요한 잔여 lease는 DeliverySendTimeout + max(2*PollInterval, 5s)다. 이 값을 LockTimeout과 같게 두면
	// grouped 발송이 쓴 시간만큼 개별 발송 예산이 모자란다.
	f.engine.config.DeliverySendTimeout = f.engine.config.LockTimeout - 5*time.Second
	f.sender.beforeReturn = func(context.Context, int) error { return iris.ErrPermanent }

	result := f.dispatch(t.Context(), t, settlementPathFallback)

	require.Len(t, f.sender.ids, 1, "only the grouped attempt may reach the provider")
	require.Empty(t, result.SuccessDeliveryIDs)
	require.Equal(t, 2, result.FailedDeliveries)

	var stored []domain.YouTubeNotificationDelivery

	require.NoError(t, findDeliveryTestRowsOrdered(f.pool, &stored, "id ASC").Error)
	require.Len(t, stored, 2)

	for i := range stored {
		require.Equal(t, domain.OutboxStatusPending, stored[i].Status, "delivery %d", stored[i].ID)
		require.Equal(t, 1, stored[i].AttemptCount, "delivery %d", stored[i].ID)
		require.Equal(t, string(lifecycleReasonLeaseBudget), stored[i].Error, "delivery %d", stored[i].ID)
	}
}

func engineTransitionSpy(t *testing.T, engine *SendEngine) *lifecycleTransitionSpy {
	t.Helper()

	spy, ok := engine.transition.(*lifecycleTransitionSpy)
	if !ok {
		t.Fatalf("transition = %T, want *lifecycleTransitionSpy", engine.transition)
	}

	return spy
}
