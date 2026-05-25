package delivery

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestOutboxGrouperCollectRoomsByChannelUsesTypedSubscriberLookup(t *testing.T) {
	t.Parallel()

	lookedUpKeys := make([]string, 0, 2)
	var lookedUpKeysMu sync.Mutex
	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		lookedUpKeysMu.Lock()
		lookedUpKeys = append(lookedUpKeys, key)
		lookedUpKeysMu.Unlock()
		switch key {
		case sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeShorts):
			return []string{"room-shorts"}, nil
		case sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeCommunity):
			return []string{"room-community"}, nil
		default:
			return nil, nil
		}
	}

	grouper := newOutboxGrouper(nil, cache, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})
	roomsByChannel := grouper.collectRoomsByChannel(context.Background(), []domain.YouTubeNotificationOutbox{
		{ChannelID: "UCtarget", Kind: domain.OutboxKindNewShort},
		{ChannelID: "UCtarget", Kind: domain.OutboxKindCommunityPost},
		{ChannelID: "UCtarget", Kind: domain.OutboxKindNewShort},
	})

	lookedUpKeysMu.Lock()
	recordedKeys := append([]string(nil), lookedUpKeys...)
	lookedUpKeysMu.Unlock()

	require.Len(t, recordedKeys, 2)
	require.True(t, sameStrings(recordedKeys, []string{
		sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeShorts),
		sharedalarmkeys.BuildChannelSubscriberKey("UCtarget", domain.AlarmTypeCommunity),
	}))

	targets, ok := roomsByChannel["UCtarget"]
	require.True(t, ok)
	require.Equal(t, map[string]bool{"room-shorts": true}, targets[domain.AlarmTypeShorts])
	require.Equal(t, map[string]bool{"room-community": true}, targets[domain.AlarmTypeCommunity])
}

func TestOutboxGrouperFilterLiveCatchupSuppressedRoomsSkipsRecentUpcomingRooms(t *testing.T) {
	startedAt := time.Now().UTC().Add(-time.Minute)
	scheduledAt := startedAt.Add(-5 * time.Minute)
	payload := `{"video_id":"live-1","title":"Live One","published_at":"` + startedAt.Format(time.RFC3339) + `","scheduled_start_at":"` + scheduledAt.Format(time.RFC3339) + `"}`
	item := domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindLiveStream,
		ChannelID: "UC_LIVE",
		ContentID: "live-1",
		Payload:   payload,
	}
	suppressedKey := sharedalarmkeys.BuildUpcomingEventKey("room-suppressed", item.ChannelID, "live-1", "Live One", scheduledAt)
	cache := cachemocks.NewStrictClient()
	cache.GetFunc = func(_ context.Context, key string, dest any) error {
		data, ok := dest.(*liveUpcomingSuppressionData)
		require.True(t, ok)
		if key == suppressedKey {
			data.NotifiedAt = time.Now().UTC().Format(time.RFC3339)
		}
		return nil
	}
	grouper := newOutboxGrouper(nil, cache, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})

	filtered := grouper.filterLiveCatchupSuppressedRooms(context.Background(), item, map[string]bool{
		"room-suppressed": true,
		"room-live-only":  true,
	})

	require.Equal(t, map[string]bool{"room-live-only": true}, filtered)
}
