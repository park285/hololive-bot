package dispatchoutbox

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestConsumerDrainBatch_QuarantinesStaleSendingBeforeClaiming(t *testing.T) {
	t.Parallel()

	repo := &consumerTestRepository{
		claimDueFunc: func(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
			return nil, nil
		},
	}
	consumer := NewConsumer(repo, slog.Default(), WithWorkerID("worker-1"), WithLease(30*time.Second))

	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() error = %v", err)
	}

	if repo.quarantineStaleSendingCalls != 1 {
		t.Fatalf("QuarantineStaleSending calls = %d, want 1", repo.quarantineStaleSendingCalls)
	}
	if repo.quarantineOlderThan != 30*time.Second {
		t.Fatalf("QuarantineStaleSending olderThan = %v, want 30s", repo.quarantineOlderThan)
	}
}

func TestConsumerDrainBatch_ThrottlesRecovery(t *testing.T) {
	t.Parallel()

	repo := &consumerTestRepository{
		claimDueFunc: func(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
			return nil, nil
		},
	}
	now := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	consumer := NewConsumer(repo, slog.Default(), WithWorkerID("worker-1"), WithRecoveryInterval(30*time.Second))
	consumer.now = func() time.Time { return now }

	for range 100 {
		if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
			t.Fatalf("DrainBatch() error = %v", err)
		}
	}
	if repo.recoverExpiredLeasedCalls != 1 {
		t.Fatalf("RecoverExpiredLeased calls = %d, want 1", repo.recoverExpiredLeasedCalls)
	}
	if repo.quarantineStaleSendingCalls != 1 {
		t.Fatalf("QuarantineStaleSending calls = %d, want 1", repo.quarantineStaleSendingCalls)
	}

	now = now.Add(31 * time.Second)
	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() after interval error = %v", err)
	}
	if repo.recoverExpiredLeasedCalls != 2 {
		t.Fatalf("RecoverExpiredLeased calls = %d, want 2 after interval", repo.recoverExpiredLeasedCalls)
	}
}

func TestConsumerDrainBatch_RecoveryFailureDoesNotBlockClaimAndIsThrottled(t *testing.T) {
	t.Parallel()

	repo := &consumerTestRepository{
		recoverExpiredLeasedFunc: func(context.Context, int) (int, error) {
			return 0, errors.New("postgres unavailable")
		},
	}
	now := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	consumer := NewConsumer(repo, slog.Default(), WithWorkerID("worker-1"), WithRecoveryInterval(30*time.Second))
	consumer.now = func() time.Time { return now }

	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() error = %v, want recovery warning only", err)
	}
	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() second error = %v, want throttled recovery warning only", err)
	}
	if repo.recoverExpiredLeasedCalls != 1 {
		t.Fatalf("RecoverExpiredLeased calls = %d, want 1 after failed recovery throttle", repo.recoverExpiredLeasedCalls)
	}
}

func TestConsumerMarkDispatchedPassesWorkerID(t *testing.T) {
	t.Parallel()

	repo := &consumerTestRepository{}
	consumer := NewConsumer(repo, slog.Default(), WithWorkerID("worker-1"))

	err := consumer.MarkDispatched(context.Background(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 42}})
	if err != nil {
		t.Fatalf("MarkDispatched() error = %v", err)
	}

	if repo.markSentWorkerID != "worker-1" {
		t.Fatalf("MarkSent workerID = %q, want worker-1", repo.markSentWorkerID)
	}
}

func TestConsumerMarkSendingPassesWorkerID(t *testing.T) {
	t.Parallel()

	repo := &consumerTestRepository{}
	consumer := NewConsumer(repo, slog.Default(), WithWorkerID("worker-1"))

	err := consumer.MarkSending(context.Background(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 42}})
	if err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}

	if repo.markSendingWorkerID != "worker-1" {
		t.Fatalf("MarkSending workerID = %q, want worker-1", repo.markSendingWorkerID)
	}
}

func TestConsumerDrainBatchLoadsDistinctEventsAndRehydratesDeliveryContext(t *testing.T) {
	t.Parallel()

	eventPayload := mustMarshalTestEnvelope(t, domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			Channel:      &domain.Channel{ID: "channel-1"},
			Stream:       &domain.Stream{ID: "stream-1", ChannelID: "channel-1"},
			MinutesUntil: 10,
		},
		ClaimKeys:  []string{"event-claim"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    1,
	})
	repo := &consumerTestRepository{
		claimDueFunc: func(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
			return []*Record{
				{ID: 41, EventID: 7, RoomID: "room-1", DeliveryContext: []byte(`{"users":["alice"]}`), ClaimKeys: []string{"claim-1"}},
				{ID: 42, EventID: 7, RoomID: "room-2", DeliveryContext: []byte(`{"users":["bob","charlie"]}`), ClaimKeys: []string{"claim-2"}},
			}, nil
		},
		events: map[int64]EventRecord{
			7: {ID: 7, Payload: eventPayload},
		},
	}
	consumer := NewConsumer(repo, slog.Default(), WithWorkerID("worker-1"))

	envelopes, err := consumer.DrainBatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("DrainBatch() error = %v", err)
	}

	if repo.loadEventsCalls != 1 {
		t.Fatalf("LoadEventsByID calls = %d, want 1", repo.loadEventsCalls)
	}
	if got := repo.loadedEventIDs; len(got) != 1 || got[0] != 7 {
		t.Fatalf("LoadEventsByID ids = %v, want [7]", got)
	}
	if len(envelopes) != 2 {
		t.Fatalf("DrainBatch() envelopes = %d, want 2", len(envelopes))
	}
	if envelopes[0].Notification.RoomID != "room-1" || envelopes[0].Notification.Users[0] != "alice" {
		t.Fatalf("first envelope not rehydrated: %+v", envelopes[0].Notification)
	}
	if envelopes[1].Notification.RoomID != "room-2" || len(envelopes[1].Notification.Users) != 2 {
		t.Fatalf("second envelope not rehydrated: %+v", envelopes[1].Notification)
	}
}

