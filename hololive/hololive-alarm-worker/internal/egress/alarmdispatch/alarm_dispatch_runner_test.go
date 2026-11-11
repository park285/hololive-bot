package alarmdispatch

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
)

var errAlarmDispatchRunnerTestSend = errors.New("send failed")

type alarmDispatchRunnerTestConsumer struct {
	alarmRequestTestStore

	batches                  [][]dispatchoutbox.AlarmQueueEnvelope
	drainErr                 error
	onDrain                  func()
	markSending              []dispatchoutbox.AlarmQueueEnvelope
	markDispatched           []dispatchoutbox.AlarmQueueEnvelope
	markDispatchedRequestIDs []string
	quarantined              []dispatchoutbox.AlarmQueueEnvelope
	quarantineReason         string
	scheduledRetry           []dispatchoutbox.AlarmQueueEnvelope
	scheduledSendingRetry    []dispatchoutbox.AlarmQueueEnvelope
	preSendRequeued          []dispatchoutbox.AlarmQueueEnvelope
	movedDLQ                 []dispatchoutbox.AlarmQueueEnvelope
	requeued                 []dispatchoutbox.AlarmQueueEnvelope
	released                 []dispatchoutbox.AlarmQueueEnvelope
	releasedClaims           []string
	markSendingErr           error
	markDispatchedErr        error
	quarantineErr            error
	routeFailuresErr         error
}

