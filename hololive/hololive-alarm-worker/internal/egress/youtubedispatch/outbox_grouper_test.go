package youtubedispatch

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
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
		case alarmkeys.BuildChannelSubscriberKey(testChannelTarget, domain.AlarmTypeShorts):
			return []string{testRoomShorts}, nil
		case alarmkeys.BuildChannelSubscriberKey(testChannelTarget, domain.AlarmTypeCommunity):
			return []string{testRoomCommunity}, nil
		default:
			return nil, nil
		}
	}

	grouper := newOutboxGrouper(nil, cache, slog.New(slog.DiscardHandler), withTestDispatchConfigDefaults(nil))
	roomsByChannel := grouper.collectRoomsByChannel(t.Context(), []domain.YouTubeNotificationOutbox{
		{ChannelID: testChannelTarget, Kind: domain.OutboxKindNewShort},
		{ChannelID: testChannelTarget, Kind: domain.OutboxKindCommunityPost},
		{ChannelID: testChannelTarget, Kind: domain.OutboxKindNewShort},
	})

	lookedUpKeysMu.Lock()

	recordedKeys := append([]string(nil), lookedUpKeys...)
	lookedUpKeysMu.Unlock()

	require.Len(t, recordedKeys, 2)
	require.True(t, sameStrings(recordedKeys, []string{
		alarmkeys.BuildChannelSubscriberKey(testChannelTarget, domain.AlarmTypeShorts),
		alarmkeys.BuildChannelSubscriberKey(testChannelTarget, domain.AlarmTypeCommunity),
	}))

	targets, ok := roomsByChannel[testChannelTarget]
	require.True(t, ok)
	require.Equal(t, map[string]bool{testRoomShorts: true}, targets[domain.AlarmTypeShorts])
	require.Equal(t, map[string]bool{testRoomCommunity: true}, targets[domain.AlarmTypeCommunity])
}
