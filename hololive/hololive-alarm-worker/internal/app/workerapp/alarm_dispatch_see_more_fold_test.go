package workerapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
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

type seeMoreFoldConsumer struct {
	batch      []dispatchoutbox.AlarmQueueEnvelope
	dispatched int
	failures   []string
	cancel     context.CancelFunc
}

func (c *seeMoreFoldConsumer) DrainBatch(context.Context, int) ([]dispatchoutbox.AlarmQueueEnvelope, error) {
	batch := c.batch

	c.batch = nil

	return batch, nil
}

func (*seeMoreFoldConsumer) MarkSending(context.Context, []dispatchoutbox.AlarmQueueEnvelope) error {
	return nil
}

func (c *seeMoreFoldConsumer) MarkDispatched(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, _ string, _ time.Duration) error {
	c.dispatched += len(envelopes)
	return nil
}

func (*seeMoreFoldConsumer) ReleaseClaimKeys(context.Context, []string) error {
	return nil
}

func (c *seeMoreFoldConsumer) ReleaseLeased(context.Context, []dispatchoutbox.AlarmQueueEnvelope) error {
	return c.unexpected("release leased")
}

func (c *seeMoreFoldConsumer) unexpected(route string) error {
	c.failures = append(c.failures, route)
	c.cancel()

	return fmt.Errorf("unexpected dispatch route: %s", route)
}

func (c *seeMoreFoldConsumer) RouteFailures(context.Context, []dispatchoutbox.AlarmQueueEnvelope, []dispatchoutbox.AlarmQueueEnvelope) error {
	return c.unexpected("retry or dead letter")
}

func (c *seeMoreFoldConsumer) RouteSendingFailures(context.Context, []dispatchoutbox.AlarmQueueEnvelope, []dispatchoutbox.AlarmQueueEnvelope) error {
	return c.unexpected("sending retry or dead letter")
}

func (c *seeMoreFoldConsumer) RequeuePreSend(context.Context, []dispatchoutbox.AlarmQueueEnvelope) error {
	return c.unexpected("pre-send requeue")
}

func (c *seeMoreFoldConsumer) Requeue(context.Context, []dispatchoutbox.AlarmQueueEnvelope) error {
	return c.unexpected("requeue")
}

func (c *seeMoreFoldConsumer) Quarantine(context.Context, []dispatchoutbox.AlarmQueueEnvelope, error) error {
	return c.unexpected("quarantine")
}

func (*seeMoreFoldConsumer) Wait(context.Context) bool { return false }

func (*seeMoreFoldConsumer) Reset() {}

// 알림 이름은 종단 문구가 되며 접기 판단과 무관하다.
type unregisteredMembers struct {
	domain.MemberDataProvider
}

func (unregisteredMembers) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func alarmDispatchRunnerTestEnvelope(roomID string, retry *dispatchoutbox.AlarmQueueRetryMetadata) dispatchoutbox.AlarmQueueEnvelope {
	return dispatchoutbox.AlarmQueueEnvelope{
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

type seeMoreFoldIrisClient struct {
	texts []string
}

func (c *seeMoreFoldIrisClient) SendMessageAccepted(_ context.Context, _, message string, _ ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	c.texts = append(c.texts, message)

	return &iris.ReplyAcceptedResponse{Success: true, Delivery: "queued", RequestID: "see-more-request"}, nil
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
func runSeeMoreFoldFinalPayload(t *testing.T, renderer *template.Renderer, store *messagestrings.Store, envelopes ...dispatchoutbox.AlarmQueueEnvelope) string {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	client := &seeMoreFoldIrisClient{}
	consumer := &seeMoreFoldConsumer{batch: envelopes, cancel: cancel}
	runner, err := alarmdispatch.NewRunner(consumer, egress.NewIrisMessageSender(client), renderer, store, consumer,
		alarmdispatch.RunnerConfig{MaxBatch: len(envelopes), Members: unregisteredMembers{}}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	require.NoError(t, runner.Start(ctx))
	require.Empty(t, consumer.failures)
	require.Equal(t, len(envelopes), consumer.dispatched)
	require.Len(t, client.texts, 1, "grouped envelopes must leave as one Kakao text message")

	return client.texts[0]
}

// 사용자 지정 본문의 단발 ZWSP는 접기 패딩으로 보지 않는다.
func requireNoAutoPadding(t *testing.T, final string) {
	t.Helper()

	require.NotContains(t, final, kakaotext.ZeroWidthSpace+kakaotext.ZeroWidthSpace)
}

func TestAlarmDispatchRunnerKeepsLongAlarmNotificationGroupUnfoldedAtFinalPayload(t *testing.T) {
	t.Parallel()

	renderer, store := newSeeMoreFoldRendering(t, nil)
	start := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	envelopes := make([]dispatchoutbox.AlarmQueueEnvelope, 0, 6)

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

	requireNoAutoPadding(t, final)

	require.Equal(t, len(envelopes), strings.Count(final, "https://youtube.com/watch?v="))
}

func (*seeMoreFoldConsumer) LoadSendRequest(context.Context, []dispatchoutbox.AlarmQueueEnvelope) (*dispatchoutbox.SendRequest, error) {
	return nil, dispatchoutbox.ErrSendRequestUnpinned
}

func (*seeMoreFoldConsumer) PinSendRequest(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, request dispatchoutbox.SendRequest) (*dispatchoutbox.SendRequest, error) {
	request.ClientRequestID = envelopes[0].ClientRequestID
	return &request, nil
}

func (c *seeMoreFoldConsumer) ReissueSendRequest(context.Context, []dispatchoutbox.AlarmQueueEnvelope, *dispatchoutbox.SendRequest) error {
	return c.unexpected("reissue")
}
