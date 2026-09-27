package youtubedispatch

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestCollectRoomsByChannel_PerformsTypedLookupsConcurrently(t *testing.T) {
	t.Parallel()

	// DB fixture의 실제 IO와 수명은 가상 시계 bubble 밖에서 소유한다.
	cache := cachemocks.NewStrictClient()
	dispatcher := newDispatcherForTest(t, nil, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{})

	synctest.Test(t, func(t *testing.T) {
		shortsKey := sharedalarmkeys.BuildChannelSubscriberKey("UCparallel", domain.AlarmTypeShorts)
		communityKey := sharedalarmkeys.BuildChannelSubscriberKey("UCparallel", domain.AlarmTypeCommunity)
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

		// 두 조회가 모두 release를 기다리는지 확인하며 wall-clock 속도는 판정하지 않는다.
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
		require.Equal(t, sharedalarmkeys.BuildChannelSubscriberKey("UCfallback", domain.AlarmTypeShorts), key)

		return nil, nil
	}

	dispatcher := newDispatcherForTest(t, db, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{})
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
		require.Equal(t, sharedalarmkeys.BuildChannelSubscriberKey("UCfallback-error", domain.AlarmTypeCommunity), key)

		return nil, errors.New("cache unavailable")
	}

	dispatcher := newDispatcherForTest(t, db, cache, &testSender{failRoom: map[string]bool{}}, nil, slog.New(slog.DiscardHandler), &dispatchstate.Config{})
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
