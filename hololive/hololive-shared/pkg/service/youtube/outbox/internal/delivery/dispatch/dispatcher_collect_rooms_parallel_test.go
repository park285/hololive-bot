package dispatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestCollectRoomsByChannel_PerformsTypedLookupsConcurrently(t *testing.T) {
	t.Parallel()

	shortsKey := sharedalarmkeys.BuildChannelSubscriberKey("UCparallel", domain.AlarmTypeShorts)
	communityKey := sharedalarmkeys.BuildChannelSubscriberKey("UCparallel", domain.AlarmTypeCommunity)
	shortsStarted := make(chan struct{})
	communityStarted := make(chan struct{})
	release := make(chan struct{})

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		switch key {
		case shortsKey:
			close(shortsStarted)
			<-release
			return []string{"room-shorts"}, nil
		case communityKey:
			close(communityStarted)
			<-release
			return []string{"room-community"}, nil
		default:
			return nil, nil
		}
	}

	dispatcher := NewDispatcher(nil, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})
	done := make(chan map[string]channelAlarmRoomTargets, 1)
	go func() {
		done <- dispatcher.grouper.collectRoomsByChannel(context.Background(), []domain.YouTubeNotificationOutbox{
			{ChannelID: "UCparallel", Kind: domain.OutboxKindNewShort},
			{ChannelID: "UCparallel", Kind: domain.OutboxKindCommunityPost},
		})
	}()

	select {
	case <-shortsStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("shorts lookup did not start")
	}

	select {
	case <-communityStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("community lookup did not start while shorts lookup was blocked")
	}

	close(release)

	roomsByChannel := <-done
	require.Contains(t, roomsByChannel, "UCparallel")
	require.Contains(t, roomsByChannel["UCparallel"][domain.AlarmTypeShorts], "room-shorts")
	require.Contains(t, roomsByChannel["UCparallel"][domain.AlarmTypeCommunity], "room-community")
}

func TestCollectRoomsByChannelFallsBackToDBWhenCacheEmpty(t *testing.T) {
	t.Parallel()

	db := newDispatcherSubscriberLookupTestDB(t)
	require.NoError(t, insertDeliveryTestRows(db, &domain.Alarm{
		RoomID:     "room-db",
		ChannelID:  "UCfallback",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts},
	}).Error)

	cache := cachemocks.NewLenientClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		require.Equal(t, sharedalarmkeys.BuildChannelSubscriberKey("UCfallback", domain.AlarmTypeShorts), key)
		return nil, nil
	}

	dispatcher := NewDispatcher(db, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})
	roomsByChannel := dispatcher.grouper.collectRoomsByChannel(context.Background(), []domain.YouTubeNotificationOutbox{
		{ChannelID: "UCfallback", Kind: domain.OutboxKindNewShort},
	})

	require.Contains(t, roomsByChannel, "UCfallback")
	require.Contains(t, roomsByChannel["UCfallback"][domain.AlarmTypeShorts], "room-db")
}

func TestCollectRoomsByChannelFallsBackToDBWhenCacheErrors(t *testing.T) {
	t.Parallel()

	db := newDispatcherSubscriberLookupTestDB(t)
	require.NoError(t, insertDeliveryTestRows(db, &domain.Alarm{
		RoomID:     "room-db",
		ChannelID:  "UCfallback-error",
		AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity},
	}).Error)

	cache := cachemocks.NewLenientClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		require.Equal(t, sharedalarmkeys.BuildChannelSubscriberKey("UCfallback-error", domain.AlarmTypeCommunity), key)
		return nil, errors.New("cache unavailable")
	}

	dispatcher := NewDispatcher(db, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})
	roomsByChannel := dispatcher.grouper.collectRoomsByChannel(context.Background(), []domain.YouTubeNotificationOutbox{
		{ChannelID: "UCfallback-error", Kind: domain.OutboxKindCommunityPost},
	})

	require.Contains(t, roomsByChannel, "UCfallback-error")
	require.Contains(t, roomsByChannel["UCfallback-error"][domain.AlarmTypeCommunity], "room-db")
}

func newDispatcherSubscriberLookupTestDB(t *testing.T) *deliveryTestDB {
	t.Helper()

	db := newDeliveryPool(t)

	return db
}
