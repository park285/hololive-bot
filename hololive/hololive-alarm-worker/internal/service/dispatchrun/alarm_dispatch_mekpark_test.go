package dispatchrun

import (
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const mekparkUnitBChannel = "UC3OH5FKQ3qtl4uRme_vZTgA"

func TestMekParkLiveAndUpcomingRendering(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)

	for _, minutes := range []int{0, 5} {
		notification := &domain.AlarmNotification{
			Channel: &domain.Channel{ID: mekparkUnitBChannel, Name: "유닛 B"},
			Stream: &domain.Stream{
				ID: "video123", ChannelID: mekparkUnitBChannel,
				ChannelName: "UNIT B", Title: "【ピアノ】#玲銘ミラ", Status: domain.StreamStatusUpcoming,
			},
			MinutesUntil: minutes,
		}
		message, err := renderAlarmDispatchNotification(t.Context(), renderer, store, nil, notification)
		require.NoError(t, err)
		require.Contains(t, message, "**유닛 B · 미라**")

		item := buildAlarmDispatchNotificationKaringContentItem(t.Context(), store, notification)
		require.Equal(t, "유닛 B · 미라", item.MemberName)
		require.Equal(t, "UNIT B", item.ChannelName)
		require.Equal(t, "【ピアノ】#玲銘ミラ", item.Title)
		require.Equal(t, "유닛 B", notification.Channel.Name)
		require.Equal(t, "UNIT B", notification.Stream.ChannelName)

		notification.Stream.ChannelID = ""
		require.Equal(t, "유닛 B · 미라", resolveAlarmDispatchMemberName(t.Context(), store, notification))

		notification.Stream.IsChzzkOnly = true
		require.Equal(t, "유닛 B", resolveAlarmDispatchMemberName(t.Context(), store, notification))
	}
}

func TestMekParkOutboxKaringKeepsHostsPerItem(t *testing.T) {
	_, store := newAlarmDispatchTestRendering(t)
	payload := &domain.YouTubeOutboxDispatchPayload{
		Kind: domain.OutboxKindNewVideo, ChannelID: mekparkUnitBChannel, MemberName: "유닛 B",
	}

	for _, tc := range []struct {
		title string
		name  string
	}{
		{title: "#玲銘ミラ", name: "유닛 B · 미라"},
		{title: "#宵凪ネオン", name: "유닛 B · 네온"},
		{title: "?", name: "유닛 B"},
	} {
		data, err := jsonv2.Marshal(alarmDispatchKaringVideoPayload{VideoID: "video123", Title: tc.title})
		require.NoError(t, err)

		item, err := buildAlarmDispatchYouTubeOutboxKaringContentItem(t.Context(), store, payload, domain.YouTubeOutboxItem{Payload: string(data)})
		require.NoError(t, err)
		require.Equal(t, tc.name, item.MemberName)
		require.Equal(t, "유닛 B", item.ChannelName)
		require.Equal(t, tc.title, item.Title)
	}

	require.Equal(t, "유닛 B", payload.MemberName)
}

func TestMekParkSeriesHostRendering(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)
	notification := &domain.AlarmNotification{
		Channel: &domain.Channel{ID: "UChpRPsAeSZn5DistGacR3iA", Name: "아크로라"},
		Stream: &domain.Stream{
			ID: "video123", ChannelID: "UChpRPsAeSZn5DistGacR3iA",
			Title: "【#由比河ひなみ】#あさやなストレッチ夏 #墨汐さやな", Status: domain.StreamStatusLive,
		},
	}
	message, err := renderAlarmDispatchNotification(t.Context(), renderer, store, nil, notification)
	require.NoError(t, err)
	require.Contains(t, message, "아크로라 · 사야나 (게스트: 히나미)")

	item := buildAlarmDispatchNotificationKaringContentItem(t.Context(), store, notification)
	require.Equal(t, "아크로라 · 사야나 (게스트: 히나미)", item.MemberName)
}
