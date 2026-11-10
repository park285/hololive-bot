package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
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

// newLiveCatchupSuppressionGrouper는 roomID별 upcoming marker 원문을 miniredis에 넣은 grouper를 만든다.
func newLiveCatchupSuppressionGrouper(t *testing.T, item *domain.YouTubeNotificationOutbox, scheduledAt time.Time, markers map[string]string) *OutboxGrouper {
	t.Helper()

	cache, mini := sharedtestutil.NewTestCacheServiceWithMini(t.Context(), t)

	for roomID, marker := range markers {
		require.NoError(t, mini.Set(keys.BuildUpcomingEventKey(roomID, item.ChannelID, "live-1", "Live One", scheduledAt), marker))
	}

	return newOutboxGrouper(nil, cache, slog.New(slog.DiscardHandler), withTestDispatchConfigDefaults(nil))
}

func notifiedMarker(at time.Time) string {
	return `{"notified_at":"` + at.UTC().Format(time.RFC3339) + `"}`
}

func liveCatchupSuppressionCount(t *testing.T, result string) int64 {
	t.Helper()
	initOutboxMetrics()

	return int64(testutil.ToFloat64(outboxLiveCatchupSuppressionTotal.WithLabelValues(result)))
}

func TestFilterLiveCatchupSuppressedRoomsSkipsRecentUpcomingRooms(t *testing.T) {
	item, scheduledAt := newLiveCatchupSuppressionItem(t)
	grouper := newLiveCatchupSuppressionGrouper(t, &item, scheduledAt, map[string]string{
		"room-suppressed": notifiedMarker(time.Now()),
	})
	suppressedBefore := liveCatchupSuppressionCount(t, liveCatchupSuppressionResultSuppressed)

	filtered := grouper.filterLiveCatchupSuppressedRooms(t.Context(), &item, map[string]bool{
		"room-suppressed": true,
		"room-live-only":  true,
		"room-unselected": false,
	})

	require.Equal(t, map[string]bool{"room-live-only": true}, filtered)
	require.Equal(t, suppressedBefore+1, liveCatchupSuppressionCount(t, liveCatchupSuppressionResultSuppressed))
}

func TestFilterLiveCatchupSuppressedRoomsFailsOpenOnCacheError(t *testing.T) {
	item, scheduledAt := newLiveCatchupSuppressionItem(t)
	cache, mini := sharedtestutil.NewTestCacheServiceWithMini(t.Context(), t)
	require.NoError(t, mini.Set(keys.BuildUpcomingEventKey(testRoomA, item.ChannelID, "live-1", "Live One", scheduledAt), notifiedMarker(time.Now())))
	mini.SetError("redis down")

	grouper := newOutboxGrouper(nil, cache, slog.New(slog.DiscardHandler), withTestDispatchConfigDefaults(nil))
	cacheErrorBefore := liveCatchupSuppressionCount(t, liveCatchupSuppressionResultCacheError)
	suppressedBefore := liveCatchupSuppressionCount(t, liveCatchupSuppressionResultSuppressed)

	filtered := grouper.filterLiveCatchupSuppressedRooms(t.Context(), &item, map[string]bool{
		testRoomA: true,
		testRoomB: true,
	})

	require.Equal(t, map[string]bool{testRoomA: true, testRoomB: true}, filtered)
	require.Equal(t, cacheErrorBefore+2, liveCatchupSuppressionCount(t, liveCatchupSuppressionResultCacheError))
	require.Equal(t, suppressedBefore, liveCatchupSuppressionCount(t, liveCatchupSuppressionResultSuppressed))
}

func TestFilterLiveCatchupSuppressedRoomsFailsOpenOnInvalidMarker(t *testing.T) {
	item, scheduledAt := newLiveCatchupSuppressionItem(t)
	grouper := newLiveCatchupSuppressionGrouper(t, &item, scheduledAt, map[string]string{
		testRoomA: `{"notified_at":"not-a-timestamp"}`,
		testRoomB: `not json`,
	})
	invalidBefore := liveCatchupSuppressionCount(t, liveCatchupSuppressionResultInvalidMarker)
	cacheErrorBefore := liveCatchupSuppressionCount(t, liveCatchupSuppressionResultCacheError)

	filtered := grouper.filterLiveCatchupSuppressedRooms(t.Context(), &item, map[string]bool{
		testRoomA: true,
		testRoomB: true,
	})

	require.Equal(t, map[string]bool{testRoomA: true, testRoomB: true}, filtered)
	require.Equal(t, invalidBefore+1, liveCatchupSuppressionCount(t, liveCatchupSuppressionResultInvalidMarker))
	require.Equal(t, cacheErrorBefore+1, liveCatchupSuppressionCount(t, liveCatchupSuppressionResultCacheError))
}

func TestFilterLiveCatchupSuppressedRoomsLeavesNotCoveredUncounted(t *testing.T) {
	item, scheduledAt := newLiveCatchupSuppressionItem(t)
	grouper := newLiveCatchupSuppressionGrouper(t, &item, scheduledAt, map[string]string{
		"room-empty":   `{"notified_at":""}`,
		"room-expired": notifiedMarker(time.Now().Add(-tier.LiveCatchupSuppressWindow - time.Minute)),
	})
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
		"room-missing": true,
	})

	require.Equal(t, map[string]bool{"room-empty": true, "room-expired": true, "room-missing": true}, filtered)

	for _, result := range results {
		require.Equal(t, before[result], liveCatchupSuppressionCount(t, result), result)
	}
}
