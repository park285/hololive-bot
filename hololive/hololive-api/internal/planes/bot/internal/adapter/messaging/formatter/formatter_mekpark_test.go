package formatter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestMekParkStreamDisplays(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdLiveStreams:     "{{range .Streams}}{{.ChannelName}}|{{.Title}}\n{{end}}",
		domain.TemplateKeyCmdUpcomingStreams: "{{range .Streams}}{{.ChannelName}}|{{.Title}}\n{{end}}",
		domain.TemplateKeyCmdChannelSchedule: "{{.ChannelName}}\n{{range .Streams}}{{.Title}}\n{{end}}",
	})
	f := NewResponseFormatter("!", renderer, WithMessageStrings(setupFormatterTestStore(t)))
	channel := &domain.Channel{ID: "UC3OH5FKQ3qtl4uRme_vZTgA", Name: "유닛 B"}
	stream := &domain.Stream{
		ID: "video123", ChannelID: channel.ID, ChannelName: channel.Name,
		Title: strings.Repeat("長いタイトル", 15) + " #玲銘ミラ", Status: domain.StreamStatusUpcoming,
	}
	original := *stream
	streams := []*domain.Stream{stream}

	require.Contains(t, formatLiveStreams(t.Context(), f, streams), "유닛 B · 미라|")
	require.Contains(t, f.UpcomingStreams(t.Context(), streams, 24), "유닛 B · 미라|")
	require.Contains(t, f.ChannelSchedule(t.Context(), channel, streams, 7), "유닛 B\n미라 ·")
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

	stream.ChannelID = "UC_other"
	stream.ChannelName = "다른 채널"
	require.Equal(t, "다른 채널", f.formatChannelName(t.Context(), stream))
}
