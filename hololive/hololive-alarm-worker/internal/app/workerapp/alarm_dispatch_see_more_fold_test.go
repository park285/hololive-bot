package workerapp

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
	"github.com/kapu/hololive-alarm-worker/internal/service/dispatchrun"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
)

const (
	testAlarmRoomID    = "room-1"
	testAlarmChannelID = "UCtest"
)

// seeMoreFoldConsumer는 한 배치 뒤 idle에서 끝내며 예상하지 않은 실패 경로는 기록하고 중단한다.
type seeMoreFoldConsumer struct {
	batch      []domain.AlarmQueueEnvelope
	dispatched int
	failures   []string
	cancel     context.CancelFunc
}

func (c *seeMoreFoldConsumer) DrainBatch(context.Context, int) ([]domain.AlarmQueueEnvelope, error) {
	batch := c.batch

	c.batch = nil

	return batch, nil
}

func (*seeMoreFoldConsumer) MarkSending(context.Context, []domain.AlarmQueueEnvelope) error {
	return nil
}

func (c *seeMoreFoldConsumer) MarkDispatched(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.dispatched += len(envelopes)
	return nil
}

func (*seeMoreFoldConsumer) ReleaseClaimKeys(context.Context, []string) error {
	return nil
}

func (c *seeMoreFoldConsumer) unexpected(route string) error {
	c.failures = append(c.failures, route)
	c.cancel()

	return fmt.Errorf("unexpected dispatch route: %s", route)
}

func (c *seeMoreFoldConsumer) RouteFailures(context.Context, []domain.AlarmQueueEnvelope, []domain.AlarmQueueEnvelope) error {
	return c.unexpected("retry or dead letter")
}

func (c *seeMoreFoldConsumer) RouteSendingFailures(context.Context, []domain.AlarmQueueEnvelope, []domain.AlarmQueueEnvelope) error {
	return c.unexpected("sending retry or dead letter")
}

func (c *seeMoreFoldConsumer) RequeuePreSend(context.Context, []domain.AlarmQueueEnvelope) error {
	return c.unexpected("pre-send requeue")
}

func (c *seeMoreFoldConsumer) Requeue(context.Context, []domain.AlarmQueueEnvelope) error {
	return c.unexpected("requeue")
}

func (c *seeMoreFoldConsumer) Quarantine(context.Context, []domain.AlarmQueueEnvelope, error) error {
	return c.unexpected("quarantine")
}

func (*seeMoreFoldConsumer) Wait(context.Context) bool { return false }

func (*seeMoreFoldConsumer) Reset() {}

func alarmDispatchRunnerTestEnvelope(roomID string, retry *domain.AlarmQueueRetryMetadata) domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    roomID,
			Channel:   &domain.Channel{Name: "Test Member"},
			Stream:    &domain.Stream{ID: "stream-1", Title: "Test Stream"},
		},
		Retry: retry,
	}
}

// seeMoreFoldIrisClient는 실제 egress.IrisMessageSender 뒤의 Kakao 텍스트 lane 최종 payload만 기록한다.
type seeMoreFoldIrisClient struct {
	texts []string
}

func (c *seeMoreFoldIrisClient) SendMessage(_ context.Context, _, message string, _ ...iris.SendOption) error {
	c.texts = append(c.texts, message)

	return nil
}

func (*seeMoreFoldIrisClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, errors.New("markdown lane is not configured")
}

func (*seeMoreFoldIrisClient) SendKaringContentList(context.Context, iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	return nil, errors.New("karing lane is not configured")
}

func (*seeMoreFoldIrisClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	return nil, errors.New("reply status is not used by the text lane")
}

func newSeeMoreFoldRendering(t *testing.T, overrides map[domain.TemplateKey]string) (*template.Renderer, *messagestrings.Store) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)

	for key, body := range overrides {
		_, err := pool.Exec(t.Context(),
			`INSERT INTO notification_templates(template_key, channel_id, body) VALUES ($1, $2, $3)`,
			key, testAlarmChannelID, body)
		require.NoError(t, err, "seed channel override %s", key)
	}

	return template.NewRenderer(pool, logger), messagestrings.NewStore(pool, logger)
}

