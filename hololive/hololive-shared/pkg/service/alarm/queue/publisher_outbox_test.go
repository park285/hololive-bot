package queue

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
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

func (r *fakeOutboxRepository) InsertPending(ctx context.Context, envelope *domain.AlarmQueueEnvelope) (*dispatchoutbox.Record, dispatchoutbox.InsertResult, error) {
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

func (r *fakeOutboxRepository) InsertBatch(ctx context.Context, input dispatchoutbox.PublishBatchInput) (dispatchoutbox.PublishBatchResult, error) {
	r.insertBatchCalls++
	r.lastBatchInput = input
	r.batchInputs = append(r.batchInputs, input)
	callIndex := r.insertBatchCalls - 1
	if callIndex < len(r.batchErrors) && r.batchErrors[callIndex] != nil {
		return dispatchoutbox.PublishBatchResult{}, r.batchErrors[callIndex]
	}
	if r.batchErr != nil {
		return dispatchoutbox.PublishBatchResult{}, r.batchErr
	}
	result := r.batchResult
	if callIndex < len(r.batchResults) {
		result = r.batchResults[callIndex]
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
	return result, nil
}

func TestPublisherDoesNotPushLegacyQueue(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repository := &fakeOutboxRepository{}
	publisher := NewPublisher(cacheClient, sharedlogging.NewTestLogger(),
		WithOutbox(repository),
		WithWakeupEnabled(false),
	)

	_, err := publisher.Publish(context.Background(), &domain.AlarmNotification{
		AlarmType: domain.AlarmTypeLive,
		RoomID:    "room-pg-first",
	}, nil)
	require.NoError(t, err)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
	assert.Equal(t, 1, repository.insertBatchCalls)
	assert.Equal(t, dispatchoutbox.StatusPending, repository.lastBatchInput.Status)
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

	result, err := publisher.Publish(context.Background(), &domain.AlarmNotification{
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

	result, err := publisher.PublishBatch(context.Background(), []*domain.AlarmNotification{
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

	_, err := publisher.PublishBatch(context.Background(), []*domain.AlarmNotification{
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-1", Channel: channel, Stream: stream, MinutesUntil: 10},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-2", Channel: channel, Stream: stream, MinutesUntil: 10},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-3", Channel: channel, Stream: stream, MinutesUntil: 10},
	}, [][]string{{"claim:event"}, {"claim:event"}, {"claim:event"}})
	require.NoError(t, err)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
	assert.Equal(t, 1, repository.insertBatchCalls)
	assert.Equal(t, 0, repository.insertPendingCalls)
	assert.Equal(t, dispatchoutbox.StatusPending, repository.lastBatchInput.Status)
	assert.Len(t, repository.lastBatchInput.Envelopes, 3)
	assert.Equal(t, []string{"1"}, queueItemsByKeyOrEmpty(t, mini, AlarmDispatchWakeupQueue))
}
