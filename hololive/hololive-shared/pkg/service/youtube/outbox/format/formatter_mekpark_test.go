package format

import (
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const mekparkUnitBChannel = "UC3OH5FKQ3qtl4uRme_vZTgA"

func mekparkVideoOutbox(t *testing.T, kind domain.OutboxKind, title string) domain.YouTubeNotificationOutbox {
	t.Helper()

	payload, err := jsonv2.Marshal(VideoPayload{VideoID: "video123", Title: title})
	require.NoError(t, err)

	return domain.YouTubeNotificationOutbox{Kind: kind, ChannelID: mekparkUnitBChannel, Payload: string(payload)}
}

func TestMekParkVideoOutboxDisplay(t *testing.T) {
	t.Parallel()

	for _, kind := range []domain.OutboxKind{domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			item := mekparkVideoOutbox(t, kind, "【挑戦】ピアノ #玲銘ミラ")
			original := item
			data, err := (&MessageFormatter{}).BuildTemplateData("유닛 B", &item)
			require.NoError(t, err)
			require.Equal(t, "유닛 B · 미라", data.MemberName)
			require.Equal(t, "【挑戦】ピアノ #玲銘ミラ", data.Title)
			require.Equal(t, original, item)
			require.Contains(t, renderOutboxBody(t, outboxBodyVideo, data), "**유닛 B · 미라**")
		})
	}
}

func TestMekParkGroupedOutboxKeepsHostsPerItem(t *testing.T) {
	t.Parallel()

	items := []domain.YouTubeNotificationOutbox{
		mekparkVideoOutbox(t, domain.OutboxKindNewVideo, "#玲銘ミラ"),
		mekparkVideoOutbox(t, domain.OutboxKindNewVideo, "#宵凪ネオン"),
		mekparkVideoOutbox(t, domain.OutboxKindNewVideo, "?"),
	}
	data := (&MessageFormatter{}).BuildGroupedTemplateData("유닛 B", domain.OutboxKindNewVideo, items)
	require.Equal(t, "유닛 B", data.MemberName)
	require.Equal(t, "미라 · #玲銘ミラ", data.Items[0].Title)
	require.Equal(t, "네온 · #宵凪ネオン", data.Items[1].Title)
	require.Equal(t, "?", data.Items[2].Title)

	message := renderOutboxBody(t, outboxBodyVideoGroup, data)
	require.Contains(t, message, "유닛 B 새 영상 (3)")
	require.Contains(t, message, "1. [미라 ·")
	require.Contains(t, message, "2. [네온 ·")
	require.Contains(t, message, "3. [?]")
}

func TestMekParkUnattributedOutboxDisplay(t *testing.T) {
	t.Parallel()

	formatter := &MessageFormatter{}
	unknown := mekparkVideoOutbox(t, domain.OutboxKindLiveStream, "【UNIT_B】誰が出るかな?")
	data, err := formatter.BuildTemplateData("유닛 B", &unknown)
	require.NoError(t, err)
	require.Equal(t, "유닛 B", data.MemberName)

	foreign := mekparkVideoOutbox(t, domain.OutboxKindNewVideo, "#玲銘ミラ")

	foreign.ChannelID = "UC_other"
	data, err = formatter.BuildTemplateData("다른 채널", &foreign)
	require.NoError(t, err)
	require.Equal(t, "다른 채널", data.MemberName)
	require.Equal(t, "#玲銘ミラ", BuildGroupedItemData(&foreign).Title)

	for _, kind := range []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindMilestone} {
		item := domain.YouTubeNotificationOutbox{
			Kind: kind, ChannelID: mekparkUnitBChannel,
			Payload: `{"content_text":"#玲銘ミラ","milestone":"100000","title":"#玲銘ミラ"}`,
		}

		data, err = formatter.BuildTemplateData("유닛 B", &item)
		require.NoError(t, err)
		require.Equal(t, "유닛 B", data.MemberName)
	}
}
