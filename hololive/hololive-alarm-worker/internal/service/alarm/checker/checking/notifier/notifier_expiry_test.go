package notifier

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type expiryClaimCache struct {
	cache.Client

	released []string
}

func (*expiryClaimCache) SetNX(context.Context, string, string, time.Duration) (bool, error) {
	time.Sleep(2 * time.Second)

	return true, nil
}

func (c *expiryClaimCache) DelMany(_ context.Context, keys []string) (int64, error) {
	c.released = append(c.released, keys...)
	return int64(len(keys)), nil
}

func TestUpcomingExpiresDuringDedupClaim(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now().Add(time.Second)
		item := newNotifierPublishTestItem("expiry-room", "expiry-stream", "expiry-channel", start, 5, nil)
		outbox := &notifierBatchOutbox{}
		logger := newCheckerTestLogger()
		claimCache := &expiryClaimCache{}
		n, err := NewNotifier(dedup.NewService(claimCache, []int{5}, logger), queue.NewPublisher(claimCache, logger, queue.WithOutbox(outbox), queue.WithWakeupEnabled(false)), nil, logger)
		require.NoError(t, err)

		result, err := n.Send(t.Context(), []*domain.AlarmNotification{item.payload.notification})
		require.NoError(t, err)
		require.Zero(t, outbox.insertBatchCalls)
		require.Equal(t, SendResult{Skipped: 1}, result)
		require.NotEmpty(t, claimCache.released)
	})
}

func TestPublishPreparedBatchSkipsExpiredAndKeepsActiveReceipt(t *testing.T) {
	ctx := t.Context()
	now := time.Now()
	expired := newNotifierPublishTestItem("expired-room", "expired-stream", "expiry-channel", now.Add(-time.Second), 5, []string{"expired-claim"})
	active := newNotifierPublishTestItem("active-room", "active-stream", "expiry-channel", now.Add(time.Minute), 5, []string{"active-claim"})
	cacheClient := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	service := dedup.NewService(cacheClient, []int{5}, logger)
	outbox := &notifierBatchOutbox{}
	n, err := NewNotifier(service, queue.NewPublisher(cacheClient, logger, queue.WithOutbox(outbox), queue.WithWakeupEnabled(false)), nil, logger)
	require.NoError(t, err)
	require.NoError(t, cacheClient.Set(ctx, "expired-claim", "1", time.Minute))
	require.NoError(t, cacheClient.Set(ctx, "active-claim", "1", time.Minute))

	result := SendResult{}
	errs := n.publishPreparedBatch(ctx, []claimedSend{expired, active}, &result, nil)
	require.Empty(t, errs)
	require.Equal(t, SendResult{Sent: 1, Skipped: 1}, result)
	require.Len(t, outbox.lastBatchInput.Envelopes, 1)
	require.Equal(t, "active-room", outbox.lastBatchInput.Envelopes[0].Notification.RoomID)

	expiredHeld, err := cacheClient.Exists(ctx, "expired-claim")
	require.NoError(t, err)
	require.False(t, expiredHeld)

	activeHeld, err := cacheClient.Exists(ctx, "active-claim")
	require.NoError(t, err)
	require.True(t, activeHeld)

	for _, item := range []claimedSend{expired, active} {
		marked, markErr := service.WasUpcomingEventNotifiedRecently(ctx, item.payload.notification.RoomID, item.payload.channelID, item.payload.notification.Stream, time.Hour)
		require.NoError(t, markErr)
		require.Equal(t, item.payload.notification.RoomID == "active-room", marked)
	}
}
