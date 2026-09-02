package youtubedispatch

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func newLiveCatchupSuppressionItem(t *testing.T) (domain.YouTubeNotificationOutbox, time.Time) {
	t.Helper()

	startedAt := time.Now().UTC().Add(-time.Minute)
	scheduledAt := startedAt.Add(-5 * time.Minute)
	payload := `{"video_id":"live-1","title":"Live One","published_at":"` + startedAt.Format(time.RFC3339) + `","scheduled_start_at":"` + scheduledAt.Format(time.RFC3339) + `"}`

	return domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindLiveStream,
		ChannelID: "UC_LIVE",
		ContentID: "live-1",
		Payload:   payload,
	}, scheduledAt
}

func newLiveCatchupSuppressionGrouper(cache *cachemocks.Client) *OutboxGrouper {
	return newOutboxGrouper(nil, cache, slog.New(slog.DiscardHandler), &dispatchstate.Config{})
}

func liveCatchupSuppressionCount(t *testing.T, result string) int64 {
	t.Helper()
	initOutboxMetrics()

	return int64(testutil.ToFloat64(outboxLiveCatchupSuppressionTotal.WithLabelValues(result)))
}

func TestFilterLiveCatchupSuppressedRoomsSkipsRecentUpcomingRooms(t *testing.T) {
	item, scheduledAt := newLiveCatchupSuppressionItem(t)
	suppressedKey := keys.BuildUpcomingEventKey("room-suppressed", item.ChannelID, "live-1", "Live One", scheduledAt)
	cache := cachemocks.NewStrictClient()

	cache.GetFunc = func(_ context.Context, key string, dest any) error {
		data, ok := dest.(*liveUpcomingSuppressionData)
		require.True(t, ok)

		if key == suppressedKey {
			data.NotifiedAt = time.Now().UTC().Format(time.RFC3339)
		}

		return nil
	}

	grouper := newLiveCatchupSuppressionGrouper(cache)
	suppressedBefore := liveCatchupSuppressionCount(t, liveCatchupSuppressionResultSuppressed)

	filtered := grouper.filterLiveCatchupSuppressedRooms(t.Context(), &item, map[string]bool{
		"room-suppressed": true,
		"room-live-only":  true,
	})

	require.Equal(t, map[string]bool{"room-live-only": true}, filtered)
	require.Equal(t, suppressedBefore+1, liveCatchupSuppressionCount(t, liveCatchupSuppressionResultSuppressed))
}

func TestFilterLiveCatchupSuppressedRoomsFailsOpenOnCacheError(t *testing.T) {
	item, _ := newLiveCatchupSuppressionItem(t)
	cache := cachemocks.NewStrictClient()

	cache.GetFunc = func(_ context.Context, _ string, _ any) error {
		return errors.New("redis down")
	}

	grouper := newLiveCatchupSuppressionGrouper(cache)
	cacheErrorBefore := liveCatchupSuppressionCount(t, liveCatchupSuppressionResultCacheError)
	suppressedBefore := liveCatchupSuppressionCount(t, liveCatchupSuppressionResultSuppressed)

	filtered := grouper.filterLiveCatchupSuppressedRooms(t.Context(), &item, map[string]bool{
		testRoomA: true,
		"room-b":  true,
	})

	require.Equal(t, map[string]bool{testRoomA: true, "room-b": true}, filtered)
	require.Equal(t, cacheErrorBefore+2, liveCatchupSuppressionCount(t, liveCatchupSuppressionResultCacheError))
	require.Equal(t, suppressedBefore, liveCatchupSuppressionCount(t, liveCatchupSuppressionResultSuppressed))
}

func TestFilterLiveCatchupSuppressedRoomsFailsOpenOnInvalidMarker(t *testing.T) {
	item, _ := newLiveCatchupSuppressionItem(t)
	cache := cachemocks.NewStrictClient()

	cache.GetFunc = func(_ context.Context, _ string, dest any) error {
		data, ok := dest.(*liveUpcomingSuppressionData)
		require.True(t, ok)

		data.NotifiedAt = "not-a-timestamp"

		return nil
	}

	grouper := newLiveCatchupSuppressionGrouper(cache)
	invalidBefore := liveCatchupSuppressionCount(t, liveCatchupSuppressionResultInvalidMarker)

	filtered := grouper.filterLiveCatchupSuppressedRooms(t.Context(), &item, map[string]bool{
		testRoomA: true,
	})

	require.Equal(t, map[string]bool{testRoomA: true}, filtered)
	require.Equal(t, invalidBefore+1, liveCatchupSuppressionCount(t, liveCatchupSuppressionResultInvalidMarker))
}

func TestFilterLiveCatchupSuppressedRoomsLeavesNotCoveredUncounted(t *testing.T) {
	item, scheduledAt := newLiveCatchupSuppressionItem(t)
	expiredAt := time.Now().UTC().Add(-constants.LiveCatchupSuppressWindow - time.Minute).Format(time.RFC3339)
	markers := map[string]string{
		"room-empty":   "",
		"room-expired": expiredAt,
	}
	cache := cachemocks.NewStrictClient()

	cache.GetFunc = func(_ context.Context, key string, dest any) error {
		data, ok := dest.(*liveUpcomingSuppressionData)
		require.True(t, ok)

		for roomID, marker := range markers {
			if key == keys.BuildUpcomingEventKey(roomID, item.ChannelID, "live-1", "Live One", scheduledAt) {
				data.NotifiedAt = marker
			}
		}

		return nil
	}

	grouper := newLiveCatchupSuppressionGrouper(cache)
	results := []string{
		liveCatchupSuppressionResultSuppressed,
		liveCatchupSuppressionResultCacheError,
		liveCatchupSuppressionResultInvalidMarker,
	}
	before := make(map[string]int64, len(results))

	for _, result := range results {
		before[result] = liveCatchupSuppressionCount(t, result)
	}

	filtered := grouper.filterLiveCatchupSuppressedRooms(t.Context(), &item, map[string]bool{
		"room-empty":   true,
		"room-expired": true,
	})

	require.Equal(t, map[string]bool{"room-empty": true, "room-expired": true}, filtered)

	for _, result := range results {
		require.Equal(t, before[result], liveCatchupSuppressionCount(t, result), result)
	}
}