// runSeeMoreFoldFinalPayload는 실제 DB template와 Runner 렌더링을 거쳐 Iris 텍스트 lane에 넘어간 최종 문자열을 반환한다.
func runSeeMoreFoldFinalPayload(t *testing.T, renderer *template.Renderer, store *messagestrings.Store, fold bool, envelopes ...domain.AlarmQueueEnvelope) string {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	client := &seeMoreFoldIrisClient{}
	consumer := &seeMoreFoldConsumer{batch: envelopes, cancel: cancel}
	runner := dispatchrun.NewRunner(consumer, egress.NewIrisMessageSender(client), renderer, store, consumer,
		dispatchrun.RunnerConfig{MaxBatch: len(envelopes), SeeMoreFold: fold}, slog.New(slog.DiscardHandler))

	require.NoError(t, runner.Start(ctx))
	require.Empty(t, consumer.failures)
	require.Equal(t, len(envelopes), consumer.dispatched)
	require.Len(t, client.texts, 1, "grouped envelopes must leave as one Kakao text message")

	return client.texts[0]
}

func seeMoreFoldPadding() string {
	return strings.Repeat(util.KakaoZeroWidthSpace, util.KakaoSeeMorePadding)
}

func logSeeMoreFoldFinalPayload(t *testing.T, name, final, plain string) {
	t.Helper()

	padding := seeMoreFoldPadding()
	head := ""

	if prefix, _, found := strings.Cut(final, padding); found {
		head = prefix
	}

	t.Logf("%s final payload: head=%q padding_after_rune=%d padding_runs=%d padding_zwsp=%d visible_runes=%d final_runes=%d visible_body_preserved=%t\n%q",
		name, head, utf8.RuneCountInString(head), strings.Count(final, padding), util.KakaoSeeMorePadding,
		utf8.RuneCountInString(plain), utf8.RuneCountInString(final), strings.Replace(final, padding, "", 1) == plain,
		strings.Replace(final, padding, "〔ZWSP×500〕", 1))
}

// requireFoldedAfterHead는 접힌 최종 payload가 머리 문단 직후 패딩 한 번만 더한 비접힘 payload인지 확인한다.
func requireFoldedAfterHead(t *testing.T, folded, plain, head string) {
	t.Helper()

	padding := seeMoreFoldPadding()

	require.NotContains(t, plain, padding, "fold-off payload must not carry see-more padding")
	require.True(t, strings.HasPrefix(plain, head), "head %q is not the start of %q", head, plain)
	require.Equal(t, head+padding+plain[len(head):], folded, "padding must sit right after the head and keep the expanded body")
	require.Equal(t, 1, strings.Count(folded, padding))
}

func seeMoreFoldVideoPayload(t *testing.T, videoID, title string) string {
	t.Helper()

	payload, err := json.Marshal(map[string]string{"video_id": videoID, "title": title})
	require.NoError(t, err)

	return string(payload)
}

func seeMoreFoldCommunityPayload(t *testing.T, postID, text string) string {
	t.Helper()

	payload, err := json.Marshal(map[string]string{"post_id": postID, "content_text": text})
	require.NoError(t, err)

	return string(payload)
}

func seeMoreFoldOutboxEnvelope(kind domain.OutboxKind, payloads []string) domain.AlarmQueueEnvelope {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	ids := make([]int64, len(payloads))
	items := make([]domain.YouTubeOutboxItem, len(payloads))

	for i := range payloads {
		ids[i] = int64(i + 1)
		items[i] = domain.YouTubeOutboxItem{OutboxID: ids[i], ContentID: fmt.Sprintf("content-%02d", i+1), Payload: payloads[i]}
	}

	envelope.Notification.AlarmType = kind.ToAlarmType()
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  ids,
		Kind:       kind,
		AlarmType:  kind.ToAlarmType(),
		ChannelID:  testAlarmChannelID,
		MemberName: "리오나",
		Items:      items,
	}

	return envelope
}

// seeMoreFoldScreenshotShortPayloads는 사용자 첨부 화면의 쇼츠 10개 묶음과 같은 형태의 payload다.
func seeMoreFoldScreenshotShortPayloads(t *testing.T) []string {
	t.Helper()

	shorts := []struct{ id, title string }{
		{"eIrXBWTz3rk", "[Announcement] Liona does not have athlete's foot. #vtuber #shorts #hololive"},
		{"1OaBQ8SOdSI", "Keep listening to my stories from now on too 🎧 #vtuber #shorts #hololive"},
		{"FuAABGoRcUI", "Marine-senpai's cute dance cover ♡ #vtuber #shorts #dance"},
		{"KX0m-fAbc12", "I'm out of breath from dancing so much 🤣 #vtuber #hololive #dance"},
		{"Q7mR2xLp9sA", "Morning routine speedrun with Liona #vtuber #shorts"},
		{"Zt4Kq8Wn3eB", "Can Liona guess the anime from one frame? #vtuber #shorts #quiz"},
		{"Hn6Vd2Jc5uC", "Trying the viral lemon challenge 🍋 #vtuber #shorts"},
		{"Pw9Ls1Xa7rD", "When chat asks for one more song #vtuber #shorts #singing"},
		{"Bg3Ty8Mk2qE", "Behind the scenes of my first 3D rehearsal #vtuber #hololive"},
		{"Rc5Nf4Hs6wF", "Thank you for 1 million views!! #vtuber #shorts #thankyou"},
	}
	payloads := make([]string, 0, len(shorts))

	for _, short := range shorts {
		payloads = append(payloads, seeMoreFoldVideoPayload(t, short.id, short.title))
	}

	return payloads
}

