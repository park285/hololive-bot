package dispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store"
)

func TestDispatcherAggregateSyncQuarantinesStaleSendingDelivery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := newDeliveryPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	lockedAt := now.Add(-10 * time.Minute)

	outbox := domain.YouTubeNotificationOutbox{
		Kind:          domain.OutboxKindNewVideo,
		ChannelID:     "UC_stale_sending",
		ContentID:     "video-stale-sending",
		Payload:       `{"video_id":"video-stale-sending","title":"stale"}`,
		Status:        domain.OutboxStatusPending,
		AttemptCount:  0,
		NextAttemptAt: now.Add(-time.Hour),
		CreatedAt:     now.Add(-time.Hour),
	}
	require.NoError(t, insertDeliveryTestRows(db, &outbox).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID:      outbox.ID,
		RoomID:        "room-stale-sending",
		Status:        store.DeliveryStatusSending,
		AttemptCount:  0,
		NextAttemptAt: now.Add(-time.Hour),
		CreatedAt:     now.Add(-time.Hour),
		LockedAt:      &lockedAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &delivery).Error)

	dispatcher := NewDispatcher(db, nil, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &Config{
		BatchSize:      10,
		LockTimeout:    time.Minute,
		PollInterval:   time.Hour,
		MaxRetries:     3,
		RetryBackoff:   time.Minute,
		CleanupAfter:   time.Hour,
		CleanupEnabled: false,
	})
	dispatcher.telemetry = nil

	dispatcher.aggregateSyncOnce(ctx)

	var gotDelivery deliveryTestDeliveryModel
	require.NoError(t, firstDeliveryTestRow(db, &gotDelivery, delivery.ID).Error)
	require.Equal(t, string(store.DeliveryStatusQuarantined), gotDelivery.Status)
	require.Equal(t, 1, gotDelivery.AttemptCount)
	require.Nil(t, gotDelivery.LockedAt)
	require.Equal(t, "stale sending; external send outcome unknown", gotDelivery.Error)

	var gotOutbox deliveryTestOutboxModel
	require.NoError(t, firstDeliveryTestRow(db, &gotOutbox, outbox.ID).Error)
	require.Equal(t, string(domain.OutboxStatusFailed), gotOutbox.Status)
}
