package dispatchrun

import (
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestKaringDisplayKeepsSourceAndRequestIdentity(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("123", nil)
	title := strings.Repeat("\u200b", 20) + "첫 줄\r\n다음 줄\t" + strings.Repeat("긴 제목 ", 30)

	envelope.Notification.Stream.Title = title
	envelope.Notification.Channel.Name = "미코\r\nMiko"
	envelope.Notification.Stream.ChannelName = "채널\t이름"

	group := alarmDispatchGroup{
		roomID: "123", minutesUntil: 5,
		notifications: []domain.AlarmNotification{envelope.Notification}, envelopes: []domain.AlarmQueueEnvelope{envelope},
	}
	requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, group)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	require.Len(t, requests[0].Items, 1)

	item := requests[0].Items[0]
	require.Equal(t, 64, utf8.RuneCountInString(item.Title))
	require.True(t, strings.HasPrefix(item.Title, "첫 줄 다음 줄 "))
	require.True(t, strings.HasSuffix(item.Title, "..."))
	require.NotContains(t, item.Title, "\u200b")
	require.Equal(t, "미코 Miko", item.MemberName)
	require.Equal(t, "채널 이름", item.ChannelName)
	require.Equal(t, title, envelope.Notification.Stream.Title)
	require.Equal(t, "미코\r\nMiko", envelope.Notification.Channel.Name)

	envelope.Notification.Stream.Title = "다른 표시 제목"

	updated, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, group)
	require.NoError(t, err)
	require.Equal(t, "다른 표시 제목", updated[0].Items[0].Title)
	require.Equal(t, requests[0].ClientRequestID, updated[0].ClientRequestID)
	require.Equal(t, item.URL, updated[0].Items[0].URL)
	require.Equal(t, requests[0].TemplateID, updated[0].TemplateID)
}

func TestKaringOutboxTitlesAreDisplayOnly(t *testing.T) {
	for _, kind := range []domain.OutboxKind{domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindCommunityPost} {
		t.Run(string(kind), func(t *testing.T) {
			title := "첫 줄\n" + strings.Repeat("多言語 제목 ", 30)
			encoded, err := jsonv2.Marshal(map[string]string{"title": title, "video_id": "abc_123_456", "content_text": title, "post_id": "post-123"})
			require.NoError(t, err)

			source := domain.YouTubeOutboxItem{ContentID: "abc_123_456", Payload: string(encoded)}
			payload := &domain.YouTubeOutboxDispatchPayload{Kind: kind, MemberName: "미코\nMiko"}
			item, err := buildAlarmDispatchYouTubeOutboxKaringContentItem(t.Context(), nil, payload, source)
			require.NoError(t, err)
			require.Equal(t, 64, utf8.RuneCountInString(item.Title))
			require.NotContains(t, item.Title, "\n")
			require.Equal(t, "미코 Miko", item.MemberName)
			require.Equal(t, string(encoded), source.Payload)
			require.Equal(t, "미코\nMiko", payload.MemberName)
		})
	}
}