func TestAlarmDispatchRunnerFoldsLongGroupedOutboxAtFinalPayload(t *testing.T) {
	t.Parallel()

	renderer, store := newSeeMoreFoldRendering(t, nil)

	videos := seeMoreFoldScreenshotShortPayloads(t)
	posts := make([]string, 0, 6)

	for i := range 6 {
		posts = append(posts, seeMoreFoldCommunityPayload(t, fmt.Sprintf("Ugkx%02d", i), fmt.Sprintf("커뮤니티 공지 %d번입니다. 이번 주 방송 일정과 굿즈 안내를 함께 확인해 주세요.", i+1)))
	}

	for _, tc := range []struct {
		name      string
		kind      domain.OutboxKind
		payloads  []string
		urlPrefix string
	}{
		{name: "screenshot shorts 10", kind: domain.OutboxKindNewShort, payloads: videos, urlPrefix: "https://www.youtube.com/shorts/"},
		{name: "video group", kind: domain.OutboxKindNewVideo, payloads: videos, urlPrefix: "https://youtu.be/"},
		{name: "community group", kind: domain.OutboxKindCommunityPost, payloads: posts, urlPrefix: "https://www.youtube.com/post/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelope := seeMoreFoldOutboxEnvelope(tc.kind, tc.payloads)
			plain := runSeeMoreFoldFinalPayload(t, renderer, store, false, envelope)
			folded := runSeeMoreFoldFinalPayload(t, renderer, store, true, envelope)

			head, _, _ := strings.Cut(plain, "\n")
			require.True(t, strings.HasPrefix(plain[len(head):], "\n\n"), "only the count header may stay above the fold: %q", plain)
			require.Contains(t, head, fmt.Sprintf("· %d개", len(tc.payloads)))
			requireFoldedAfterHead(t, folded, plain, head)
			require.Equal(t, len(tc.payloads), strings.Count(folded, tc.urlPrefix), "every item URL must stay in the expanded body")
			logSeeMoreFoldFinalPayload(t, tc.name, folded, plain)
		})
	}
}

func TestAlarmDispatchRunnerFoldsLongAlarmNotificationGroupAtFinalPayload(t *testing.T) {
	t.Parallel()

	renderer, store := newSeeMoreFoldRendering(t, nil)
	start := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	envelopes := make([]domain.AlarmQueueEnvelope, 0, 6)

	for i := range 6 {
		envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

		envelope.Notification.MinutesUntil = 5
		envelope.Notification.Channel.Name = fmt.Sprintf("멤버%d", i+1)
		envelope.Notification.Stream.ID = fmt.Sprintf("stream-%02d", i+1)
		envelope.Notification.Stream.Title = fmt.Sprintf("【歌枠】%d번째 노래 방송 with 여러분 #hololive", i+1)
		envelope.Notification.Stream.StartScheduled = &start
		envelopes = append(envelopes, envelope)
	}

	plain := runSeeMoreFoldFinalPayload(t, renderer, store, false, envelopes...)
	folded := runSeeMoreFoldFinalPayload(t, renderer, store, true, envelopes...)

	head, _, _ := strings.Cut(plain, "\n")
	require.True(t, strings.HasPrefix(plain[len(head):], "\n\n"), "only the alarm header may stay above the fold: %q", plain)
	require.Contains(t, head, "· 6개")
	requireFoldedAfterHead(t, folded, plain, head)
	require.Equal(t, len(envelopes), strings.Count(folded, "https://youtube.com/watch?v="))
	logSeeMoreFoldFinalPayload(t, "alarm notification group", folded, plain)
}

