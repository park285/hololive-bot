package alarmdispatch

import (
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
		require.Contains(t, message, "유닛 B · 미라")

		require.Equal(t, "유닛 B", notification.Channel.Name)
		require.Equal(t, "UNIT B", notification.Stream.ChannelName)

		notification.Stream.ChannelID = ""
		require.Equal(t, "유닛 B · 미라", resolveAlarmDispatchMemberName(t.Context(), store, notification))
	}
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
}
