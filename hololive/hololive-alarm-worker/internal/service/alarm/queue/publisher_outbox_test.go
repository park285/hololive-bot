package queue

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type fakeOutboxRepository struct {
	insertBatchCalls int
	lastBatchInput   dispatchoutbox.PublishBatchInput
	batchInputs      []dispatchoutbox.PublishBatchInput
	batchResult      dispatchoutbox.PublishBatchResult
	batchResults     []dispatchoutbox.PublishBatchResult
	batchErrors      []error
	batchErr         error
}

func (r *fakeOutboxRepository) InsertBatch(_ context.Context, input dispatchoutbox.PublishBatchInput) (dispatchoutbox.PublishBatchResult, error) {
	r.insertBatchCalls++

	r.lastBatchInput = input
	r.batchInputs = append(r.batchInputs, input)

	callIndex := r.insertBatchCalls - 1

	result := r.batchResult
	if callIndex < len(r.batchResults) {
		result = r.batchResults[callIndex]
	}

	if callIndex < len(r.batchErrors) && r.batchErrors[callIndex] != nil {
		return result, r.batchErrors[callIndex]
	}

	if r.batchErr != nil {
		return dispatchoutbox.PublishBatchResult{}, r.batchErr
	}

	if result.RequestedDeliveries == 0 {
		result.RequestedDeliveries = len(input.Envelopes)
		result.InsertedDeliveries = len(input.Envelopes)
		result.ProcessedDeliveries = len(input.Envelopes)
		result.RequestedEvents = 1
		result.InsertedEvents = 1
	}

	if result.ProcessedDeliveries == 0 {
		result.ProcessedDeliveries = result.RequestedDeliveries
	}

	if result.Receipts == nil {
		for i := range input.Envelopes {
			result.Receipts = append(result.Receipts, dispatchoutbox.PublishReceipt{Ordinal: i, Outcome: dispatchoutbox.PublishInserted})
		}
	}

	return result, nil
}

func TestPublisherPGFirstTreatsDuplicatesAsSuccess(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	repository := &fakeOutboxRepository{batchResult: dispatchoutbox.PublishBatchResult{
		RequestedEvents:     1,
		ProcessedDeliveries: 1,
		RequestedDeliveries: 1,
		DuplicateDeliveries: 1,
		TerminalDuplicates:  1,
		InsertedDeliveries:  0,
		HashConflictEvents:  0,
	}}
	publisher := newTestPublisher(t, cacheClient, repository, PublishConfig{WakeupEnabled: false, MaxDeliveriesPerBatch: 1000})

	result, err := publisher.PublishBatch(t.Context(), []*domain.AlarmNotification{{
		AlarmType: domain.AlarmTypeLive,
		RoomID:    "room-duplicate",
	}}, [][]string{nil})
	require.NoError(t, err)
	assert.Equal(t, 1, repository.insertBatchCalls)
	assert.Equal(t, 1, result.ProcessedDeliveries)
	assert.Equal(t, 1, result.DuplicateDeliveries)
	assert.Equal(t, 0, result.InsertedDeliveries)
}

func TestPublisherPGFirstChunkFailureReportsProcessedPrefix(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	repository := &fakeOutboxRepository{
		batchErrors: []error{nil, errors.New("pg unavailable")},
	}
	publisher := newTestPublisher(t, cacheClient, repository, PublishConfig{WakeupEnabled: false, MaxDeliveriesPerBatch: 1})

	result, err := publisher.PublishBatch(t.Context(), []*domain.AlarmNotification{
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-1"},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-2"},
	}, make([][]string, 2))
	require.Error(t, err)

	assert.Equal(t, 2, repository.insertBatchCalls)
	assert.Equal(t, 2, result.RequestedDeliveries)
	assert.Equal(t, 1, result.ProcessedDeliveries)
	assert.Equal(t, 1, result.InsertedDeliveries)
}

func TestPublisherPGFirstPublishBatchUsesOneRepositoryBatchAndPayloadFreeWakeup(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repository := &fakeOutboxRepository{}
	publisher := newTestPublisher(t, cacheClient, repository, PublishConfig{WakeupEnabled: true, MaxDeliveriesPerBatch: 1000})
	channel := &domain.Channel{ID: "channel-1"}
	stream := &domain.Stream{ID: "stream-1", ChannelID: "channel-1"}

	_, err := publisher.PublishBatch(t.Context(), []*domain.AlarmNotification{
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-1", Channel: channel, Stream: stream, MinutesUntil: 10},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-2", Channel: channel, Stream: stream, MinutesUntil: 10},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-3", Channel: channel, Stream: stream, MinutesUntil: 10},
	}, [][]string{{"claim:event"}, {"claim:event"}, {"claim:event"}})
	require.NoError(t, err)
	assert.Equal(t, 1, repository.insertBatchCalls)
	assert.Len(t, repository.lastBatchInput.Envelopes, 3)
	assert.Equal(t, []string{"1"}, queueItemsByKeyOrEmpty(t, mini, AlarmDispatchWakeupQueue))
}

