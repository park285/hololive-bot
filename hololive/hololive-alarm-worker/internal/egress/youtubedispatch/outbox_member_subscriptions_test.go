package youtubedispatch

import (
	jsonv2 "encoding/json/v2"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	format "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/format"
)

const (
	outboxUnitBChannel = "UC3OH5FKQ3qtl4uRme_vZTgA"
	outboxWholeRoom    = "whole"
	outboxMiraRoom     = "mira"
	outboxNeonRoom     = "neon"
	outboxSeveralRoom  = "several"
)

func TestOutboxMemberSubscriptionsSeparateTitlesInOneBatch(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	repo := sharedalarm.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)

	for _, alarm := range []*domain.Alarm{
		{RoomID: outboxWholeRoom, ChannelID: outboxUnitBChannel},
		{RoomID: outboxMiraRoom, ChannelID: outboxUnitBChannel, HostID: "reimei-mira"},
		{RoomID: outboxNeonRoom, ChannelID: outboxUnitBChannel, HostID: "yoinagi-neon", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
		{RoomID: outboxSeveralRoom, ChannelID: outboxUnitBChannel, HostID: "reimei-mira"},
		{RoomID: outboxSeveralRoom, ChannelID: outboxUnitBChannel, HostID: "yoinagi-neon"},
		{RoomID: "unrelated", ChannelID: "other-channel"},
	} {
		require.NoError(t, repo.Add(t.Context(), alarm))
	}

	cases := []struct {
		title string
		kind  domain.OutboxKind
		rooms []string
	}{
		{"#玲銘ミラ", domain.OutboxKindLiveStream, []string{outboxWholeRoom, outboxMiraRoom, outboxSeveralRoom}},
		{"#宵凪ネオン", domain.OutboxKindLiveStream, []string{outboxWholeRoom, outboxNeonRoom, outboxSeveralRoom}},
		{"#玲銘ミラ #宵凪ネオン", domain.OutboxKindLiveStream, []string{outboxWholeRoom, outboxMiraRoom, outboxNeonRoom, outboxSeveralRoom}},
		{"今日は何をしよう？", domain.OutboxKindLiveStream, []string{outboxWholeRoom, outboxMiraRoom, outboxNeonRoom, outboxSeveralRoom}},
		{"", domain.OutboxKindNewVideo, []string{outboxWholeRoom, outboxMiraRoom, outboxNeonRoom, outboxSeveralRoom}},
		{"#宵凪ネオン", domain.OutboxKindNewShort, []string{outboxWholeRoom, outboxSeveralRoom}},
		{"?", domain.OutboxKindNewShort, []string{outboxWholeRoom, outboxMiraRoom, outboxSeveralRoom}},
	}
	items := make([]domain.YouTubeNotificationOutbox, 0, len(cases))

	for i, tc := range cases {
		payload, err := jsonv2.Marshal(format.VideoPayload{VideoID: "video", Title: tc.title})
		require.NoError(t, err)

		items = append(items, domain.YouTubeNotificationOutbox{ID: int64(i + 1), ChannelID: outboxUnitBChannel, Kind: tc.kind, Payload: string(payload)})
	}

	config := dispatchstate.DefaultConfig()
	grouper := newOutboxGrouper(pool, nil, logger, &config)
	targets := grouper.collectRoomsByChannel(t.Context(), items)

	for i, tc := range cases {
		rooms, ok := roomsForItem(targets, &items[i])
		require.True(t, ok)

		got := make([]string, 0, len(rooms))
		for roomID, selected := range rooms {
			require.True(t, selected)

			got = append(got, roomID)
		}

		require.ElementsMatch(t, tc.rooms, got, "title=%s kind=%s", tc.title, tc.kind)
	}
}

func TestOutboxMemberSubscriptionFailuresAreNotUnknownTitles(t *testing.T) {
	config := dispatchstate.DefaultConfig()
	grouper := newOutboxGrouper(nil, nil, slog.New(slog.DiscardHandler), &config)

	for _, payload := range []string{"not-json", "null", `{"title":123}`, `{"title":"?"}`} {
		item := domain.YouTubeNotificationOutbox{ID: 1, ChannelID: outboxUnitBChannel, Kind: domain.OutboxKindLiveStream, Payload: payload}
		targets := grouper.collectRoomsByChannel(t.Context(), []domain.YouTubeNotificationOutbox{item})
		_, ok := roomsForItem(targets, &item)
		require.False(t, ok, "malformed payload or missing DB must preserve fanout failure")
	}
}
