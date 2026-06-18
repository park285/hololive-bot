package dispatch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestFilterLiveCatchupSuppressedRoomsSkipsRecentUpcomingRooms(t *testing.T) {
	startedAt := time.Now().UTC().Add(-time.Minute)
	scheduledAt := startedAt.Add(-5 * time.Minute)
	payload := `{"video_id":"live-1","title":"Live One","published_at":"` + startedAt.Format(time.RFC3339) + `","scheduled_start_at":"` + scheduledAt.Format(time.RFC3339) + `"}`
	item := domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindLiveStream,
		ChannelID: "UC_LIVE",
		ContentID: "live-1",
		Payload:   payload,
	}
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
	dispatcher := &Dispatcher{
		grouper: newOutboxGrouper(nil, cache, slog.New(slog.NewTextHandler(io.Discard, nil)), &Config{}),
	}

	filtered := dispatcher.grouper.filterLiveCatchupSuppressedRooms(context.Background(), &item, map[string]bool{
		"room-suppressed": true,
		"room-live-only":  true,
	})

	require.Equal(t, map[string]bool{"room-live-only": true}, filtered)
}
