package alarmdispatch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
	"github.com/kapu/hololive-shared/pkg/util"
)

var errAlarmDispatchRunnerTestSend = errors.New("send failed")

type alarmDispatchRunnerTestConsumer struct {
	alarmRequestTestStore

	batches               [][]domain.AlarmQueueEnvelope
	drainErr              error
	onDrain               func()
	markSending           []domain.AlarmQueueEnvelope
	markDispatched        []domain.AlarmQueueEnvelope
	quarantined           []domain.AlarmQueueEnvelope
	quarantineReason      string
	scheduledRetry        []domain.AlarmQueueEnvelope
	scheduledSendingRetry []domain.AlarmQueueEnvelope
	preSendRequeued       []domain.AlarmQueueEnvelope
	movedDLQ              []domain.AlarmQueueEnvelope
	requeued              []domain.AlarmQueueEnvelope
	releasedClaims        []string
	markSendingErr        error
	markDispatchedErr     error
	quarantineErr         error
	routeFailuresErr      error
}

func (c *alarmDispatchRunnerTestConsumer) DrainBatch(context.Context, int) ([]domain.AlarmQueueEnvelope, error) {
	if c.onDrain != nil {
		c.onDrain()
	}

	if c.drainErr != nil {
		return nil, c.drainErr
	}

	if len(c.batches) == 0 {
		return nil, nil
	}

	batch := c.batches[0]

	c.batches = c.batches[1:]

	return withAlarmDispatchTestSendUnitIdentity(batch), nil
}

// 운영 claim은 send unit이 저장된 delivery만 돌려주고, dispatchoutbox는 dispatch group마다 client_request_id를
// 저장한다. 식별자를 직접 지정하지 않은 테스트 봉투에는 같은 그룹 키로 결정적인 ID를 붙여 그 계약을 흉내 낸다.
func withAlarmDispatchTestSendUnitIdentity(batch []domain.AlarmQueueEnvelope) []domain.AlarmQueueEnvelope {
	out := make([]domain.AlarmQueueEnvelope, len(batch))
	for i := range batch {
		out[i] = batch[i]
		if out[i].SendUnitID == 0 && out[i].ClientRequestID == "" {
			sum := sha256.Sum256([]byte(alarmDispatchGroupKey(&out[i])))

			out[i].ClientRequestID = "hololive-alarm:test-" + hex.EncodeToString(sum[:8])
		}
	}

	return out
}

func (c *alarmDispatchRunnerTestConsumer) MarkSending(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markSending = append(c.markSending, envelopes...)
	return c.markSendingErr
}

func (c *alarmDispatchRunnerTestConsumer) MarkDispatched(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markDispatched = append(c.markDispatched, envelopes...)
	return c.markDispatchedErr
}

func (c *alarmDispatchRunnerTestConsumer) Quarantine(_ context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	c.quarantined = append(c.quarantined, envelopes...)

	if cause != nil {
		c.quarantineReason = cause.Error()
	}

	return c.quarantineErr
}

