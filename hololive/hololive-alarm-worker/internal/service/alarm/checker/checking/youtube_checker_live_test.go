package checking

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestLiveCatchupReadsUpcomingMarkersInOneRoundTripAndSkipsFailedRooms(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	start := now.Add(-2 * time.Minute)
	countingCache := &countingCheckerCacheClient{Client: newCheckerTestCacheClient(t)}
	dedupService := dedup.NewService(countingCache, newCheckerTestLogger())
	checker := &YouTubeChecker{dedupService: dedupService, logger: newCheckerTestLogger()}
	stream := &domain.Stream{
		ID:             "live-batched-marker",
		Title:          "batched marker",
		ChannelID:      testChIDLive,
		Status:         domain.StreamStatusLive,
		StartScheduled: &start,
		StartActual:    &start,
		Channel:        &domain.Channel{ID: testChIDLive},
	}

	require.NoError(t, dedupService.MarkUpcomingEventNotified(ctx, "room-upcoming", testChIDLive, stream))

	brokenKey := alarmkeys.BuildUpcomingEventKey("room-broken", testChIDLive, stream.ID, stream.Title, start)
	require.NoError(t, countingCache.Set(ctx, brokenKey, "not an upcoming marker object", time.Hour))

	rooms := []string{"room-upcoming", "room-broken", "room-new-1", "room-settled", "room-new-2"}
	settled := map[string]struct{}{"room-settled": {}}

	notifications, err := checker.buildLiveCatchupNotifications(ctx, testTargetPolicy(), testChIDLive, stream, rooms, now, settled)
	require.NoError(t, err)
	assert.Equal(t, 1, countingCache.doMultiCalls)

	notifiedRooms := make([]string, 0, len(notifications))
	for _, notification := range notifications {
		notifiedRooms = append(notifiedRooms, notification.RoomID)
	}

	assert.Equal(t, []string{"room-new-1", "room-new-2"}, notifiedRooms)

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()

	_, err = checker.buildLiveCatchupNotifications(canceledCtx, testTargetPolicy(), testChIDLive, stream, rooms, now, settled)
	require.ErrorIs(t, err, context.Canceled)
}