func (c *alarmDispatchRunnerTestConsumer) DrainBatch(context.Context, int) ([]dispatchoutbox.AlarmQueueEnvelope, error) {
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

// 운영 claim은 send unit이 저장된 delivery만 돌려주고, dispatchoutbox는 dispatch group key마다 send unit과
// client_request_id를 저장한다. 식별자를 직접 지정하지 않은 테스트 봉투에는 생산자와 같은 dispatch group key로 결정적인
// send unit과 ID를 붙여 그 계약을 흉내 낸다.
func withAlarmDispatchTestSendUnitIdentity(batch []dispatchoutbox.AlarmQueueEnvelope) []dispatchoutbox.AlarmQueueEnvelope {
	out := make([]dispatchoutbox.AlarmQueueEnvelope, len(batch))
	for i := range batch {
		out[i] = batch[i]
		if out[i].SendUnitID == 0 && out[i].ClientRequestID == "" {
			sum := sha256.Sum256([]byte(dispatchoutbox.BuildDispatchGroupKeyFromEnvelope(&out[i])))

			out[i].SendUnitID = int64(binary.BigEndian.Uint32(sum[:4])) + 1
			out[i].ClientRequestID = "hololive-alarm:test-" + hex.EncodeToString(sum[:8])
		}
	}

	return out
}

func (c *alarmDispatchRunnerTestConsumer) MarkSending(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error {
	c.markSending = append(c.markSending, envelopes...)
	return c.markSendingErr
}

func (c *alarmDispatchRunnerTestConsumer) MarkDispatched(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, irisRequestID string, _ time.Duration) error {
	c.markDispatched = append(c.markDispatched, envelopes...)
	c.markDispatchedRequestIDs = append(c.markDispatchedRequestIDs, irisRequestID)

	return c.markDispatchedErr
}

func (c *alarmDispatchRunnerTestConsumer) Quarantine(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) error {
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

func (c *alarmDispatchRunnerTestConsumer) ReleaseLeased(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error {
	c.released = append(c.released, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) RouteFailures(_ context.Context, retryEnvelopes, dlqEnvelopes []dispatchoutbox.AlarmQueueEnvelope) error {
	c.scheduledRetry = append(c.scheduledRetry, retryEnvelopes...)
	c.movedDLQ = append(c.movedDLQ, dlqEnvelopes...)

	return c.routeFailuresErr
}

func (c *alarmDispatchRunnerTestConsumer) RouteSendingFailures(_ context.Context, retryEnvelopes, dlqEnvelopes []dispatchoutbox.AlarmQueueEnvelope) error {
	c.scheduledSendingRetry = append(c.scheduledSendingRetry, retryEnvelopes...)
	c.movedDLQ = append(c.movedDLQ, dlqEnvelopes...)

	return nil
}

func (c *alarmDispatchRunnerTestConsumer) RequeuePreSend(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error {
	c.preSendRequeued = append(c.preSendRequeued, envelopes...)
	return nil
}

func (c *alarmDispatchRunnerTestConsumer) Requeue(_ context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error {
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
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	// YouTube 방송 알림도 Text 경로로 저장된 send-unit ID를 붙여 보낸다.
	assert.Equal(t, testAlarmRoomID, sender.roomID)
	require.Len(t, sender.messages, 1)
	require.Len(t, sender.clientRequestIDs, 1)
	assert.Contains(t, sender.clientRequestIDs[0], "hololive-alarm:")
	assert.Len(t, consumer.markSending, 1)
	assert.Len(t, consumer.markDispatched, 1)
	assert.Empty(t, consumer.scheduledRetry)
	assert.Empty(t, consumer.movedDLQ)
}

func TestAlarmDispatchRunnerQuarantinesReplyHandoffOutcomeUnknownWithoutRetry(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.SendUnitID = 7
	envelope.ClientRequestID = testRetryClientRequestID

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{
		messageErr: errors.Join(sendoutcome.ErrAdmissionOutcomeUnknown, context.DeadlineExceeded),
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
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
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
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
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
		batches:       [][]dispatchoutbox.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}},
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

	envelope.SourceKind = domain.AlarmDispatchSourceKindDeliveryDigest
	envelope.DeliveryDigest = nil

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{envelope}}}
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

func TestAlarmDispatchRunnerRoutesRetiredSourceKindBeforeMarkSending(t *testing.T) {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.SourceKind = domain.AlarmDispatchSourceKind("youtube_outbox")

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledRetry, 1)
	require.NotNil(t, consumer.scheduledRetry[0].Retry)
	assert.Contains(t, consumer.scheduledRetry[0].Retry.LastError, `source kind "youtube_outbox" has no egress path`)
	assert.Empty(t, consumer.preSendRequeued)
	assert.Empty(t, consumer.movedDLQ)
	assert.Empty(t, consumer.markSending)
	assert.Empty(t, consumer.quarantined)
	assert.Empty(t, sender.messages)
}

func TestAlarmDispatchRunnerDoesNotRetryMarkDispatchedFailureAfterSend(t *testing.T) {
	markErr := &sendfence.MissError{
		Action: "mark_sent", Expected: 1,
		Rows: []sendfence.RowMiss{{ID: 1, Reason: sendfence.ReasonStatusChanged, Status: string(dispatchoutbox.StatusQuarantined)}},
	}
	consumer := &alarmDispatchRunnerTestConsumer{
		batches:           [][]dispatchoutbox.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}},
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
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &dispatchoutbox.AlarmQueueRetryMetadata{Attempt: alarmDispatchRetryableMaxAttempts - 1})

	envelope.ClaimKeys = []string{testAlarmClaimKey}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{envelope}}}
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
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &dispatchoutbox.AlarmQueueRetryMetadata{Attempt: alarmDispatchMaxAttempts - 1})

	envelope.ClaimKeys = []string{testAlarmClaimKey}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{envelope}}}
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

func TestAlarmDispatchRunnerRetriesPersistedStreamGroupDeadline(t *testing.T) {
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)
	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	second.DispatchOutboxID = 2

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{first, second}}}
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
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{envelope}}}
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
			consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{envelope}}}
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

func TestNewRunnerRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	consumer := &alarmDispatchRunnerTestConsumer{}
	sender := &alarmDispatchRunnerTestSender{}
	waiter := &alarmDispatchRunnerTestIdleWaiter{}

	for _, tc := range []struct {
		name       string
		consumer   Consumer
		sender     delivery.PreparedMessageSender
		idleWaiter IdleWaiter
	}{
		{name: "consumer", sender: sender, idleWaiter: waiter},
		{name: "sender", consumer: consumer, idleWaiter: waiter},
		{name: "idle waiter", consumer: consumer, sender: sender},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runner, err := NewRunner(tc.consumer, tc.sender, nil, nil, tc.idleWaiter, RunnerConfig{}, nil)

			require.Error(t, err)
			assert.Nil(t, runner)
		})
	}

	runner, err := NewRunner(consumer, sender, nil, nil, waiter, RunnerConfig{}, nil)

	require.NoError(t, err)
	assert.NotNil(t, runner)
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
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}}}
	waiter := &alarmDispatchRunnerTestIdleWaiter{returnValue: true}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: &alarmDispatchRunnerTestSender{}, renderer: newAlarmDispatchTestRenderer(t), maxBatch: 10, idleWaiter: waiter}

	keepGoing := runner.runStep(t.Context())

	assert.True(t, keepGoing)
	assert.Zero(t, waiter.waits)
	assert.Equal(t, 1, waiter.resets)
}