func (c *alarmDispatchRunnerTestConsumer) ReleaseClaimKeys(_ context.Context, claimKeys []string) error {
	c.releasedClaims = append(c.releasedClaims, claimKeys...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) RouteFailures(_ context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	c.scheduledRetry = append(c.scheduledRetry, retryEnvelopes...)
	c.movedDLQ = append(c.movedDLQ, dlqEnvelopes...)

	return c.routeFailuresErr
}

func (c *alarmDispatchRunnerTestConsumer) RouteSendingFailures(_ context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	c.scheduledSendingRetry = append(c.scheduledSendingRetry, retryEnvelopes...)
	c.movedDLQ = append(c.movedDLQ, dlqEnvelopes...)

	return nil
}

func (c *alarmDispatchRunnerTestConsumer) RequeuePreSend(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.preSendRequeued = append(c.preSendRequeued, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) Requeue(_ context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.requeued = append(c.requeued, envelopes...)
	return nil
}

type alarmDispatchRunnerTestSender struct {
	fail             bool
	messageErr       error
	roomID           string
	messages         []string
	clientRequestIDs []string
}

func (s *alarmDispatchRunnerTestSender) SendMessage(_ context.Context, roomID, message string) error {
	s.roomID = roomID
	s.messages = append(s.messages, message)

	if s.messageErr != nil {
		return s.messageErr
	}

	if s.fail {
		return errAlarmDispatchRunnerTestSend
	}

	return nil
}

func (s *alarmDispatchRunnerTestSender) SendMessageWithClientRequestID(_ context.Context, roomID, message, clientRequestID string) error {
	s.roomID = roomID
	s.messages = append(s.messages, message)
	s.clientRequestIDs = append(s.clientRequestIDs, clientRequestID)

	if s.messageErr != nil {
		return s.messageErr
	}

	if s.fail {
		return errAlarmDispatchRunnerTestSend
	}

	return nil
}

func TestAlarmDispatchRunnerRunOnceSendsAndMarksDispatched(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	// YouTube 방송 알림도 Text 경로로 저장된 send-unit ID를 붙여 보낸다(DEC-20260926-hololive-karing-egress-disposition).
	assert.Equal(t, testAlarmRoomID, sender.roomID)
	require.Len(t, sender.messages, 1)
	require.Len(t, sender.clientRequestIDs, 1)
	assert.Contains(t, sender.clientRequestIDs[0], "hololive-alarm:")
	assert.Len(t, consumer.markSending, 1)
	assert.Len(t, consumer.markDispatched, 1)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
}

func TestAlarmDispatchRunnerRejectsRetiredStreamProviders(t *testing.T) {
	testCases := []struct {
		name      string
		configure func(*domain.Stream)
	}{
		{
			name: "twitch only",
			configure: func(stream *domain.Stream) {
				stream.IsTwitchOnly = true
				stream.TwitchLiveURL = testTwitchLiveURL
			},
		},
		{
			name: "chzzk only",
			configure: func(stream *domain.Stream) {
				stream.IsChzzkOnly = true
				stream.ChzzkLiveURL = "https://chzzk.naver.com/live/member"
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
			tc.configure(envelope.Notification.Stream)

			envelope.Retry = &domain.AlarmQueueRetryMetadata{Attempt: alarmDispatchMaxAttempts}

			var logs bytes.Buffer

			consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
			sender := &alarmDispatchRunnerTestSender{}
			runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10, logger: slog.New(slog.NewTextHandler(&logs, nil))}

			processed, err := runner.runOnce(t.Context())

			require.NoError(t, err)
			assert.True(t, processed)
			assert.Empty(t, sender.messages)
			assert.Empty(t, consumer.markSending)
			assert.Empty(t, consumer.markDispatched)
			require.Len(t, consumer.movedDLQ, 1)
			assert.Empty(t, consumer.scheduledRetry)
			// 드레인 종단은 조용히 버리지 않고 error 로그로 드러나야 한다.
			assert.Contains(t, logs.String(), "level=ERROR")
			assert.Contains(t, logs.String(), "retired stream provider envelope")
		})
	}
}

func TestAlarmDispatchRunnerQuarantinesReplyHandoffOutcomeUnknownWithoutRetry(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.SendUnitID = 7
	envelope.ClientRequestID = testRetryClientRequestID

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{
		messageErr: errors.Join(sendoutcome.ErrHandoffOutcomeUnknown, context.DeadlineExceeded),
	}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Len(t, sender.messages, 1)
	assert.Len(t, consumer.quarantined, 1)
	assert.Empty(t, consumer.scheduledSendingRetry)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerQuarantinesPGSendFailureAfterMarkSending(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.quarantined, 1)
	assert.Contains(t, consumer.quarantineReason, errAlarmDispatchRunnerTestSend.Error())
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerRetriesBadGatewayAfterMarkSending(t *testing.T) {
	sendErr := fmt.Errorf("iris send message: %w", &iris.HTTPError{StatusCode: 502, URL: testIrisReplyPath})
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	sender := &alarmDispatchRunnerTestSender{messageErr: sendErr}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.scheduledSendingRetry, 1)
	require.NotNil(t, consumer.scheduledSendingRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledSendingRetry[0].Retry.Attempt)
	assert.Contains(t, consumer.scheduledSendingRetry[0].Retry.LastError, "returned 502")
	assert.Equal(t, dispatchoutbox.ErrorCodeHTTP5xx, consumer.scheduledSendingRetry[0].Retry.LastErrorCode)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerReturnsErrorWhenPostSendQuarantineFails(t *testing.T) {
	quarantineErr := errors.New("quarantine failed")
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:       [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}},
		quarantineErr: quarantineErr,
	}
	sender := &alarmDispatchRunnerTestSender{fail: true}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.Error(t, err)
	assert.True(t, processed)
	require.ErrorIs(t, err, quarantineErr)
	assert.Empty(t, consumer.scheduledRetry)
}

