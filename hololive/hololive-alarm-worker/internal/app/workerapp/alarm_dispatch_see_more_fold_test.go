package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/egress/alarmdispatch"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
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

func (c *seeMoreFoldConsumer) MarkDispatched(_ context.Context, envelopes []domain.AlarmQueueEnvelope, _ string, _ time.Duration) error {
	c.dispatched += len(envelopes)
	return nil
}

func (*seeMoreFoldConsumer) ReleaseClaimKeys(context.Context, []string) error {
	return nil
}

func (c *seeMoreFoldConsumer) ReleaseLeased(context.Context, []domain.AlarmQueueEnvelope) error {
	return c.unexpected("release leased")
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

// unregisteredMembers는 모든 채널을 members 미등록으로 답한다. 알림 이름은 종단 문구가 되며 접기 판단과 무관하다.
type unregisteredMembers struct {
	domain.MemberDataProvider
}

func (unregisteredMembers) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func alarmDispatchRunnerTestEnvelope(roomID string, retry *domain.AlarmQueueRetryMetadata) domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		SendUnitID:      1,
		ClientRequestID: "hololive-alarm:0123456789abcdef0123456789abcdef",
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

func (c *seeMoreFoldIrisClient) SendMessageAccepted(_ context.Context, _, message string, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.texts = append(c.texts, message)

	return &iris.ReplyAcceptedResponse{Success: true, Delivery: "queued", RequestID: "see-more-request"}, nil
}

func (*seeMoreFoldIrisClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, errors.New("markdown lane is not configured")
}

func (*seeMoreFoldIrisClient) SendRichText(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, errors.New("richtext lane is not configured")
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

	store := messagestrings.NewStore(pool, logger)
	require.NoError(t, store.Load(t.Context()))
	require.NoError(t, store.Validate(messagestrings.AlarmWorkerEgressRequirements()))

	return template.NewRenderer(pool, logger), store
}

// runSeeMoreFoldFinalPayload는 실제 DB template와 Runner 렌더링을 거쳐 Iris 텍스트 lane에 넘어간 최종 문자열을 반환한다.
func runSeeMoreFoldFinalPayload(t *testing.T, renderer *template.Renderer, store *messagestrings.Store, envelopes ...domain.AlarmQueueEnvelope) string {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	client := &seeMoreFoldIrisClient{}
	consumer := &seeMoreFoldConsumer{batch: envelopes, cancel: cancel}
	runner := alarmdispatch.NewRunner(consumer, egress.NewIrisMessageSender(client), renderer, store, consumer,
		alarmdispatch.RunnerConfig{MaxBatch: len(envelopes), Members: unregisteredMembers{}}, slog.New(slog.DiscardHandler))

	require.NoError(t, runner.Start(ctx))
	require.Empty(t, consumer.failures)
	require.Equal(t, len(envelopes), consumer.dispatched)
	require.Len(t, client.texts, 1, "grouped envelopes must leave as one Kakao text message")

	return client.texts[0]
}

// requireNoAutoPadding은 접기 패딩 판정과 같은 기준(연속 ZWSP)으로 확인해, 길이가 다른 패딩이 다시 생겨도 잡는다.
// 템플릿의 단발 ZWSP(제목 Markdown 해석 방지)는 허용한다.
func requireNoAutoPadding(t *testing.T, final string) {
	t.Helper()

	require.NotContains(t, final, kakaotext.ZeroWidthSpace+kakaotext.ZeroWidthSpace)
}

func TestAlarmDispatchRunnerKeepsLongAlarmNotificationGroupUnfoldedAtFinalPayload(t *testing.T) {
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

	final := runSeeMoreFoldFinalPayload(t, renderer, store, envelopes...)

	head, _, _ := strings.Cut(final, "\n")
	require.True(t, strings.HasPrefix(final[len(head):], "\n\n"), "alarm header and body must stay separated: %q", final)
	require.Contains(t, head, "· 6개")
	requireNoAutoPadding(t, final)

	require.Equal(t, len(envelopes), strings.Count(final, "https://youtube.com/watch?v="))
}

func TestAlarmDispatchRunnerKeepsPreRenderedDigestUnpadded(t *testing.T) {
	t.Parallel()

	renderer, store := newSeeMoreFoldRendering(t, nil)

	digest := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	digest.Notification.AlarmType = domain.AlarmTypeCommunity
	digest.SourceKind = domain.AlarmDispatchSourceKindDeliveryDigest
	digest.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{
		Kind:               domain.DeliveryKindMemberNewsWeekly,
		PeriodKey:          "2026-W39",
		PreRenderedMessage: "주간 멤버 뉴스\n\n" + strings.Repeat("- 멤버 소식 항목이 이어집니다\n", 20),
	}

	storedMessage := digest.DeliveryDigest.PreRenderedMessage

	final := runSeeMoreFoldFinalPayload(t, renderer, store, digest)

	require.Greater(t, utf8.RuneCountInString(final), 250, "exercise a long message")
	requireNoAutoPadding(t, final)
	// 저장 본문은 그대로 두고, 최종 text lane의 기존 Markdown 변환만 허용한다.
	require.Equal(t, storedMessage, digest.DeliveryDigest.PreRenderedMessage)
	require.Equal(t, kakaoformat.Render(storedMessage), final)
}

func (*seeMoreFoldConsumer) LoadSendRequest(context.Context, []domain.AlarmQueueEnvelope) (*dispatchoutbox.SendRequest, error) {
	return nil, dispatchoutbox.ErrSendRequestUnpinned
}

func (*seeMoreFoldConsumer) PinSendRequest(_ context.Context, envelopes []domain.AlarmQueueEnvelope, request dispatchoutbox.SendRequest) (*dispatchoutbox.SendRequest, error) {
	request.ClientRequestID = envelopes[0].ClientRequestID
	return &request, nil
}

func (c *seeMoreFoldConsumer) ReissueSendRequest(context.Context, []domain.AlarmQueueEnvelope, *dispatchoutbox.SendRequest) error {
	return c.unexpected("reissue")
}
