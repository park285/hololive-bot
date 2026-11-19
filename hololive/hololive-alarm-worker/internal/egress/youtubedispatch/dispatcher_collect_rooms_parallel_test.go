package youtubedispatch

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestCollectRoomsByChannel_PerformsTypedLookupsConcurrently(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	dispatcher := newDispatcherForTest(t, nil, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.DiscardHandler), &Config{})

	synctest.Test(t, func(t *testing.T) {
		shortsKey := alarmkeys.BuildChannelSubscriberKey("UCparallel", domain.AlarmTypeShorts)
		communityKey := alarmkeys.BuildChannelSubscriberKey("UCparallel", domain.AlarmTypeCommunity)
		shortsStarted := make(chan struct{})
		communityStarted := make(chan struct{})
		release := make(chan struct{})

		cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
			switch key {
			case shortsKey:
				close(shortsStarted)
				<-release

				return []string{testRoomShorts}, nil
			case communityKey:
				close(communityStarted)
				<-release

				return []string{testRoomCommunity}, nil
			default:
				return nil, nil
			}
		}

		done := make(chan map[string]channelAlarmRoomTargets, 1)

		go func() {
			done <- dispatcher.grouper.collectRoomsByChannel(t.Context(), []domain.YouTubeNotificationOutbox{
				{ChannelID: "UCparallel", Kind: domain.OutboxKindNewShort},
				{ChannelID: "UCparallel", Kind: domain.OutboxKindCommunityPost},
			})
		}()

		synctest.Wait()

		shortsConcurrent, communityConcurrent := false, false

		select {
		case <-shortsStarted:
			shortsConcurrent = true
		default:
		}

		select {
		case <-communityStarted:
			communityConcurrent = true
		default:
		}

		close(release)

		roomsByChannel := <-done

		require.True(t, shortsConcurrent, "shorts lookup did not start while lookups were blocked")
		require.True(t, communityConcurrent, "community lookup did not start while lookups were blocked")
		require.Contains(t, roomsByChannel, "UCparallel")
		require.Contains(t, roomsByChannel["UCparallel"][domain.AlarmTypeShorts], testRoomShorts)
		require.Contains(t, roomsByChannel["UCparallel"][domain.AlarmTypeCommunity], testRoomCommunity)
	})
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
		require.Equal(t, alarmkeys.BuildChannelSubscriberKey("UCfallback", domain.AlarmTypeShorts), key)

		return nil, nil
	}

	dispatcher := newDispatcherForTest(t, db, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.DiscardHandler), &Config{})
	roomsByChannel := dispatcher.grouper.collectRoomsByChannel(t.Context(), []domain.YouTubeNotificationOutbox{
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
		require.Equal(t, alarmkeys.BuildChannelSubscriberKey("UCfallback-error", domain.AlarmTypeCommunity), key)

		return nil, errors.New("cache unavailable")
	}

	dispatcher := newDispatcherForTest(t, db, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.DiscardHandler), &Config{})
	roomsByChannel := dispatcher.grouper.collectRoomsByChannel(t.Context(), []domain.YouTubeNotificationOutbox{
		{ChannelID: "UCfallback-error", Kind: domain.OutboxKindCommunityPost},
	})

	require.Contains(t, roomsByChannel, "UCfallback-error")
	require.Contains(t, roomsByChannel["UCfallback-error"][domain.AlarmTypeCommunity], "room-db")
}

func newDispatcherSubscriberLookupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	db := newDeliveryPool(t)

	return db
}