func TestAlarmDispatchRunnerConsumesAttemptForRenderFailureBeforeMarkSending(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = nil

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledRetry, 1)
	require.NotNil(t, consumer.scheduledRetry[0].Retry)
	assert.Equal(t, 1, consumer.scheduledRetry[0].Retry.Attempt)
	assert.Empty(t, consumer.preSendRequeued)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markSending)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, sender.messages)
}

func TestAlarmDispatchRunnerDoesNotRetryMarkDispatchedFailureAfterSend(t *testing.T) {
	markErr := &dispatchoutbox.PartialTransitionError{
		Action: "mark dispatch deliveries sent", Updated: 0, Expected: 1,
	}
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:           [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}},
		markDispatchedErr: markErr,
	}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.Error(t, err)
	assert.True(t, processed)
	require.ErrorIs(t, err, markErr)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, consumer.requeued)
	require.Len(t, consumer.markDispatched, 1)
}

func TestAlarmDispatchRunnerRunOnceMovesExhaustedRetryToDLQAndReleasesClaims(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &domain.AlarmQueueRetryMetadata{Attempt: alarmDispatchRetryableMaxAttempts - 1})

	envelope.ClaimKeys = []string{testAlarmClaimKey}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{messageErr: &iris.HTTPError{StatusCode: 503}}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.scheduledSendingRetry)
	require.Len(t, consumer.movedDLQ, 1)
	require.NotNil(t, consumer.movedDLQ[0].Retry)
	assert.Equal(t, alarmDispatchRetryableMaxAttempts, consumer.movedDLQ[0].Retry.Attempt)
	assert.Equal(t, []string{testAlarmClaimKey}, consumer.releasedClaims)
}

func TestAlarmDispatchRunnerKeepsRetryingRetryableCauseBeyondBaseAttemptCap(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &domain.AlarmQueueRetryMetadata{Attempt: alarmDispatchMaxAttempts - 1})

	envelope.ClaimKeys = []string{testAlarmClaimKey}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{messageErr: &iris.HTTPError{StatusCode: 503}}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.movedDLQ, "retryable cause must not hit the base attempt cap")
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 1)
	require.NotNil(t, consumer.scheduledSendingRetry[0].Retry)
	assert.Equal(t, alarmDispatchMaxAttempts, consumer.scheduledSendingRetry[0].Retry.Attempt)
	assert.Empty(t, consumer.releasedClaims, "claim keys stay held while the envelope is still retryable")
}

// Karing 경로가 없어져 방송 알림 그룹도 방 유형과 무관하게 같은 Text 경로와 저장된 send-unit ID로 다시 나간다.
// 그래서 ambiguous 실패는 다른 source와 같은 규칙으로 재시도하고, 재시도 요청은 같은 ID로 admission 중복 제거에 접힌다.
func TestAlarmDispatchRunnerRetriesPersistedStreamGroupDeadline(t *testing.T) {
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	second.DispatchOutboxID = 2

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{first, second}}}
	sender := &alarmDispatchRunnerTestSender{
		messageErr: fmt.Errorf("send iris text: %w", context.DeadlineExceeded),
	}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, sender.clientRequestIDs, 1)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 2)
	assert.Equal(t, sender.clientRequestIDs[0], consumer.scheduledSendingRetry[0].ClientRequestID)
	assert.Equal(t, sender.clientRequestIDs[0], consumer.scheduledSendingRetry[1].ClientRequestID)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerRetriesIntrinsicTextTransportFailure(t *testing.T) {
	transportErr := &iris.TransportError{Op: testIrisPostOp, URL: testIrisReplyPath, Err: errors.New("connection refused")}
	envelope := alarmDispatchRunnerIntrinsicTextEnvelope()
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{messageErr: transportErr}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	assert.Empty(t, consumer.quarantined)
	require.Len(t, consumer.scheduledSendingRetry, 1)
	assert.Empty(t, consumer.markDispatched)
}

