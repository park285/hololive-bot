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
	"unicode/utf8"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
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

func (*seeMoreFoldEgressClient) SendKaringContentList(context.Context, iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	return nil, errors.New("karing lane is not configured")
}

func (*seeMoreFoldEgressClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	return nil, errors.New("reply status is not used by the text lane")
}

// 사용자 첨부 화면처럼 한 방에 쇼츠 10개가 한 번에 묶여 나가는 v1 직접 발송 경로다.
func runDirectGroupedShortsFinalPayload(t *testing.T, fold bool) string {
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
	client := &seeMoreFoldEgressClient{}
	d := newDispatcherWithDepsForTest(t, nil, Dependencies{
		Cache:       cachemocks.NewLenientClient(),
		Sender:      egress.NewIrisMessageSender(client),
		SeeMoreFold: fold,
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

func TestDispatchDeliveryRowsFoldsLongGroupedShortsAtFinalPayload(t *testing.T) {
	t.Parallel()

	padding := strings.Repeat(util.KakaoZeroWidthSpace, util.KakaoSeeMorePadding)
	plain := runDirectGroupedShortsFinalPayload(t, false)
	folded := runDirectGroupedShortsFinalPayload(t, true)

	head, _, _ := strings.Cut(plain, "\n")

	require.NotContains(t, plain, padding, "BOT_SEE_MORE_FOLD=false must keep the grouped text unfolded")
	require.Contains(t, head, "· 10개")
	require.True(t, strings.HasPrefix(plain[len(head):], "\n\n"), "only the count header may stay above the fold: %q", plain)
	require.Equal(t, head+padding+plain[len(head):], folded, "padding must follow the header and keep every expanded item")
	require.Equal(t, 1, strings.Count(folded, padding))
	require.Equal(t, 10, strings.Count(folded, "https://www.youtube.com/shorts/"))

	t.Logf("direct grouped shorts final payload: head=%q padding_after_rune=%d padding_runs=%d visible_runes=%d final_runes=%d\n%q",
		head, utf8.RuneCountInString(head), strings.Count(folded, padding), utf8.RuneCountInString(plain),
		utf8.RuneCountInString(folded), strings.Replace(folded, padding, "〔ZWSP×500〕", 1))
}