func TestAlarmDispatchRunnerSeeMoreFoldKeepsSingleShortAndPreRenderedMessages(t *testing.T) {
	t.Parallel()

	longText := strings.Repeat("단일 공지 본문이 길게 이어집니다. ", 12)
	renderer, store := newSeeMoreFoldRendering(t, map[domain.TemplateKey]string{
		// 저장된 채널 override는 seed의 100자 제한 없이 단일 공지를 전문으로 보낸다.
		domain.TemplateKeyOutboxCommunity: "{{.MemberName}} 공지\n\n{{.ContentText}}\n{{.URL}}",
	})

	digest := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	digest.Notification.AlarmType = domain.AlarmTypeCommunity
	digest.SourceKind = domain.AlarmDispatchSourceKindDeliveryDigest
	digest.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{
		Kind:               domain.DeliveryKindMemberNewsWeekly,
		PeriodKey:          "2026-W39",
		PreRenderedMessage: "주간 멤버 뉴스\n\n" + strings.Repeat("- 멤버 소식 항목이 이어집니다\n", 20),
	}

	for _, tc := range []struct {
		name     string
		envelope domain.AlarmQueueEnvelope
		long     bool
	}{
		{
			name:     "single long custom community",
			envelope: seeMoreFoldOutboxEnvelope(domain.OutboxKindCommunityPost, []string{seeMoreFoldCommunityPayload(t, "UgkxSingle", longText)}),
			long:     true,
		},
		{
			name: "short grouped shorts",
			envelope: seeMoreFoldOutboxEnvelope(domain.OutboxKindNewShort, []string{
				seeMoreFoldVideoPayload(t, "short-a", "짧은 쇼츠"),
				seeMoreFoldVideoPayload(t, "short-b", "또 쇼츠"),
			}),
		},
		{name: "pre-rendered digest", envelope: digest, long: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plain := runSeeMoreFoldFinalPayload(t, renderer, store, false, tc.envelope)
			final := runSeeMoreFoldFinalPayload(t, renderer, store, true, tc.envelope)

			if tc.long {
				require.Greater(t, utf8.RuneCountInString(plain), util.KakaoSeeMoreThreshold, "boundary needs a message long enough to fold")
			} else {
				require.LessOrEqual(t, utf8.RuneCountInString(plain), util.KakaoSeeMoreThreshold)
			}

			require.Equal(t, plain, final, "fold must not touch single, short, or pre-rendered messages")
			require.NotContains(t, final, seeMoreFoldPadding())
			logSeeMoreFoldFinalPayload(t, tc.name, final, plain)
		})
	}
}

func TestAlarmDispatchRunnerSeeMoreFoldHonorsCustomGroupTemplates(t *testing.T) {
	t.Parallel()

	envelope := seeMoreFoldOutboxEnvelope(domain.OutboxKindNewShort, seeMoreFoldScreenshotShortPayloads(t))

	t.Run("override without blank line folds after first line", func(t *testing.T) {
		t.Parallel()

		renderer, store := newSeeMoreFoldRendering(t, map[domain.TemplateKey]string{
			domain.TemplateKeyOutboxShortsGroup: "{{.MemberName}} 쇼츠 {{.Count}}개\n{{range .Items}}{{.Title}}\n{{.URL}}\n{{end}}",
		})

		plain := runSeeMoreFoldFinalPayload(t, renderer, store, false, envelope)
		folded := runSeeMoreFoldFinalPayload(t, renderer, store, true, envelope)

		requireFoldedAfterHead(t, folded, plain, "리오나 쇼츠 10개")
		logSeeMoreFoldFinalPayload(t, "custom override", folded, plain)
	})

	t.Run("override already folded stays single padded", func(t *testing.T) {
		t.Parallel()

		renderer, store := newSeeMoreFoldRendering(t, map[domain.TemplateKey]string{
			domain.TemplateKeyOutboxShortsGroup: "{{.MemberName}} 쇼츠 {{.Count}}개" + seeMoreFoldPadding() + "\n\n{{range .Items}}{{.Title}}\n{{.URL}}\n{{end}}",
		})

		plain := runSeeMoreFoldFinalPayload(t, renderer, store, false, envelope)
		folded := runSeeMoreFoldFinalPayload(t, renderer, store, true, envelope)

		require.Equal(t, plain, folded)
		require.Equal(t, 1, strings.Count(folded, seeMoreFoldPadding()))
		require.True(t, strings.HasPrefix(folded, "리오나 쇼츠 10개"+seeMoreFoldPadding()+"\n\n"))
	})
}