func TestAlarmDispatchRunnerNonRetryableHTTPFailureStillQuarantines(t *testing.T) {
	for _, statusCode := range []int{500, 504, 401, 403} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
			consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
			sender := &alarmDispatchRunnerTestSender{messageErr: &iris.HTTPError{StatusCode: statusCode}}
			runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

			processed, err := runner.runOnce(t.Context())

			require.NoError(t, err)
			assert.True(t, processed)
			require.Len(t, consumer.quarantined, 1, "ambiguous-outcome status must stay terminal")
			assert.Empty(t, consumer.scheduledSendingRetry)
			assert.Empty(t, consumer.markDispatched)
		})
	}
}

func TestAlarmDispatchRunnerWaitsOnIdleWaiterForEmptyPGBatch(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: false}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: &alarmDispatchRunnerTestSender{}, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10, idleWaiter: waiter}

	keepGoing := runner.runStep(t.Context())

	assert.False(t, keepGoing)
	assert.Equal(t, 1, waiter.waits)
	assert.Zero(t, waiter.resets)
}

func TestAlarmDispatchRunnerResetsIdleWaiterAfterProcessedBatch(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: true}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: &alarmDispatchRunnerTestSender{}, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10, idleWaiter: waiter}

	keepGoing := runner.runStep(t.Context())

	assert.True(t, keepGoing)
	assert.Zero(t, waiter.waits)
	assert.Equal(t, 1, waiter.resets)
}

func TestAlarmDispatchRunnerYieldsAfterMaxBatchesPerWake(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{
		{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)},
		{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)},
	}}
	yieldCount := 0
	runner := Runner{
		members:           alarmGoldenMembers{},
		consumer:          consumer,
		sender:            &alarmDispatchRunnerTestSender{},
		renderer:          newAlarmDispatchTestRenderer(t),
		maxBatch:          10,
		maxBatchesPerWake: 2,
		yield: func(context.Context) bool {
			yieldCount++
			return true
		},
	}

	assert.True(t, runner.runStep(t.Context()))
	assert.Zero(t, yieldCount)
	assert.True(t, runner.runStep(t.Context()))
	assert.Equal(t, 1, yieldCount)
}

func TestAlarmDispatchRunnerStartProcessesBatchesUntilIdleWaitStops(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{
		{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)},
		{alarmDispatchRunnerTestEnvelope("room-2", nil)},
	}}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: false}
	sender := &alarmDispatchRunnerTestSender{}
	runner := &Runner{
		members:    alarmGoldenMembers{},
		consumer:   consumer,
		sender:     sender,
		renderer:   newAlarmDispatchTestRenderer(t),
		maxBatch:   10,
		idleWaiter: waiter,
	}

	err := runner.Start(t.Context())

	require.NoError(t, err)
	require.Len(t, consumer.markDispatched, 2)
	assert.Equal(t, []string{testAlarmRoomID, "room-2"}, []string{consumer.markDispatched[0].Notification.RoomID, consumer.markDispatched[1].Notification.RoomID})
	assert.Len(t, sender.messages, 2)
	assert.Equal(t, 2, waiter.resets)
	assert.Equal(t, 1, waiter.waits)
	assert.Zero(t, runner.batchesSinceWake)
}

func TestAlarmDispatchRunnerRunStepStopsWhenDrainErrorArrivesAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	consumer := &alarmDispatchRunnerTestConsumer{
		drainErr: errors.New("drain failed"),
		onDrain:  cancel,
	}
	runner := Runner{
		members:  alarmGoldenMembers{},
		consumer: consumer,
		sender:   &alarmDispatchRunnerTestSender{},
		maxBatch: 10,
	}

	keepGoing := runner.runStep(ctx)

	assert.False(t, keepGoing)
	assert.Empty(t, consumer.markDispatched)
}

func TestGroupAlarmDispatchEnvelopesForDeliveryPreservesScheduledMinuteBuckets(t *testing.T) {
	firstStart := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	secondStart := firstStart.Add(time.Minute)
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.Notification.Stream.StartScheduled = &firstStart
	second.Notification.Stream.StartScheduled = &secondStart

	groups := groupAlarmDispatchEnvelopesForDelivery([]domain.AlarmQueueEnvelope{first, second})

	assert.Len(t, groups, 2)
}

