package queue

import (
	"context"
	"errors"
	"testing"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type fakeOutboxRepository struct {
	insertPendingCalls int
	insertBatchCalls   int
	lastBatchInput     dispatchoutbox.PublishBatchInput
	batchInputs        []dispatchoutbox.PublishBatchInput
	pendingRecord      *dispatchoutbox.Record
	pendingResult      dispatchoutbox.InsertResult
	batchResult        dispatchoutbox.PublishBatchResult
	batchResults       []dispatchoutbox.PublishBatchResult
	batchErrors        []error
	pendingErr         error
	batchErr           error
}

func (r *fakeOutboxRepository) InsertPending(_ context.Context, _ *domain.AlarmQueueEnvelope) (*dispatchoutbox.Record, dispatchoutbox.InsertResult, error) {
	r.insertPendingCalls++
	if r.pendingErr != nil {
		return nil, "", r.pendingErr
	}

	result := r.pendingResult
	if result == "" {
		result = dispatchoutbox.Inserted
	}

	record := r.pendingRecord
	if record == nil {
		record = &dispatchoutbox.Record{ID: 12, Status: dispatchoutbox.StatusPending}
	}

	return record, result, nil
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

func TestPublisherDoesNotPushLegacyQueue(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repository := &fakeOutboxRepository{}
	publisher := NewPublisher(cacheClient, sharedlogging.NewTestLogger(),
		WithOutbox(repository),
		WithWakeupEnabled(false),
	)

	_, err := publisher.Publish(t.Context(), &domain.AlarmNotification{
		AlarmType: domain.AlarmTypeLive,
		RoomID:    "room-pg-first",
	}, nil)
	require.NoError(t, err)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
	assert.Equal(t, 1, repository.insertBatchCalls)
	assert.Equal(t, 0, repository.insertPendingCalls)
}

func TestPublisherPGFirstTreatsDuplicatesAsSuccess(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repository := &fakeOutboxRepository{batchResult: dispatchoutbox.PublishBatchResult{
		RequestedEvents:     1,
		ProcessedDeliveries: 1,
		RequestedDeliveries: 1,
		DuplicateDeliveries: 1,
		TerminalDuplicates:  1,
		InsertedDeliveries:  0,
		HashConflictEvents:  0,
	}}
	publisher := NewPublisher(cacheClient, sharedlogging.NewTestLogger(),
		WithOutbox(repository),
		WithWakeupEnabled(false),
	)

	result, err := publisher.Publish(t.Context(), &domain.AlarmNotification{
		AlarmType: domain.AlarmTypeLive,
		RoomID:    "room-duplicate",
	}, nil)
	require.NoError(t, err)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
	assert.Equal(t, 1, repository.insertBatchCalls)
	assert.Equal(t, 0, repository.insertPendingCalls)
	assert.Equal(t, 1, result.ProcessedDeliveries)
	assert.Equal(t, 1, result.DuplicateDeliveries)
	assert.Equal(t, 0, result.InsertedDeliveries)
}

func TestPublisherPGFirstChunkFailureReportsProcessedPrefix(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	repository := &fakeOutboxRepository{
		batchErrors: []error{nil, errors.New("pg unavailable")},
	}
	publisher := NewPublisher(cacheClient, sharedlogging.NewTestLogger(),
		WithOutbox(repository),
		WithWakeupEnabled(false),
		WithMaxDeliveriesPerBatch(1),
	)

	result, err := publisher.PublishBatch(t.Context(), []*domain.AlarmNotification{
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-1"},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-2"},
	}, nil)
	require.Error(t, err)

	assert.Equal(t, 2, repository.insertBatchCalls)
	assert.Equal(t, 2, result.RequestedDeliveries)
	assert.Equal(t, 1, result.ProcessedDeliveries)
	assert.Equal(t, 1, result.InsertedDeliveries)
}

func TestPublisherPGFirstPublishBatchUsesOneRepositoryBatchAndPayloadFreeWakeup(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repository := &fakeOutboxRepository{}
	publisher := NewPublisher(cacheClient, sharedlogging.NewTestLogger(),
		WithOutbox(repository),
	)
	channel := &domain.Channel{ID: "channel-1"}
	stream := &domain.Stream{ID: "stream-1", ChannelID: "channel-1"}

	_, err := publisher.PublishBatch(t.Context(), []*domain.AlarmNotification{
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-1", Channel: channel, Stream: stream, MinutesUntil: 10},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-2", Channel: channel, Stream: stream, MinutesUntil: 10},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-3", Channel: channel, Stream: stream, MinutesUntil: 10},
	}, [][]string{{"claim:event"}, {"claim:event"}, {"claim:event"}})
	require.NoError(t, err)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
	assert.Equal(t, 1, repository.insertBatchCalls)
	assert.Equal(t, 0, repository.insertPendingCalls)
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
	publisher := NewPublisher(nil, sharedlogging.NewTestLogger(), WithOutbox(repository), WithMaxDeliveriesPerBatch(2), WithWakeupEnabled(false))
	notifications := make([]*domain.AlarmNotification, 4)

	for i := range notifications {
		notifications[i] = &domain.AlarmNotification{AlarmType: domain.AlarmTypeLive, RoomID: "room"}
	}

	result, err := publisher.PublishBatch(t.Context(), notifications, nil)
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
