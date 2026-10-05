package youtubedispatch

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
)

// seeMoreFoldEgressClient는 실제 egress.IrisMessageSender가 Kakao 텍스트 lane에 넘긴 최종 payload만 기록한다.
type seeMoreFoldEgressClient struct {
	texts []string
}

func (c *seeMoreFoldEgressClient) SendMessage(_ context.Context, _, message string, _ ...iris.SendOption) error {
	c.texts = append(c.texts, message)

	return nil
}

func (*seeMoreFoldEgressClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, errors.New("markdown lane is not configured")
}

func (*seeMoreFoldEgressClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	return nil, errors.New("reply status is not used by the text lane")
}

// 사용자 첨부 화면처럼 한 방에 쇼츠 10개가 한 번에 묶여 나가는 v1 직접 발송 경로다.
func runDirectGroupedShortsFinalPayload(t *testing.T, count int, renderer *template.Renderer) string {
	t.Helper()

	titles := []string{
		"[Announcement] Liona does not have athlete's foot. #vtuber #shorts #hololive",
		"Keep listening to my stories from now on too 🎧 #vtuber #shorts #hololive",
		"Marine-senpai's cute dance cover ♡ #vtuber #shorts #dance",
		"I'm out of breath from dancing so much 🤣 #vtuber #hololive #dance",
		"Morning routine speedrun with Liona #vtuber #shorts",
		"Can Liona guess the anime from one frame? #vtuber #shorts #quiz",
		"Trying the viral lemon challenge 🍋 #vtuber #shorts",
		"When chat asks for one more song #vtuber #shorts #singing",
		"Behind the scenes of my first 3D rehearsal #vtuber #hololive",
		"Thank you for 1 million views!! #vtuber #shorts #thankyou",
	}

	titles = titles[:count]

	client := &seeMoreFoldEgressClient{}
	d := newDispatcherWithDepsForTest(t, nil, Dependencies{
		Renderer: renderer,
		Cache:    cachemocks.NewLenientClient(),
		Sender:   egress.NewIrisMessageSender(client),
	}, slog.New(slog.DiscardHandler), &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 2,
	})

	outboxByID := make(map[int64]domain.YouTubeNotificationOutbox, len(titles))
	rows := make([]domain.YouTubeNotificationDelivery, 0, len(titles))

	for i, title := range titles {
		id := int64(i + 1)
		videoID := fmt.Sprintf("short%02d", id)
		payload, err := json.Marshal(map[string]string{"canonical_post_id": "short:" + videoID, "video_id": videoID, "title": title})
		require.NoError(t, err)

		outboxByID[id] = domain.YouTubeNotificationOutbox{
			ID: id, ChannelID: testChannelCh1, Kind: domain.OutboxKindNewShort,
			ContentID: videoID, Payload: string(payload),
		}
		rows = append(rows, domain.YouTubeNotificationDelivery{ID: 100 + id, OutboxID: id, RoomID: testRoom1})
	}

	result := d.send.dispatchDeliveryRows(t.Context(), rows, outboxByID)

	require.Len(t, result.SuccessDeliveryIDs, len(titles))
	require.Len(t, client.texts, 1, "ten shorts for one room must leave as one grouped Kakao text")

	return client.texts[0]
}

func TestDispatchDeliveryRowsKeepsShortsUnfoldedAtFinalPayload(t *testing.T) {
	t.Parallel()

	for _, count := range []int{1, 2, 10} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			t.Parallel()

			final := runDirectGroupedShortsFinalPayload(t, count, nil)
			require.NotContains(t, final, util.KakaoZeroWidthSpace+util.KakaoZeroWidthSpace, "alarm text must not carry see-more padding (consecutive ZWSP)")
			require.Equal(t, count, strings.Count(final, "https://www.youtube.com/shorts/"))
			require.Contains(t, final, "[Announcement]")

			if count > 1 {
				require.Contains(t, final, fmt.Sprintf("· %d개", count))
			}
		})
	}
}

func TestDispatchDeliveryRowsPreservesCustomPadding(t *testing.T) {
	padding := strings.Repeat(util.KakaoZeroWidthSpace, 500)
	renderer := newGroupedTemplateRenderer(t, domain.TemplateKeyOutboxShortsGroup, "제목"+padding+"\n{{range .Items}}{{.Title}}\n{{.URL}}\n{{end}}")
	final := runDirectGroupedShortsFinalPayload(t, 2, renderer)
	require.Equal(t, 1, strings.Count(final, padding))
	require.True(t, strings.HasPrefix(final, "제목"+padding+"\n"))
	require.Equal(t, 2, strings.Count(final, "https://www.youtube.com/shorts/"))
}

func (c *seeMoreFoldEgressClient) PrepareMessageRequest(_ context.Context, _, body string) (string, string, error) {
	return body, testPreparedTextRoute, nil
}

func (c *seeMoreFoldEgressClient) SendPreparedMessage(ctx context.Context, room, body, _, _ string) error {
	return c.SendMessage(ctx, room, body)
}