func TestRenderAlarmDispatchNotificationGroupUsesCanonicalTemplate(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.Notification.MinutesUntil = 3
	second.Notification.MinutesUntil = 1
	first.Notification.Channel.Name = "Member1"
	first.Notification.Stream.ChannelID = alarmGoldenChannelPrefix + "Member1"
	second.Notification.Channel.Name = "Member2"
	second.Notification.Stream.ChannelID = alarmGoldenChannelPrefix + "Member2"
	first.Notification.Stream.ID = "abc"
	second.Notification.Stream.ID = "def"
	first.Notification.Stream.Title = "Title1"
	second.Notification.Stream.Title = "Title2"
	first.Notification.Stream.StartScheduled = &start
	second.Notification.Stream.StartScheduled = &start

	group := groupAlarmDispatchEnvelopesForDelivery([]domain.AlarmQueueEnvelope{first, second})[0]

	message, err := renderAlarmDispatchGroup(t.Context(), newAlarmDispatchTestRenderer(t), nil, alarmGoldenMembers{}, "", group)

	require.NoError(t, err)
	assert.Equal(t, "⏰ 방송 1분 전 · 2개\n\n"+
		"1 · ⏰ Member1 방송 3분 전\n\u200bTitle1\nhttps://youtube.com/watch?v=abc\n──────────\n"+
		"2 · ⏰ Member2 방송 예정\n\u200bTitle2\nhttps://youtube.com/watch?v=def", message)
}

func TestRenderAlarmDispatchNotificationGroupAllLiveCatchupUsesStartingHeader(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.Notification.MinutesUntil = 5
	second.Notification.MinutesUntil = 5
	first.Notification.Channel.Name = "Member1"
	first.Notification.Stream.ChannelID = alarmGoldenChannelPrefix + "Member1"
	second.Notification.Channel.Name = "Member2"
	second.Notification.Stream.ChannelID = alarmGoldenChannelPrefix + "Member2"
	first.Notification.Stream.ID = "abc"
	second.Notification.Stream.ID = "def"
	first.Notification.Stream.Title = "Title1"
	second.Notification.Stream.Title = "Title2"
	first.Notification.Stream.StartActual = &start
	second.Notification.Stream.StartActual = &start

	group := alarmDispatchGroup{
		roomID:        testAlarmRoomID,
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{first.Notification, second.Notification},
	}

	message, err := renderAlarmDispatchNotificationGroup(t.Context(), newAlarmDispatchTestRenderer(t), nil, alarmGoldenMembers{}, "", group)

	require.NoError(t, err)
	assert.Equal(t, "🔴 방송 시작 · 2개\n\n"+
		"1 · 🔴 Member1 방송 시작\n\u200bTitle1\nhttps://youtube.com/watch?v=abc\n──────────\n"+
		"2 · 🔴 Member2 방송 시작\n\u200bTitle2\nhttps://youtube.com/watch?v=def", message)
}

func TestRenderAlarmDispatchNotificationGroupMixedCatchupKeepsConservativeHeader(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	first.Notification.MinutesUntil = 5
	second.Notification.MinutesUntil = 5
	first.Notification.Channel.Name = "LiveMember"
	first.Notification.Stream.ChannelID = alarmGoldenChannelPrefix + "LiveMember"
	second.Notification.Channel.Name = "UpcomingMember"
	second.Notification.Stream.ChannelID = alarmGoldenChannelPrefix + "UpcomingMember"
	first.Notification.Stream.ID = "live"
	second.Notification.Stream.ID = "upcoming"
	first.Notification.Stream.Title = "Live Title"
	second.Notification.Stream.Title = "Upcoming Title"
	first.Notification.Stream.StartActual = &start
	second.Notification.Stream.StartScheduled = &start

	group := alarmDispatchGroup{
		roomID:        testAlarmRoomID,
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{first.Notification, second.Notification},
	}

	message, err := renderAlarmDispatchNotificationGroup(t.Context(), newAlarmDispatchTestRenderer(t), nil, alarmGoldenMembers{}, "", group)

	require.NoError(t, err)
	assert.Equal(t, "⏰ 방송 5분 전 · 2개\n\n"+
		"1 · 🔴 LiveMember 방송 시작\n\u200bLive Title\nhttps://youtube.com/watch?v=live\n──────────\n"+
		"2 · ⏰ UpcomingMember 방송 예정\n\u200bUpcoming Title\nhttps://youtube.com/watch?v=upcoming", message)
}

