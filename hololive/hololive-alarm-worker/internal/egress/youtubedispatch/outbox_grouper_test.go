package youtubedispatch

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	dispatchstate "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
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

	grouper := newOutboxGrouper(nil, cache, slog.New(slog.NewTextHandler(io.Discard, nil)), &dispatchstate.Config{})
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
