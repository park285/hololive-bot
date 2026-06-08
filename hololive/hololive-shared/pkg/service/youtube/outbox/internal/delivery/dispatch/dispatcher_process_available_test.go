package dispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestDispatcher_ProcessAvailable_DrainsMultipleRounds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	cache := cachemocks.NewLenientClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == sharedalarmkeys.BuildChannelSubscriberKey("UCdrain", domain.AlarmTypeLive) {
			return []string{"room-drain"}, nil
		}
		return nil, nil
	}

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cache, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
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

	dispatcher.processAvailable(ctx, 4)

	sender.mu.Lock()
	require.Len(t, sender.messages, 3)
	sender.mu.Unlock()

	var sentCount int64
	require.NoError(t, countDeliveryTestRowsWhere(db, &deliveryTestOutboxModel{}, &sentCount, "status = ?", string(domain.OutboxStatusSent)).Error)
	require.EqualValues(t, 3, sentCount)
}

func TestDispatcher_ProcessAvailable_StopsWhenIdle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cachemocks.NewLenientClient(), sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           1,
		LockTimeout:         time.Minute,
		PollInterval:        time.Hour,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})
	dispatcher.telemetry = nil

	dispatcher.processAvailable(ctx, 4)

	sender.mu.Lock()
	require.Len(t, sender.messages, 0)
	sender.mu.Unlock()

	var deliveryCount int64
	require.NoError(t, countDeliveryTestRowsWhere(db, &deliveryTestDeliveryModel{}, &deliveryCount, "").Error)
	require.Zero(t, deliveryCount)
}