func TestRenderAlarmDispatchNotificationLiveCatchupUsesRecoveredUpcomingMessage(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = testAlarmMemberName
	notification.Stream.ChannelID = alarmGoldenChannelPrefix + testAlarmMemberName
	notification.Stream.ID = "live-1"
	notification.Stream.Title = "Live Title"
	notification.Stream.StartScheduled = &start
	notification.Stream.StartActual = &start

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, alarmGoldenMembers{}, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		"🔴 Member 방송 시작\n\u200bLive Title\nhttps://youtube.com/watch?v=live-1",
		got,
	)
}

func TestRenderAlarmDispatchNotificationLiveStatusUsesStartingMessage(t *testing.T) {
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = testAlarmMemberName
	notification.Stream.ChannelID = alarmGoldenChannelPrefix + testAlarmMemberName
	notification.Stream.ID = "live-status-1"
	notification.Stream.Title = "Live Title"
	notification.Stream.Status = domain.StreamStatusLive

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, alarmGoldenMembers{}, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		"🔴 Member 방송 시작\n\u200bLive Title\nhttps://youtube.com/watch?v=live-status-1",
		got,
	)
}

func TestRenderAlarmDispatchNotificationUpcomingKeepsPreliveMessage(t *testing.T) {
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = testAlarmMemberName
	notification.Stream.ChannelID = alarmGoldenChannelPrefix + testAlarmMemberName
	notification.Stream.ID = "upcoming-1"
	notification.Stream.Title = "Upcoming Title"
	notification.Stream.Status = domain.StreamStatusUpcoming

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, alarmGoldenMembers{}, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		"⏰ Member 방송 5분 전\n\u200bUpcoming Title\nhttps://youtube.com/watch?v=upcoming-1",
		got,
	)
}

func TestRenderAlarmDispatchNotificationSeparatesLongTitleAndURL(t *testing.T) {
	const (
		title = "【ホロライブ ドリームス】水着きちゃ!音ゲー初心者!hololive Dreamsやってみる!【#" + util.KakaoZeroWidthSpace +
			"綺々羅々ヴィヴィ #" + util.KakaoZeroWidthSpace + "hololiveDEV_" + util.KakaoZeroWidthSpace +
			"IS #" + util.KakaoZeroWidthSpace + "FLOWGLOW】"
		streamURL = "https://www.youtube.com/watch?v=DCW0CvsJAnw"
	)

	link := streamURL
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = "비비"
	notification.Stream.ChannelID = alarmGoldenChannelPrefix + "비비"
	notification.Stream.ID = "DCW0CvsJAnw"
	notification.Stream.Title = title
	notification.Stream.Link = &link
	notification.Stream.Status = domain.StreamStatusUpcoming

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, alarmGoldenMembers{}, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		fmt.Sprintf("⏰ 비비 방송 5분 전\n%s%s\n%s", util.KakaoZeroWidthSpace, util.MarkdownNeutralize(string([]rune(strings.ReplaceAll(title, util.KakaoZeroWidthSpace, ""))[:61])+"..."), streamURL),
		got,
	)
}

func TestRenderAlarmDispatchNotificationOmitsRetiredSimulcastLink(t *testing.T) {
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = "비비"
	notification.Stream.ChannelID = alarmGoldenChannelPrefix + "비비"
	notification.Stream.ID = "integrated-1"
	notification.Stream.Title = "동시송출 방송"
	notification.Stream.IsIntegrated = true
	notification.Stream.ChzzkLiveURL = "https://chzzk.naver.com/live/integrated-1"

	got, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, alarmGoldenMembers{}, &notification)

	require.NoError(t, err)
	assert.Equal(t,
		"⏰ 비비 방송 5분 전\n"+util.KakaoZeroWidthSpace+
			"동시송출 방송\nhttps://youtube.com/watch?v=integrated-1",
		got,
	)
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

type alarmDispatchRunnerTestIdleWaiter struct {
	waits       int
	resets      int
	returnValue bool
}

func (w *alarmDispatchRunnerTestIdleWaiter) Wait(context.Context) bool {
	w.waits++
	return w.returnValue
}