func TestAlarmDispatchRunnerYieldsAfterMaxBatchesPerWake(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{
		{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)},
		{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)},
	}}
	yieldCount := 0
	runner := Runner{
		members:           alarmGoldenMembers{},
		consumer:          consumer,
		sender:            &alarmDispatchRunnerTestSender{},
		renderer:          newAlarmDispatchTestRenderer(t),
		idleWaiter:        &alarmDispatchRunnerTestIdleWaiter{},
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
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{
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

func TestAlarmGroupViewPreservesPerItemCountdown(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	first := alarmGroupRenderEnvelope(3, "Member1", "abc", "Title1")
	second := alarmGroupRenderEnvelope(1, "Member2", "def", "Title2")

	first.SendUnitID = 7
	first.Notification.Stream.StartScheduled = &start
	second.SendUnitID = first.SendUnitID
	second.Notification.Stream.StartScheduled = &start

	group := groupAlarmDispatchEnvelopesForDelivery([]dispatchoutbox.AlarmQueueEnvelope{first, second})[0]

	view, err := buildAlarmDispatchGroupViewWithShortLinks(t.Context(), nil, &alarmDispatchMembers{provider: alarmGoldenMembers{}}, group, shortlinkservice.YouTubeBuilder{})
	require.NoError(t, err)
	require.Len(t, view.Entries, 2)
	assert.Equal(t, 1, view.MinutesUntil)
	assert.Equal(t, 3, view.Entries[0].MinutesUntil)
	assert.False(t, view.Entries[0].IsScheduled)
	assert.True(t, view.Entries[1].IsScheduled)
}

func TestAlarmGroupViewAllLiveCatchupIsStarting(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	first := alarmGroupRenderEnvelope(5, "Member1", "abc", "Title1")
	second := alarmGroupRenderEnvelope(5, "Member2", "def", "Title2")

	first.Notification.Stream.StartActual = &start
	second.Notification.Stream.StartActual = &start

	group := alarmDispatchGroup{
		roomID:        testAlarmRoomID,
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{first.Notification, second.Notification},
	}

	view, err := buildAlarmDispatchGroupViewWithShortLinks(t.Context(), nil, &alarmDispatchMembers{provider: alarmGoldenMembers{}}, group, shortlinkservice.YouTubeBuilder{})
	require.NoError(t, err)
	require.Len(t, view.Entries, 2)
	assert.True(t, view.IsStarting)
	assert.True(t, view.Entries[0].IsStarting)
	assert.True(t, view.Entries[1].IsStarting)
}

func TestAlarmGroupViewMixedCatchupKeepsPerItemState(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	first := alarmGroupRenderEnvelope(5, "LiveMember", "live", "Live Title")
	second := alarmGroupRenderEnvelope(5, "UpcomingMember", "upcoming", "Upcoming Title")

	first.Notification.Stream.StartActual = &start
	second.Notification.Stream.StartScheduled = &start

	group := alarmDispatchGroup{
		roomID:        testAlarmRoomID,
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{first.Notification, second.Notification},
	}

	view, err := buildAlarmDispatchGroupViewWithShortLinks(t.Context(), nil, &alarmDispatchMembers{provider: alarmGoldenMembers{}}, group, shortlinkservice.YouTubeBuilder{})
	require.NoError(t, err)
	require.Len(t, view.Entries, 2)
	assert.False(t, view.IsStarting)
	assert.True(t, view.Entries[0].IsStarting)
	assert.False(t, view.Entries[1].IsStarting)
	assert.True(t, view.Entries[1].IsScheduled)
}

func TestAlarmItemViewActualStartOverridesCountdown(t *testing.T) {
	start := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = testAlarmMemberName
	notification.Stream.ChannelID = alarmGoldenChannelPrefix + testAlarmMemberName
	notification.Stream.ID = "live-1"
	notification.Stream.Title = "Live Title"
	notification.Stream.StartScheduled = &start
	notification.Stream.StartActual = &start

	view, err := buildAlarmDispatchItemView(t.Context(), nil, &alarmDispatchMembers{provider: alarmGoldenMembers{}}, &notification, -1)
	require.NoError(t, err)
	assert.True(t, view.IsStarting)
}

func TestAlarmItemViewLiveStatusOverridesCountdown(t *testing.T) {
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = testAlarmMemberName
	notification.Stream.ChannelID = alarmGoldenChannelPrefix + testAlarmMemberName
	notification.Stream.ID = "live-status-1"
	notification.Stream.Title = "Live Title"
	notification.Stream.Status = domain.StreamStatusLive

	view, err := buildAlarmDispatchItemView(t.Context(), nil, &alarmDispatchMembers{provider: alarmGoldenMembers{}}, &notification, -1)
	require.NoError(t, err)
	assert.True(t, view.IsStarting)
}

func TestAlarmItemViewUpcomingKeepsCountdown(t *testing.T) {
	notification := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	notification.MinutesUntil = 5
	notification.Channel.Name = testAlarmMemberName
	notification.Stream.ChannelID = alarmGoldenChannelPrefix + testAlarmMemberName
	notification.Stream.ID = "upcoming-1"
	notification.Stream.Title = "Upcoming Title"
	notification.Stream.Status = domain.StreamStatusUpcoming

	view, err := buildAlarmDispatchItemView(t.Context(), nil, &alarmDispatchMembers{provider: alarmGoldenMembers{}}, &notification, -1)
	require.NoError(t, err)
	assert.False(t, view.IsStarting)
	assert.Equal(t, 5, view.MinutesUntil)
}

func TestRenderAlarmDispatchNotificationSeparatesLongTitleAndURL(t *testing.T) {
	const (
		title = "【ホロライブ ドリームス】水着きちゃ!音ゲー初心者!hololive Dreamsやってみる!【#" + kakaotext.ZeroWidthSpace +
			"綺々羅々ヴィヴィ #" + kakaotext.ZeroWidthSpace + "hololiveDEV_" + kakaotext.ZeroWidthSpace +
			"IS #" + kakaotext.ZeroWidthSpace + "FLOWGLOW】"
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

	variants, err := renderAlarmDispatchNotification(t.Context(), newAlarmDispatchTestRenderer(t), nil, &alarmDispatchMembers{provider: alarmGoldenMembers{}}, &notification)

	require.NoError(t, err)

	for _, got := range messageTexts(variants) {
		visible := strings.ReplaceAll(kakaoformat.Render(got), kakaotext.ZeroWidthSpace, "")
		assert.Contains(t, "\n"+visible+"\n", "\n"+streamURL+"\n")
		assert.Contains(t, visible, "【ホロライブ ドリームス】")
		assert.NotContains(t, visible, strings.ReplaceAll(title, kakaotext.ZeroWidthSpace, ""))
	}
}

func alarmDispatchRunnerTestEnvelope(roomID string, retry *dispatchoutbox.AlarmQueueRetryMetadata) dispatchoutbox.AlarmQueueEnvelope {
	return dispatchoutbox.AlarmQueueEnvelope{
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

func (c *alarmDispatchRunnerContextConsumer) MarkSending(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error {
	c.markSendingCtxErr = ctx.Err()
	if c.markSendingCtxErr != nil {
		return fmt.Errorf("mark alarm dispatch sending: %w", c.markSendingCtxErr)
	}

	if err := c.alarmDispatchRunnerTestConsumer.MarkSending(ctx, envelopes); err != nil {
		return fmt.Errorf("mark sending: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) MarkDispatched(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, irisRequestID string, handoffHorizon time.Duration) error {
	c.markDispatchedCtxErr = ctx.Err()
	if c.markDispatchedCtxErr != nil {
		return fmt.Errorf("mark alarm dispatch sent: %w", c.markDispatchedCtxErr)
	}

	if err := c.alarmDispatchRunnerTestConsumer.MarkDispatched(ctx, envelopes, irisRequestID, handoffHorizon); err != nil {
		return fmt.Errorf("mark dispatched: %w", err)
	}

	return nil
}

func (c *alarmDispatchRunnerContextConsumer) RouteSendingFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []dispatchoutbox.AlarmQueueEnvelope) error {
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

func (c *alarmDispatchRunnerContextConsumer) Quarantine(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope, cause error) error {
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

func (c *alarmDispatchRunnerContextConsumer) RequeuePreSend(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) error {
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

func alarmDispatchRunnerIntrinsicTextEnvelope() dispatchoutbox.AlarmQueueEnvelope {
	return alarmDispatchRunnerPreRenderedTextEnvelope(testAlarmRoomID)
}

// attempt deadline 검증은 렌더러의 실제 DB I/O와 분리한다.
func alarmDispatchRunnerPreRenderedTextEnvelope(roomID string) dispatchoutbox.AlarmQueueEnvelope {
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

		consumer.batches = [][]dispatchoutbox.AlarmQueueEnvelope{{alarmDispatchRunnerPreRenderedTextEnvelope(testAlarmRoomID)}}

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

		consumer.batches = [][]dispatchoutbox.AlarmQueueEnvelope{{alarmDispatchRunnerPreRenderedTextEnvelope(testAlarmRoomID)}}

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

func TestAlarmDispatchRunnerGivesEachGroupIndependentAttemptDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		consumer := &alarmDispatchRunnerContextConsumer{}

		consumer.batches = [][]dispatchoutbox.AlarmQueueEnvelope{{
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

		started := time.Now()
		processed, err := runner.runOnce(t.Context())

		assert.True(t, processed)
		require.NoError(t, err)
		assert.Equal(t, []string{testAlarmRoomID, "room-2"}, sender.rooms)
		require.Equal(t, 100*time.Millisecond, time.Since(started))
		require.Len(t, consumer.markSending, 2)
		require.Len(t, consumer.scheduledSendingRetry, 2)
		assert.Empty(t, consumer.quarantined)
	})
}

func TestAlarmDispatchRunnerBoundsStateContextWhenParentCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	consumer := &alarmDispatchRunnerContextConsumer{}

	consumer.batches = [][]dispatchoutbox.AlarmQueueEnvelope{{alarmDispatchRunnerPreRenderedTextEnvelope(testAlarmRoomID)}}

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

// alarmGroupRenderEnvelope는 members 정본 이름이 memberName인 채널의 방송 알림 envelope를 만든다.
func alarmGroupRenderEnvelope(minutesUntil int, memberName, streamID, title string) dispatchoutbox.AlarmQueueEnvelope {
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.MinutesUntil = minutesUntil
	envelope.Notification.Channel.Name = memberName
	envelope.Notification.Stream.ChannelID = alarmGoldenChannelPrefix + memberName
	envelope.Notification.Stream.ID = streamID
	envelope.Notification.Stream.Title = title

	return envelope
}

func TestAlarmDispatchRunnerRoutesRoomMismatchedGroupAndSendsRemainingGroups(t *testing.T) {
	const otherRoomID = "9876543210"

	mismatched := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	mismatched.ClientRequestID = "hololive-alarm:test-room-mismatch"

	healthy := alarmDispatchRunnerTestEnvelope(otherRoomID, nil)
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{mismatched, healthy}}}

	consumer.requests = map[string]*dispatchoutbox.SendRequest{
		mismatched.ClientRequestID: {
			ClientRequestID:     mismatched.ClientRequestID,
			BaseClientRequestID: mismatched.ClientRequestID,
			Body:                "stored body",
			Route:               dispatchoutbox.SendRouteText,
			RoomID:              otherRoomID,
		},
	}

	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, consumer.scheduledRetry, 1)
	assert.Equal(t, testAlarmRoomID, consumer.scheduledRetry[0].Notification.RoomID)
	require.Len(t, sender.messages, 1)
	assert.Equal(t, otherRoomID, sender.roomID)
	require.Len(t, consumer.markDispatched, 1)
	assert.Equal(t, otherRoomID, consumer.markDispatched[0].Notification.RoomID)
}
