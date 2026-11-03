package youtubedispatch

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestDispatcher_ProcessAvailable_DrainsMultipleRounds(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	cache := cachemocks.NewLenientClient()

	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == alarmkeys.BuildChannelSubscriberKey("UCdrain", domain.AlarmTypeLive) {
			return []string{"room-drain"}, nil
		}

		return nil, nil
	}

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := newDispatcherForTest(t, db, cache, sender, nil, slog.New(slog.DiscardHandler), &Config{
		BatchSize:           1,
		LockTimeout:         time.Minute,
		PollInterval:        time.Hour,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.telemetry = nil

	now := time.Now()

	for _, contentID := range []string{"drain-1", "drain-2", "drain-3"} {
		require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationOutbox{
			Kind:          domain.OutboxKindNewVideo,
			ChannelID:     "UCdrain",
			ContentID:     contentID,
			Payload:       `{"video_id":"` + contentID + `","title":"drain title"}`,
			Status:        domain.OutboxStatusPending,
			AttemptCount:  0,
			NextAttemptAt: now,
		}).Error)
	}

	require.True(t, dispatcher.processAvailable(ctx, 4))

	sender.mu.Lock()
	require.Len(t, sender.messages, 3)
	sender.mu.Unlock()

	var sentCount int64

	require.NoError(t, countDeliveryTestRowsWhere(db, &deliveryTestOutboxModel{}, &sentCount, "status = $1", string(domain.OutboxStatusSent)).Error)
	require.EqualValues(t, 3, sentCount)
}

func TestDispatcher_ProcessAvailable_StopsWhenIdle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := newDispatcherForTest(t, db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.DiscardHandler), &Config{
		BatchSize:           1,
		LockTimeout:         time.Minute,
		PollInterval:        time.Hour,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.telemetry = nil

	require.False(t, dispatcher.processAvailable(ctx, 4))

	sender.mu.Lock()
	require.Empty(t, sender.messages)
	sender.mu.Unlock()

	var deliveryCount int64

	require.NoError(t, countDeliveryTestRowsWhere(db, &deliveryTestDeliveryModel{}, &deliveryCount, "").Error)
	require.Zero(t, deliveryCount)
}

func TestDispatcher_ProcessAvailableWithoutWorkerInstrumentation(t *testing.T) {
	t.Parallel()

	dispatcher := newDispatcherForTest(t, nil, nil, nil, nil, slog.New(slog.DiscardHandler), &Config{})
	dispatcher.processClaimedOrPendingDeliveries(t.Context(), nil, 0)
}

// scriptedIdleWaiter는 run 루프가 빈 조회 뒤에만 기다리고 처리 뒤에는 대기를 짧게 되돌리는지 기록한다.
type scriptedIdleWaiter struct {
	waits     []bool
	waitCalls int
	resets    int
}

func (w *scriptedIdleWaiter) Wait(context.Context) bool {
	if w.waitCalls >= len(w.waits) {
		return false
	}

	w.waitCalls++

	return w.waits[w.waitCalls-1]
}

func (w *scriptedIdleWaiter) Reset() {
	w.resets++
}

func TestDispatcher_RunResetsWaitAfterProcessedRows(t *testing.T) {
	t.Parallel()

	db := newDeliveryPool(t)
	cache := cachemocks.NewLenientClient()

	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == alarmkeys.BuildChannelSubscriberKey("UCwake", domain.AlarmTypeLive) {
			return []string{"room-wake"}, nil
		}

		return nil, nil
	}

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := newDispatcherForTest(t, db, cache, sender, nil, slog.New(slog.DiscardHandler), &Config{
		BatchSize:           1,
		LockTimeout:         time.Minute,
		PollInterval:        time.Hour,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.telemetry = nil

	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UCwake",
		ContentID:     "wake-1",
		Payload:       `{"video_id":"wake-1","title":"wake title"}`,
		Status:        domain.OutboxStatusPending,
		NextAttemptAt: time.Now(),
	}).Error)

	idle := &scriptedIdleWaiter{waits: []bool{true, false}}

	dispatcher.idle = idle

	dispatcher.run(t.Context())

	// 첫 조회가 행을 처리해 대기를 되돌리고, 깨어난 뒤의 빈 조회는 되돌리지 않은 채 다시 기다린다.
	require.Equal(t, 1, idle.resets)
	require.Equal(t, 2, idle.waitCalls)

	sender.mu.Lock()
	require.Len(t, sender.messages, 1)
	sender.mu.Unlock()
}