func (w *alarmDispatchRunnerTestIdleWaiter) Reset() {
	w.resets++
}

type alarmDispatchRunnerContextConsumer struct {
	alarmDispatchRunnerTestConsumer

	markSendingCtxErr    error
	markDispatchedCtxErr error
	routeSendingCtxErr   error
	quarantineCtxErr     error
	routeSendingDeadline bool
	quarantineDeadline   bool
}

func (c *alarmDispatchRunnerContextConsumer) MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markSendingCtxErr = ctx.Err()
	if c.markSendingCtxErr != nil {
		return fmt.Errorf("mark alarm dispatch sending: %w", c.markSendingCtxErr)
	}

	if err := c.alarmDispatchRunnerTestConsumer.MarkSending(ctx, envelopes); err != nil {
		return fmt.Errorf("mark sending: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markDispatchedCtxErr = ctx.Err()
	if c.markDispatchedCtxErr != nil {
		return fmt.Errorf("mark alarm dispatch sent: %w", c.markDispatchedCtxErr)
	}

	if err := c.alarmDispatchRunnerTestConsumer.MarkDispatched(ctx, envelopes); err != nil {
		return fmt.Errorf("mark dispatched: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) RouteSendingFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	c.routeSendingCtxErr = ctx.Err()
	if c.routeSendingCtxErr != nil {
		return fmt.Errorf("route alarm dispatch sending failure: %w", c.routeSendingCtxErr)
	}

	_, c.routeSendingDeadline = ctx.Deadline()

	if err := c.alarmDispatchRunnerTestConsumer.RouteSendingFailures(ctx, retryEnvelopes, dlqEnvelopes); err != nil {
		return fmt.Errorf("route sending failures: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	c.quarantineCtxErr = ctx.Err()
	if c.quarantineCtxErr != nil {
		return fmt.Errorf("quarantine alarm dispatch: %w", c.quarantineCtxErr)
	}

	_, c.quarantineDeadline = ctx.Deadline()

	if err := c.alarmDispatchRunnerTestConsumer.Quarantine(ctx, envelopes, cause); err != nil {
		return fmt.Errorf("quarantine: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("release alarm dispatch claim keys: %w", err)
	}

	if err := c.alarmDispatchRunnerTestConsumer.ReleaseClaimKeys(ctx, claimKeys); err != nil {
		return fmt.Errorf("release claim keys: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) RequeuePreSend(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("requeue alarm dispatch before send: %w", err)
	}

	if err := c.alarmDispatchRunnerTestConsumer.RequeuePreSend(ctx, envelopes); err != nil {
		return fmt.Errorf("requeue pre send: %w", err)
	}

	return nil
}

type alarmDispatchRunnerBlockingSender struct {
	rooms   []string
	onSend  func()
	succeed bool
}

func alarmDispatchRunnerIntrinsicTextEnvelope() domain.AlarmQueueEnvelope {
	return alarmDispatchRunnerPreRenderedTextEnvelope(testAlarmRoomID)
}

// attempt deadline 검증은 렌더러의 실제 DB I/O와 분리한다.
func alarmDispatchRunnerPreRenderedTextEnvelope(roomID string) domain.AlarmQueueEnvelope {
	envelope := alarmDispatchRunnerTestEnvelope(roomID, nil)

	envelope.Notification.AlarmType = domain.AlarmTypeCommunity
	envelope.SourceKind = domain.AlarmDispatchSourceKindDeliveryDigest
	envelope.DeliveryDigest = &domain.DeliveryDigestDispatchPayload{
		Kind:               domain.DeliveryKindMemberNewsWeekly,
		PeriodKey:          "2026-W32",
		PreRenderedMessage: "주간 멤버 뉴스",
	}

	return envelope
}

func (s *alarmDispatchRunnerBlockingSender) waitForAttemptEnd(ctx context.Context, roomID string) error {
	s.rooms = append(s.rooms, roomID)
	if s.onSend != nil {
		s.onSend()
	}

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		return errors.New("send alarm dispatch message: attempt context never ended")
	}

	if s.succeed {
		return nil
	}

	return fmt.Errorf("send alarm dispatch message: %w", ctx.Err())
}

func (s *alarmDispatchRunnerBlockingSender) SendMessage(ctx context.Context, roomID, _ string) error {
	if err := s.waitForAttemptEnd(ctx, roomID); err != nil {
		return fmt.Errorf("wait for attempt end: %w", err)
	}

	return nil
}

func TestAlarmDispatchRunnerRoutesSendingRetryWithLiveContextAfterAttemptDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		consumer := &alarmDispatchRunnerContextConsumer{}

		consumer.batches = [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerPreRenderedTextEnvelope(testAlarmRoomID)}}

		sender := &alarmDispatchRunnerBlockingSender{}
		runner := Runner{
			members:        alarmGoldenMembers{},
			consumer:       consumer,
			sender:         sender,
			maxBatch:       10,
			attemptTimeout: 50 * time.Millisecond,
		}

		processed, err := runner.runOnce(t.Context())

		require.NoError(t, err)
		assert.True(t, processed)
		require.NoError(t, consumer.routeSendingCtxErr, "attempt 만료 뒤에도 실패 라우팅은 살아있는 컨텍스트로 실행돼야 한다")
		assert.True(t, consumer.routeSendingDeadline, "정리 컨텍스트도 시간 상한을 가져야 한다")
		require.Len(t, consumer.scheduledSendingRetry, 1)
		assert.Empty(t, consumer.quarantined)
		assert.Empty(t, consumer.markDispatched)
	})
}

func TestAlarmDispatchRunnerMarksDispatchedAfterAttemptDeadlineExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		consumer := &alarmDispatchRunnerContextConsumer{}

		consumer.batches = [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerPreRenderedTextEnvelope(testAlarmRoomID)}}

		sender := &alarmDispatchRunnerBlockingSender{succeed: true}
		runner := Runner{
			members:        alarmGoldenMembers{},
			consumer:       consumer,
			sender:         sender,
			maxBatch:       10,
			attemptTimeout: 50 * time.Millisecond,
		}

		processed, err := runner.runOnce(t.Context())

		require.NoError(t, err)
		assert.True(t, processed)
		require.NoError(t, consumer.markDispatchedCtxErr, "발송에 성공한 배치는 attempt 만료 뒤에도 sent로 기록돼야 한다")
		require.Len(t, consumer.markDispatched, 1)
		assert.Empty(t, consumer.scheduledSendingRetry)
		assert.Empty(t, consumer.quarantined)
	})
}

