package queue

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
)

type fakeOutboxRepository struct {
	insertShadowedCalls int
	insertPendingCalls  int
	insertBatchCalls    int
	lastBatchInput      dispatchoutbox.PublishBatchInput
	shadowedErr         error
	pendingRecord       *dispatchoutbox.Record
	pendingResult       dispatchoutbox.InsertResult
	batchResult         dispatchoutbox.PublishBatchResult
	pendingErr          error
	batchErr            error
}

func (r *fakeOutboxRepository) InsertShadowed(ctx context.Context, envelope domain.AlarmQueueEnvelope) (*dispatchoutbox.Record, error) {
	r.insertShadowedCalls++
	if r.shadowedErr != nil {
		return nil, r.shadowedErr
	}
	return &dispatchoutbox.Record{ID: 11, Status: dispatchoutbox.StatusShadowed}, nil
}

func (r *fakeOutboxRepository) InsertPending(ctx context.Context, envelope domain.AlarmQueueEnvelope) (*dispatchoutbox.Record, dispatchoutbox.InsertResult, error) {
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
	if r.batchErr != nil {
		return dispatchoutbox.PublishBatchResult{}, r.batchErr
	}
	if r.batchResult.RequestedDeliveries == 0 {
		r.batchResult.RequestedDeliveries = len(input.Envelopes)
		r.batchResult.InsertedDeliveries = len(input.Envelopes)
		r.batchResult.RequestedEvents = 1
		r.batchResult.InsertedEvents = 1
	}
	return r.batchResult, nil
}

func TestPublisherShadowModeWritesOutboxAfterValkeySuccess(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repo := &fakeOutboxRepository{}
	publisher := NewPublisher(cacheClient, newTestLogger(),
		WithOutbox(repo),
		WithPublishMode(PublishModeShadow),
	)

	err := publisher.Publish(context.Background(), &domain.AlarmNotification{
		AlarmType: domain.AlarmTypeLive,
		RoomID:    "room-shadow",
	}, []string{"notified:claim:shadow"})
	require.NoError(t, err)

	require.Len(t, queueItemsOrEmpty(t, mini), 1)
	assert.Equal(t, 1, repo.insertBatchCalls)
	assert.Equal(t, dispatchoutbox.StatusShadowed, repo.lastBatchInput.Status)
	assert.Len(t, repo.lastBatchInput.Envelopes, 1)
	assert.Equal(t, 0, repo.insertShadowedCalls)
	assert.Equal(t, 0, repo.insertPendingCalls)
}

func TestPublisherShadowModeHonorsFatalFlag(t *testing.T) {
	cacheClient, _ := newTestCacheClient(t)
	repo := &fakeOutboxRepository{batchErr: errors.New("pg unavailable")}
	publisher := NewPublisher(cacheClient, slog.Default(),
		WithOutbox(repo),
		WithPublishMode(PublishModeShadow),
		WithShadowFatal(true),
	)

	err := publisher.Publish(context.Background(), &domain.AlarmNotification{
		AlarmType: domain.AlarmTypeLive,
		RoomID:    "room-shadow-fatal",
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insert shadowed")
}

func TestPublisherPGFirstDoesNotPushLegacyQueue(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repo := &fakeOutboxRepository{}
	publisher := NewPublisher(cacheClient, newTestLogger(),
		WithOutbox(repo),
		WithPublishMode(PublishModePGFirst),
		WithWakeupEnabled(false),
	)

	err := publisher.Publish(context.Background(), &domain.AlarmNotification{
		AlarmType: domain.AlarmTypeLive,
		RoomID:    "room-pg-first",
	}, nil)
	require.NoError(t, err)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
	assert.Equal(t, 0, repo.insertShadowedCalls)
	assert.Equal(t, 1, repo.insertBatchCalls)
	assert.Equal(t, 0, repo.insertPendingCalls)
}

func TestPublisherPGFirstTreatsDuplicatesAsSuccess(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repo := &fakeOutboxRepository{pendingResult: dispatchoutbox.DuplicateTerminal}
	publisher := NewPublisher(cacheClient, newTestLogger(),
		WithOutbox(repo),
		WithPublishMode(PublishModePGFirst),
		WithWakeupEnabled(false),
	)

	err := publisher.Publish(context.Background(), &domain.AlarmNotification{
		AlarmType: domain.AlarmTypeLive,
		RoomID:    "room-duplicate",
	}, nil)
	require.NoError(t, err)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
	assert.Equal(t, 1, repo.insertBatchCalls)
	assert.Equal(t, 0, repo.insertPendingCalls)
}

func TestPublisherPGFirstPublishBatchUsesOneRepositoryBatchAndPayloadFreeWakeup(t *testing.T) {
	cacheClient, mini := newTestCacheClient(t)
	repo := &fakeOutboxRepository{}
	publisher := NewPublisher(cacheClient, newTestLogger(),
		WithOutbox(repo),
		WithPublishMode(PublishModePGFirst),
	)
	channel := &domain.Channel{ID: "channel-1"}
	stream := &domain.Stream{ID: "stream-1", ChannelID: "channel-1"}

	err := publisher.PublishBatch(context.Background(), []*domain.AlarmNotification{
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-1", Channel: channel, Stream: stream, MinutesUntil: 10},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-2", Channel: channel, Stream: stream, MinutesUntil: 10},
		{AlarmType: domain.AlarmTypeLive, RoomID: "room-3", Channel: channel, Stream: stream, MinutesUntil: 10},
	}, [][]string{{"claim:event"}, {"claim:event"}, {"claim:event"}})
	require.NoError(t, err)

	assert.Empty(t, queueItemsOrEmpty(t, mini))
	assert.Equal(t, 1, repo.insertBatchCalls)
	assert.Equal(t, 0, repo.insertPendingCalls)
	assert.Equal(t, dispatchoutbox.StatusPending, repo.lastBatchInput.Status)
	assert.Len(t, repo.lastBatchInput.Envelopes, 3)
	assert.Equal(t, []string{"1"}, queueItemsByKeyOrEmpty(t, mini, AlarmDispatchWakeupQueue))
}
