package notification

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmPersistence_MarkAsNotifiedRoundTrip(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()
	start := time.Date(2026, 3, 5, 11, 25, 42, 0, time.UTC)

	require.NoError(t, as.MarkAsNotified(ctx, "stream-roundtrip", start, 5))
	require.NoError(t, as.MarkAsNotified(ctx, "stream-roundtrip", start, 3))

	var data NotifiedData
	require.NoError(t, as.cache.Get(ctx, NotifiedKeyPrefix+"stream-roundtrip", &data))
	assert.Equal(t, normalizeScheduledMinute(start).Format(time.RFC3339), data.StartScheduled)
	assert.True(t, data.SentAt[5])
	assert.True(t, data.SentAt[3])
}

func TestAlarmPersistence_MarkAsNotifiedTimeout(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	time.Sleep(1 * time.Millisecond)
	cancel()

	err := as.MarkAsNotified(ctx, "stream-timeout", time.Now().UTC(), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mark as notified")
}

func TestAlarmPersistence_UpcomingEventRoundTrip(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Minute)

	stream := &domain.Stream{
		ID:             "stream-upcoming",
		ChannelID:      "channel-1",
		Title:          "테스트 예정 방송",
		StartScheduled: &start,
	}

	require.NoError(t, as.MarkUpcomingEventNotified(ctx, "room-1", "channel-1", stream))
	assert.True(t, as.WasUpcomingEventNotifiedRecently(ctx, "room-1", "channel-1", stream, time.Minute))
	assert.False(t, as.WasUpcomingEventNotifiedRecently(ctx, "room-1", "channel-1", stream, 0))
}
