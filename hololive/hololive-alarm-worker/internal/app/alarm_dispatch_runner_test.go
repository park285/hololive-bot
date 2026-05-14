package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errAlarmDispatchRunnerTestSend = errors.New("send failed")

type alarmDispatchRunnerTestConsumer struct {
	batches          [][]domain.AlarmQueueEnvelope
	markSending      []domain.AlarmQueueEnvelope
	markDispatched   []domain.AlarmQueueEnvelope
	scheduledRetry   []domain.AlarmQueueEnvelope
	movedDLQ         []domain.AlarmQueueEnvelope
	requeued         []domain.AlarmQueueEnvelope
	releasedClaims   []string
	scheduleRetryErr error
	moveDLQErr       error
}

func (c *alarmDispatchRunnerTestConsumer) DrainBatch(context.Context, int) ([]domain.AlarmQueueEnvelope, error) {
	if len(c.batches) == 0 {
		return nil, nil
	}
	batch := c.batches[0]
	c.batches = c.batches[1:]
	return batch, nil
}

func (c *alarmDispatchRunnerTestConsumer) MarkSending(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markSending = append(c.markSending, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) MarkDispatched(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markDispatched = append(c.markDispatched, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) ReleaseClaimKeys(_ context.Context, claimKeys []string) error {
	c.releasedClaims = append(c.releasedClaims, claimKeys...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) ScheduleRetry(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.scheduledRetry = append(c.scheduledRetry, envelopes...)
	return c.scheduleRetryErr
}

func (c *alarmDispatchRunnerTestConsumer) MoveToDLQ(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.movedDLQ = append(c.movedDLQ, envelopes...)
	return c.moveDLQErr
}

func (c *alarmDispatchRunnerTestConsumer) Requeue(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.requeued = append(c.requeued, envelopes...)
	return nil
}

type alarmDispatchRunnerTestSender struct {
	fail     bool
	roomID   string
	messages []string
}

func (s *alarmDispatchRunnerTestSender) SendMessage(_ context.Context, roomID, message string) error {
	s.roomID = roomID
	s.messages = append(s.messages, message)
	if s.fail {
		return errAlarmDispatchRunnerTestSend
	}
	return nil
}

func TestAlarmDispatchRunnerRunOnceSendsAndMarksDispatched(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Equal(t, "room-1", sender.roomID)
	require.Len(t, sender.messages, 1)
	assert.Contains(t, sender.messages[0], "방송 시작")
	assert.Len(t, consumer.markSending, 1)
	assert.Len(t, consumer.markDispatched, 1)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
}

func TestAlarmDispatchRunnerRunOnceSchedulesRetryOnSendFailure(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope("room-1", nil)}}}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledRetry, 1)
	require.NotNil(t, consumer.scheduledRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledRetry[0].Retry.Attempt)
	assert.Contains(t, consumer.scheduledRetry[0].Retry.LastError, errAlarmDispatchRunnerTestSend.Error())
	assert.Empty(t, consumer.markDispatched)
	assert.Empty(t, consumer.movedDLQ)
}

func TestAlarmDispatchRunnerRunOnceMovesExhaustedRetryToDLQAndReleasesClaims(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope("room-1", &domain.AlarmQueueRetryMetadata{Attempt: 2})
	envelope.ClaimKeys = []string{"alarm:dispatch:claim:room-1:stream-1"}
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.scheduledRetry)
	require.Len(t, consumer.movedDLQ, 1)
	require.NotNil(t, consumer.movedDLQ[0].Retry)
	assert.Equal(t, 3, consumer.movedDLQ[0].Retry.Attempt)
	assert.Equal(t, []string{"alarm:dispatch:claim:room-1:stream-1"}, consumer.releasedClaims)
}

func TestGroupAlarmDispatchEnvelopesSeparatesScheduledMinuteBuckets(t *testing.T) {
	firstStart := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	secondStart := firstStart.Add(time.Minute)
	first := alarmDispatchRunnerTestEnvelope("room-1", nil)
	second := alarmDispatchRunnerTestEnvelope("room-1", nil)
	first.Notification.Stream.StartScheduled = &firstStart
	second.Notification.Stream.StartScheduled = &secondStart

	groups := groupAlarmDispatchEnvelopes([]domain.AlarmQueueEnvelope{first, second})

	assert.Len(t, groups, 2)
}

func TestRenderAlarmDispatchNotificationGroupMatchesLegacyValkeyRenderer(t *testing.T) {
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	first := alarmDispatchRunnerTestEnvelope("room-1", nil)
	second := alarmDispatchRunnerTestEnvelope("room-1", nil)
	first.Notification.MinutesUntil = 3
	second.Notification.MinutesUntil = 1
	first.Notification.Channel.Name = "Member1"
	second.Notification.Channel.Name = "Member2"
	first.Notification.Stream.ID = "abc"
	second.Notification.Stream.ID = "def"
	first.Notification.Stream.Title = "Title1"
	second.Notification.Stream.Title = "Title2"
	first.Notification.Stream.StartScheduled = &start
	second.Notification.Stream.StartScheduled = &start
	group := groupAlarmDispatchEnvelopes([]domain.AlarmQueueEnvelope{first, second})[0]

	message, err := renderAlarmDispatchGroup(t.Context(), group)

	require.NoError(t, err)
	assert.Equal(t, "⏰ 방송 1분 전 알림\n\n"+
		"⏰ Member1 방송 3분 전\n📺 Title1\n🔗 https://youtube.com/watch?v=abc\n\n"+
		"⏰ Member2 방송 예정\n📺 Title2\n🔗 https://youtube.com/watch?v=def", message)
}

func TestRenderAlarmDispatchNotificationLiveCatchupUsesRecoveredUpcomingMessage(t *testing.T) {
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	notification := alarmDispatchRunnerTestEnvelope("room-1", nil).Notification
	notification.MinutesUntil = 5
	notification.Channel.Name = "Member"
	notification.Stream.ID = "live-1"
	notification.Stream.Title = "Live Title"
	notification.Stream.StartScheduled = &start
	notification.Stream.StartActual = &start

	got := renderAlarmDispatchNotification(notification)

	assert.Equal(t,
		"⏰ Member 방송 5분 전\n📺 Live Title\n🔗 https://youtube.com/watch?v=live-1",
		got,
	)
}

func TestResolveAlarmDispatchURLFallsBackLikeLegacyValkeyRenderer(t *testing.T) {
	twitchOnlyWithoutURL := alarmDispatchRunnerTestEnvelope("room-1", nil).Notification
	twitchOnlyWithoutURL.Stream.IsTwitchOnly = true

	chzzkOnlyWithoutURL := alarmDispatchRunnerTestEnvelope("room-1", nil).Notification
	chzzkOnlyWithoutURL.Stream.IsChzzkOnly = true

	assert.Equal(t, "https://youtube.com/watch?v=stream-1", resolveAlarmDispatchURL(twitchOnlyWithoutURL))
	assert.Equal(t, "https://youtube.com/watch?v=stream-1", resolveAlarmDispatchURL(chzzkOnlyWithoutURL))
}

func alarmDispatchRunnerTestEnvelope(roomID string, retry *domain.AlarmQueueRetryMetadata) domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			RoomID:       roomID,
			MinutesUntil: 0,
			Channel:      &domain.Channel{Name: "Test Member"},
			Stream: &domain.Stream{
				ID:    "stream-1",
				Title: "Test Stream",
			},
		},
		Retry: retry,
	}
}