func TestConsumerDrainBatchRestoresAttemptCountForRetryRows(t *testing.T) {
	t.Parallel()

	eventPayload := mustMarshalTestEnvelope(t, domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			Channel:      &domain.Channel{ID: "channel-1"},
			Stream:       &domain.Stream{ID: "stream-1", ChannelID: "channel-1"},
			MinutesUntil: 10,
		},
		Version: 1,
	})
	repo := &consumerTestRepository{
		claimDueFunc: func(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
			return []*Record{
				{ID: 43, EventID: 8, RoomID: "room-1", AttemptCount: 2},
			}, nil
		},
		events: map[int64]EventRecord{
			8: {ID: 8, Payload: eventPayload},
		},
	}
	consumer := NewConsumer(repo, slog.Default(), WithWorkerID("worker-1"))

	envelopes, err := consumer.DrainBatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("DrainBatch() error = %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("DrainBatch() envelopes = %d, want 1", len(envelopes))
	}
	if envelopes[0].Retry == nil {
		t.Fatal("Retry metadata is nil, want attempt restored")
	}
	if envelopes[0].Retry.Attempt != 2 {
		t.Fatalf("Retry attempt = %d, want 2", envelopes[0].Retry.Attempt)
	}
}

func mustMarshalTestEnvelope(t *testing.T, envelope domain.AlarmQueueEnvelope) []byte {
	t.Helper()

	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal test envelope: %v", err)
	}
	return payload
}

type consumerTestRepository struct {
	claimDueFunc                func(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error)
	markSendingFunc             func(ctx context.Context, ids []int64, workerID string, extendLease time.Duration) error
	recoverExpiredLeasedFunc    func(context.Context, int) (int, error)
	events                      map[int64]EventRecord
	loadEventsCalls             int
	loadedEventIDs              []int64
	recoverExpiredLeasedCalls   int
	quarantineStaleSendingCalls int
	quarantineOlderThan         time.Duration
	markSendingWorkerID         string
	markSentWorkerID            string
}

func (r *consumerTestRepository) InsertShadowed(context.Context, domain.AlarmQueueEnvelope) (*Record, error) {
	return nil, nil
}

func (r *consumerTestRepository) InsertPending(context.Context, domain.AlarmQueueEnvelope) (*Record, InsertResult, error) {
	return nil, "", nil
}

func (r *consumerTestRepository) InsertBatch(context.Context, PublishBatchInput) (PublishBatchResult, error) {
	return PublishBatchResult{}, nil
}

func (r *consumerTestRepository) ClaimDue(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
	if r.claimDueFunc != nil {
		return r.claimDueFunc(ctx, workerID, limit, lease)
	}
	return nil, nil
}

func (r *consumerTestRepository) LoadEventsByID(ctx context.Context, eventIDs []int64) (map[int64]EventRecord, error) {
	r.loadEventsCalls++
	r.loadedEventIDs = append([]int64(nil), eventIDs...)
	return r.events, nil
}

func (r *consumerTestRepository) MarkSending(ctx context.Context, ids []int64, workerID string, extendLease time.Duration) error {
	r.markSendingWorkerID = workerID
	if r.markSendingFunc != nil {
		return r.markSendingFunc(ctx, ids, workerID, extendLease)
	}
	return nil
}

func (r *consumerTestRepository) MarkSent(ctx context.Context, ids []int64, workerID string) error {
	r.markSentWorkerID = workerID
	return nil
}

func (r *consumerTestRepository) ScheduleRetry(context.Context, []RetryUpdate, string) error {
	return nil
}

func (r *consumerTestRepository) MoveToDLQ(context.Context, []TerminalUpdate, string) error {
	return nil
}

func (r *consumerTestRepository) Quarantine(context.Context, []TerminalUpdate, string) error {
	return nil
}

func (r *consumerTestRepository) ReleaseLeased(context.Context, []int64) error {
	return nil
}

func (r *consumerTestRepository) RecoverExpiredLeased(ctx context.Context, limit int) (int, error) {
	r.recoverExpiredLeasedCalls++
	if r.recoverExpiredLeasedFunc != nil {
		return r.recoverExpiredLeasedFunc(ctx, limit)
	}
	return 0, nil
}

func (r *consumerTestRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	r.quarantineStaleSendingCalls++
	r.quarantineOlderThan = olderThan
	return 0, nil
}