func TestPublisherPreservesNonPrefixReceiptsAcrossChunkError(t *testing.T) {
	repository := &fakeOutboxRepository{
		batchResults: []dispatchoutbox.PublishBatchResult{
			{RequestedDeliveries: 2, Receipts: []dispatchoutbox.PublishReceipt{
				{Ordinal: 0, Outcome: dispatchoutbox.PublishRejectedCollision},
				{Ordinal: 1, Outcome: dispatchoutbox.PublishDuplicateActive},
			}},
			{RequestedDeliveries: 2, Receipts: []dispatchoutbox.PublishReceipt{
				{Ordinal: 1, Outcome: dispatchoutbox.PublishDuplicateSent},
			}},
		},
		batchErrors: []error{nil, errors.New("commit response unavailable")},
	}
	publisher := newTestPublisher(t, nil, repository, PublishConfig{WakeupEnabled: false, MaxDeliveriesPerBatch: 2})
	notifications := make([]*domain.AlarmNotification, 4)

	for i := range notifications {
		notifications[i] = &domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "room"}
	}

	result, err := publisher.PublishBatch(t.Context(), notifications, make([][]string, len(notifications)))
	require.ErrorContains(t, err, "commit response unavailable")
	require.Len(t, result.Receipts, 3)
	require.Equal(t, 0, result.Receipts[0].Ordinal)
	require.Equal(t, 1, result.Receipts[1].Ordinal)
	require.Equal(t, 3, result.Receipts[2].Ordinal)
	require.False(t, result.Receipts[0].Accepted())
	require.True(t, result.Receipts[1].Accepted())
	require.True(t, result.Receipts[2].Accepted())
	require.Equal(t, 3, result.ProcessedDeliveries)
}

// claimKeys가 notification과 길이가 다르면 비어 있더라도 claim 없는 envelope으로 채우지 않고 저장 전에 거절한다.
func TestPublisherPublishBatchRejectsClaimKeyCountMismatch(t *testing.T) {
	t.Parallel()

	for _, claimKeys := range [][][]string{nil, {{"claim:room-1"}}} {
		repository := &fakeOutboxRepository{}
		publisher := newTestPublisher(t, nil, repository, PublishConfig{MaxDeliveriesPerBatch: 1000})

		_, err := publisher.PublishBatch(t.Context(), []*domain.AlarmNotification{
			{AlarmType: domain.AlarmTypeLive, RoomID: "room-1"},
			{AlarmType: domain.AlarmTypeLive, RoomID: "room-2"},
		}, claimKeys)
		require.ErrorContains(t, err, "claim key count")
		require.Zero(t, repository.insertBatchCalls)
	}
}

func newTestPublisher(t *testing.T, c WakeupCache, outbox dispatchoutbox.Writer, config PublishConfig) *Publisher {
	t.Helper()

	publisher, err := NewPublisher(c, outbox, config, slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	return publisher
}

func TestNewPublisherRejectsMissingOutboxAndNonPositiveBatchLimit(t *testing.T) {
	t.Parallel()

	_, err := NewPublisher(nil, nil, PublishConfig{MaxDeliveriesPerBatch: 1000}, nil)
	require.ErrorContains(t, err, "outbox repository is required")

	for _, limit := range []int{0, -1} {
		_, err := NewPublisher(nil, &fakeOutboxRepository{}, PublishConfig{MaxDeliveriesPerBatch: limit}, nil)
		require.ErrorContains(t, err, "max deliveries per batch must be positive")
	}
}

// wakeup은 outbox 적재 뒤 부가 작업이므로 호출자 취소와 분리하고, Valkey 지연이 발행 루프를 붙잡지 않게 짧은 상한 안에서 보낸다.
func TestPublisherWakeupIgnoresCallerCancellationWithinShortDeadline(t *testing.T) {
	t.Parallel()

	var (
		guardErr    error
		deadline    time.Time
		hasDeadline bool
	)

	client := cachemocks.NewStrictClient()

	client.SetNXFunc = func(ctx context.Context, _, _ string, _ time.Duration) (bool, error) {
		guardErr = ctx.Err()
		deadline, hasDeadline = ctx.Deadline()

		return false, nil
	}

	publisher := newTestPublisher(t, client, &fakeOutboxRepository{}, PublishConfig{WakeupEnabled: true, MaxDeliveriesPerBatch: 1000})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := publisher.PublishBatch(ctx, []*domain.AlarmNotification{{AlarmType: domain.AlarmTypeLive, RoomID: "room-wakeup"}}, [][]string{nil})
	require.NoError(t, err)
	require.NoError(t, guardErr)
	require.True(t, hasDeadline, "wakeup context has no deadline")
	assert.False(t, deadline.After(time.Now().Add(alarmDispatchWakeupSignalTimeout)), "wakeup deadline %s exceeds %s", deadline, alarmDispatchWakeupSignalTimeout)
}
