package format

import (
	jsonv2 "encoding/json/v2"
	"log/slog"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// 실제 기본 템플릿과 생산자를 거쳐도 동적 기호와 복사 가능한 URL이 보존된다.
func TestOutboxProducersPreserveLiteralFields(t *testing.T) {
	pool := dbtest.NewPool(t)

	const (
		member = "||멤버|| **끝**"
		title  = "1. ||가격|| **제목** _원문_"
	)

	payload, err := jsonv2.Marshal(map[string]string{
		"video_id": "a_b-c", "post_id": "p_q-r", "title": title,
		"content_text": title,
	})
	require.NoError(t, err)

	formatter := NewMessageFormatter(template.NewRenderer(pool, slog.Default()), staticMemberNameSource{name: member}, nil)

	for _, kind := range []domain.OutboxKind{domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream, domain.OutboxKindCommunityPost} {
		t.Run(string(kind), func(t *testing.T) {
			item := domain.YouTubeNotificationOutbox{ID: 17, Kind: kind, ChannelID: "UC_literal", Payload: string(payload)}
			results, err := formatter.FormatMessages(t.Context(), []domain.YouTubeNotificationOutbox{item})
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.NoError(t, results[0].Err)

			grouped, err := formatter.FormatGroupedMessage(t.Context(), member, item.ChannelID, kind, []domain.YouTubeNotificationOutbox{item, item})
			require.NoError(t, err)

			url := VideoTemplateURL(kind, "a_b-c")
			if kind == domain.OutboxKindCommunityPost {
				url = "https://www.youtube.com/post/p_q-r"
			}

			for _, message := range []string{results[0].Message, grouped} {
				visible := strings.ReplaceAll(kakaoformat.Render(message), kakaotext.ZeroWidthSpace, "")
				require.Contains(t, visible, member)

				for _, field := range []string{"||가격||", "**제목**", "_원문_"} {
					require.Contains(t, visible, field)
				}

				require.Contains(t, "\n"+visible+"\n", "\n"+url+"\n")
			}
		})
	}
}