func TestAlarmDispatchRunnerStopsRemainingGroupsWhenAttemptDeadlineExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		consumer := &alarmDispatchRunnerContextConsumer{}

		consumer.batches = [][]domain.AlarmQueueEnvelope{{
			alarmDispatchRunnerPreRenderedTextEnvelope(testAlarmRoomID),
			alarmDispatchRunnerPreRenderedTextEnvelope("room-2"),
		}}

		sender := &alarmDispatchRunnerBlockingSender{}
		runner := Runner{
			members:        alarmGoldenMembers{},
			consumer:       consumer,
			sender:         sender,
			maxBatch:       10,
			attemptTimeout: 50 * time.Millisecond,
		}

		processed, err := runner.runOnce(t.Context())

		assert.True(t, processed)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, []string{testAlarmRoomID}, sender.rooms, "만료된 attempt로 남은 그룹을 더 보내면 미발송 행이 sending으로 굳는다")
		require.Len(t, consumer.markSending, 1)
		require.Len(t, consumer.scheduledSendingRetry, 1)
		assert.Empty(t, consumer.quarantined)
	})
}

func TestAlarmDispatchRunnerBoundsStateContextWhenParentCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	consumer := &alarmDispatchRunnerContextConsumer{}

	consumer.batches = [][]domain.AlarmQueueEnvelope{{alarmDispatchRunnerPreRenderedTextEnvelope(testAlarmRoomID)}}

	sender := &alarmDispatchRunnerBlockingSender{onSend: cancel}
	runner := Runner{
		members:  alarmGoldenMembers{},
		consumer: consumer,
		sender:   sender,
		maxBatch: 10,
	}

	processed, err := runner.runOnce(ctx)

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.quarantined, 1)
	require.NoError(t, consumer.quarantineCtxErr, "프로세스 종료로 부모가 취소돼도 상태 기록은 완료돼야 한다")
	assert.True(t, consumer.quarantineDeadline, "취소를 끊은 정리 컨텍스트에도 시간 상한이 있어야 한다")
}
