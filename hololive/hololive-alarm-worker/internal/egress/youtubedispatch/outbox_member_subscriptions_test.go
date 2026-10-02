package youtubedispatch

import (
	"context"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
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
		payload, err := jsonv2.Marshal(youtubeoutbox.Video{VideoID: "video", Title: tc.title})
		require.NoError(t, err)

		items = append(items, domain.YouTubeNotificationOutbox{ID: int64(i + 1), ChannelID: outboxUnitBChannel, Kind: tc.kind, Payload: string(payload)})
	}

	config := testDispatchConfig()
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
	config := testDispatchConfig()
	grouper := newOutboxGrouper(nil, nil, slog.New(slog.DiscardHandler), &config)

	for _, payload := range []string{"not-json", "null", `{"title":123}`, `{"title":"?"}`} {
		item := domain.YouTubeNotificationOutbox{ID: 1, ChannelID: outboxUnitBChannel, Kind: domain.OutboxKindLiveStream, Payload: payload}
		targets := grouper.collectRoomsByChannel(t.Context(), []domain.YouTubeNotificationOutbox{item})
		_, ok := roomsForItem(targets, &item)
		require.False(t, ok, "malformed payload or missing DB must preserve fanout failure")
	}
}

// UNIT B는 진행자 조합마다 조회 대상이 생기지만, 같은 채널·알림 종류의 구독 목록은 한 번만 읽고 제목별로 나눈다.
func TestOutboxMemberSubscriptionsLoadEachChannelKindOnce(t *testing.T) {
	type lookupCall struct {
		channelID string
		alarmType domain.AlarmType
		titles    []string
	}

	var (
		callsMu sync.Mutex
		calls   []lookupCall
	)

	config := testDispatchConfig()
	grouper := newOutboxGrouper(nil, nil, slog.New(slog.DiscardHandler), &config)

	grouper.lookupSubscribers = func(_ context.Context, channelID string, titles []string, alarmType domain.AlarmType) (map[string][]string, error) {
		callsMu.Lock()

		calls = append(calls, lookupCall{channelID: channelID, alarmType: alarmType, titles: slices.Clone(titles)})
		callsMu.Unlock()

		out := make(map[string][]string, len(titles))
		for _, title := range titles {
			out[title] = []string{"room:" + title}
		}

		return out, nil
	}

	titles := []string{"#玲銘ミラ", "#宵凪ネオン", "#玲銘ミラ #宵凪ネオン"}
	items := make([]domain.YouTubeNotificationOutbox, 0, len(titles)+1)

	for i, title := range titles {
		payload, err := jsonv2.Marshal(youtubeoutbox.Video{VideoID: "video", Title: title})
		require.NoError(t, err)

		items = append(items, domain.YouTubeNotificationOutbox{ID: int64(i + 1), ChannelID: outboxUnitBChannel, Kind: domain.OutboxKindLiveStream, Payload: string(payload)})
	}

	shortPayload, err := jsonv2.Marshal(youtubeoutbox.Video{VideoID: "short", Title: "#玲銘ミラ"})
	require.NoError(t, err)

	items = append(items, domain.YouTubeNotificationOutbox{ID: 9, ChannelID: outboxUnitBChannel, Kind: domain.OutboxKindNewShort, Payload: string(shortPayload)})

	targets := grouper.collectRoomsByChannel(t.Context(), items)

	require.Len(t, calls, 2, "live와 shorts 종류마다 한 번씩만 조회한다")

	wantTitles := map[domain.AlarmType][]string{
		domain.AlarmTypeLive:   titles,
		domain.AlarmTypeShorts: {"#玲銘ミラ"},
	}

	for _, call := range calls {
		require.Equal(t, outboxUnitBChannel, call.channelID)
		require.Contains(t, wantTitles, call.alarmType)
		require.ElementsMatch(t, wantTitles[call.alarmType], call.titles)
	}

	for i := range items {
		rooms, ok := roomsForItem(targets, &items[i])
		require.True(t, ok)

		var payload youtubeoutbox.Video

		require.NoError(t, jsonv2.Unmarshal([]byte(items[i].Payload), &payload))
		require.Equal(t, map[string]bool{"room:" + payload.Title: true}, rooms)
	}
}
