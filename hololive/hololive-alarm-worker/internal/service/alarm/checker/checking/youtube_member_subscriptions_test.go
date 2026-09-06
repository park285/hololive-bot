package checking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

const (
	checkerUnitBChannel = "UC3OH5FKQ3qtl4uRme_vZTgA"
	checkerWholeRoom    = "whole"
	checkerMiraRoom     = "mira"
	checkerNeonRoom     = "neon"
	checkerSeveralRoom  = "several"
	checkerMiraLiveID   = "live-mira"
)

func newMemberSubscriptionChecker(t *testing.T) *YouTubeChecker {
	t.Helper()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	cache := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	repo := sharedalarm.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)

	for _, alarm := range []*domain.Alarm{
		{RoomID: checkerWholeRoom, ChannelID: checkerUnitBChannel},
		{RoomID: checkerMiraRoom, ChannelID: checkerUnitBChannel, HostID: "reimei-mira"},
		{RoomID: checkerNeonRoom, ChannelID: checkerUnitBChannel, HostID: "yoinagi-neon"},
		{RoomID: checkerSeveralRoom, ChannelID: checkerUnitBChannel, HostID: "reimei-mira"},
		{RoomID: checkerSeveralRoom, ChannelID: checkerUnitBChannel, HostID: "yoinagi-neon"},
	} {
		require.NoError(t, repo.Add(ctx, alarm))
	}

	holodexService, err := holodexprovider.NewHolodexService("http://unused", "k", cache, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeCheckerWithPersistedLiveSource(
		cache, holodexService, tier.NewTieredScheduler(logger), dedup.NewService(cache, []int{5, 3, 1}, logger),
		[]int{5, 3, 1}, 75*time.Second, nil, pool, logger,
	)
	require.NoError(t, err)

	return checker
}

func TestYouTubeCheckerMemberSubscriptionsForUpcomingAndLiveCatchup(t *testing.T) {
	ctx := t.Context()
	checker := newMemberSubscriptionChecker(t)
	now := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	window := sharedchecker.ResolveEvaluationWindow(now.Add(-5*time.Minute), now, 75*time.Second)
	coarseRooms := []string{checkerWholeRoom, checkerMiraRoom, checkerNeonRoom, checkerSeveralRoom, "stale-unsubscribed-room"}
	streams := []*domain.Stream{
		{ID: "up-mira", Title: "#玲銘ミラ", ChannelID: checkerUnitBChannel, Status: domain.StreamStatusUpcoming, StartScheduled: new(now.Add(4 * time.Minute))},
		{ID: "up-neon", Title: "#宵凪ネオン", ChannelID: checkerUnitBChannel, Status: domain.StreamStatusUpcoming, StartScheduled: new(now.Add(4 * time.Minute))},
		{ID: "up-unknown", Title: "今日は何をしよう？", ChannelID: checkerUnitBChannel, Status: domain.StreamStatusUpcoming, StartScheduled: new(now.Add(4 * time.Minute))},
		{ID: checkerMiraLiveID, Title: "#玲銘ミラ", ChannelID: checkerUnitBChannel, Status: domain.StreamStatusLive, StartActual: new(now.Add(-time.Minute))},
	}

	for _, stream := range streams {
		stream.Channel = &domain.Channel{ID: checkerUnitBChannel, Name: "유닛 B"}
	}

	notifications, err := checker.buildChannelNotifications(ctx, checkerUnitBChannel, coarseRooms, streams, window, now,
		map[string]map[string]struct{}{checkerMiraLiveID: {checkerWholeRoom: {}}},
	)
	require.NoError(t, err)

	got := make(map[string][]string)

	for _, notification := range notifications {
		got[notification.Stream.ID] = append(got[notification.Stream.ID], notification.RoomID)
	}

	for streamID, rooms := range map[string][]string{
		"up-mira":         {checkerWholeRoom, checkerMiraRoom, checkerSeveralRoom},
		"up-neon":         {checkerWholeRoom, checkerNeonRoom, checkerSeveralRoom},
		"up-unknown":      {checkerWholeRoom, checkerMiraRoom, checkerNeonRoom, checkerSeveralRoom},
		checkerMiraLiveID: {checkerMiraRoom, checkerSeveralRoom},
	} {
		require.ElementsMatch(t, rooms, got[streamID], streamID)
	}

	require.Len(t, got, 4)

	meta := &persistedLiveGuardrailMeta{streamID: checkerMiraLiveID, channelID: checkerUnitBChannel, rooms: coarseRooms}
	currentStreams := map[string][]*domain.Stream{checkerUnitBChannel: {{ID: checkerMiraLiveID, Title: "#宵凪ネオン"}}}
	rooms, err := checker.guardrailSubscriberRooms(ctx, meta, currentStreams)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{checkerWholeRoom, checkerNeonRoom, checkerSeveralRoom}, rooms)

	lookupErr := errors.New("subscription database unavailable")

	checker.lookupSubscribers = func(context.Context, string, string, domain.AlarmType) ([]string, error) { return nil, lookupErr }
	notifications, err = checker.buildChannelNotifications(ctx, checkerUnitBChannel, coarseRooms, streams[:1], window, now, nil)
	require.ErrorIs(t, err, lookupErr)
	require.Nil(t, notifications)
}
