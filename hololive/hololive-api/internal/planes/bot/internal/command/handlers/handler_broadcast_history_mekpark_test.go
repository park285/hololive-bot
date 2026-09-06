package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

func TestMekParkBroadcastHistoryDisplay(t *testing.T) {
	t.Parallel()

	entries := []handlercore.BroadcastHistoryEntry{
		{ChannelID: "UC3OH5FKQ3qtl4uRme_vZTgA", MemberName: "유닛 B", Title: "#玲銘ミラ"},
		{ChannelID: "UC3OH5FKQ3qtl4uRme_vZTgA", MemberName: "유닛 B", Title: "?"},
		{ChannelID: "UC_other", MemberName: "다른 채널", Title: "#玲銘ミラ"},
	}
	views := broadcastHistoryFormatterEntries(entries)
	require.Len(t, views, 3)
	require.Equal(t, "유닛 B · 미라", views[0].MemberName)
	require.Equal(t, "유닛 B", views[1].MemberName)
	require.Equal(t, "다른 채널", views[2].MemberName)

	for i := range entries {
		require.Equal(t, entries[i].Title, views[i].Title)
	}

	require.Equal(t, "유닛 B", entries[0].MemberName)
}
