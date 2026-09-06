package formatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestMekParkStreamAndAlarmDisplays(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdLiveStreams:            "{{range .Streams}}{{.ChannelName}}|{{.Title}}\n{{end}}",
		domain.TemplateKeyCmdUpcomingStreams:        "{{range .Streams}}{{.ChannelName}}|{{.Title}}\n{{end}}",
		domain.TemplateKeyCmdChannelSchedule:        "{{.ChannelName}}\n{{range .Streams}}{{.Title}}\n{{end}}",
		domain.TemplateKeyCmdAlarmNotification:      "{{.ChannelName}}|{{.Title}}",
		domain.TemplateKeyCmdAlarmLiveStarted:       "{{.ChannelName}}|{{.Title}}",
		domain.TemplateKeyCmdAlarmNotificationGroup: cmdAlarmNotificationGroupBody,
	})
	f := NewResponseFormatter("!", renderer, WithMessageStrings(setupFormatterTestStore(t)))
	channel := &domain.Channel{ID: "UC3OH5FKQ3qtl4uRme_vZTgA", Name: "유닛 B"}
	stream := &domain.Stream{
		ID: "video123", ChannelID: channel.ID, ChannelName: channel.Name,
		Title: strings.Repeat("長いタイトル", 15) + " #玲銘ミラ", Status: domain.StreamStatusUpcoming,
	}
	original := *stream
	streams := []*domain.Stream{stream}

	require.Contains(t, f.FormatLiveStreams(t.Context(), streams), "유닛 B · 미라|")
	require.Contains(t, f.UpcomingStreams(t.Context(), streams, 24), "유닛 B · 미라|")
	require.Contains(t, f.ChannelSchedule(t.Context(), channel, streams, 7), "유닛 B\n미라 ·")

	notification := &domain.AlarmNotification{Channel: channel, Stream: stream, MinutesUntil: 5}
	require.Contains(t, f.AlarmNotification(t.Context(), notification), "유닛 B · 미라|")

	notification.MinutesUntil = 0
	require.Contains(t, f.AlarmNotification(t.Context(), notification), "유닛 B · 미라|")

	second := *stream

	second.ID = "video456"
	second.Title = "#宵凪ネオン"

	group := []*domain.AlarmNotification{notification, {Channel: channel, Stream: &second, MinutesUntil: 0}}
	message := f.AlarmNotificationGroup(t.Context(), 0, group)
	require.Contains(t, message, "유닛 B · 미라")
	require.Contains(t, message, "유닛 B · 네온")
	require.Equal(t, original, *stream)
	require.Equal(t, "유닛 B", channel.Name)
}

func TestMekParkStreamDisplayPreservesUnrelatedStreams(t *testing.T) {
	t.Parallel()

	f := NewResponseFormatter("!", nil)
	stream := &domain.Stream{
		ChannelID: "UC3OH5FKQ3qtl4uRme_vZTgA", ChannelName: "유닛 B", Title: "?",
	}
	require.Equal(t, "유닛 B", f.formatChannelName(t.Context(), stream))

	stream.Title = "#玲銘ミラ"
	stream.IsTwitchOnly = true
	require.Equal(t, "유닛 B", f.formatChannelName(t.Context(), stream))
	require.Equal(t, "유닛 B", f.alarmChannelName(t.Context(), &domain.AlarmNotification{Stream: stream}))

	stream.IsTwitchOnly = false
	stream.ChannelID = "UC_other"
	stream.ChannelName = "다른 채널"
	require.Equal(t, "다른 채널", f.formatChannelName(t.Context(), stream))
}
