package dispatchoutbox

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	json "github.com/park285/shared-go/pkg/json"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestConsumerDrainBatch_QuarantinesStaleSendingBeforeClaiming(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{
		claimDueFunc: func(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
			return nil, nil
		},
	}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"), WithLease(30*time.Second))

	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() error = %v", err)
	}

	if repository.quarantineStaleSendingCalls != 1 {
		t.Fatalf("QuarantineStaleSending calls = %d, want 1", repository.quarantineStaleSendingCalls)
	}
	if repository.quarantineOlderThan != 90*time.Second {
		t.Fatalf("QuarantineStaleSending olderThan = %v, want 90s (3x lease default)", repository.quarantineOlderThan)
	}
}

func TestConsumerQuarantineThresholdSeparatedFromLease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts []ConsumerOption
		want time.Duration
	}{
		{
			name: "default is 3x lease",
			opts: []ConsumerOption{WithLease(45 * time.Second)},
			want: 135 * time.Second,
		},
		{
			name: "explicit threshold overrides default",
			opts: []ConsumerOption{WithLease(60 * time.Second), WithQuarantineThreshold(5 * time.Minute)},
			want: 5 * time.Minute,
		},
		{
			name: "threshold below lease is clamped to lease",
			opts: []ConsumerOption{WithLease(60 * time.Second), WithQuarantineThreshold(10 * time.Second)},
			want: 60 * time.Second,
		},
		{
			name: "option order does not matter",
			opts: []ConsumerOption{WithQuarantineThreshold(4 * time.Minute), WithLease(30 * time.Second)},
			want: 4 * time.Minute,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repository := &consumerTestRepository{}
			opts := append([]ConsumerOption{WithWorkerID("worker-1")}, tc.opts...)
			consumer := NewConsumer(repository, slog.Default(), opts...)

			if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
				t.Fatalf("DrainBatch() error = %v", err)
			}
			if repository.quarantineOlderThan != tc.want {
				t.Fatalf("QuarantineStaleSending olderThan = %v, want %v", repository.quarantineOlderThan, tc.want)
			}
		})
	}
}

func TestConsumerDrainBatch_ThrottlesRecovery(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{
		claimDueFunc: func(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
			return nil, nil
		},
	}
	now := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"), WithRecoveryInterval(30*time.Second))
	consumer.now = func() time.Time { return now }

	for range 100 {
		if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
			t.Fatalf("DrainBatch() error = %v", err)
		}
	}
	if repository.recoverExpiredLeasedCalls != 1 {
		t.Fatalf("RecoverExpiredLeased calls = %d, want 1", repository.recoverExpiredLeasedCalls)
	}
	if repository.quarantineStaleSendingCalls != 1 {
		t.Fatalf("QuarantineStaleSending calls = %d, want 1", repository.quarantineStaleSendingCalls)
	}

	now = now.Add(31 * time.Second)
	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() after interval error = %v", err)
	}
	if repository.recoverExpiredLeasedCalls != 2 {
		t.Fatalf("RecoverExpiredLeased calls = %d, want 2 after interval", repository.recoverExpiredLeasedCalls)
	}
}

func TestConsumerDrainBatch_RecoveryFailureDoesNotBlockClaimAndIsThrottled(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{
		recoverExpiredLeasedFunc: func(context.Context, int) (int, error) {
			return 0, errors.New("postgres unavailable")
		},
	}
	now := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"), WithRecoveryInterval(30*time.Second))
	consumer.now = func() time.Time { return now }

	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() error = %v, want recovery warning only", err)
	}
	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() second error = %v, want throttled recovery warning only", err)
	}
	if repository.recoverExpiredLeasedCalls != 1 {
		t.Fatalf("RecoverExpiredLeased calls = %d, want 1 after failed recovery throttle", repository.recoverExpiredLeasedCalls)
	}
}

func TestConsumerDrainBatch_RecoveryRowsUseLeasedAndSendingMetricLabels(t *testing.T) {
	now := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	repository := &consumerTestRepository{
		recoverExpiredLeasedFunc: func(ctx context.Context, limit int) (int, error) {
			if limit != 7 {
				t.Fatalf("RecoverExpiredLeased limit = %d, want 7", limit)
			}
			return 2, nil
		},
		quarantineStaleSendingFunc: func(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
			if olderThan != 135*time.Second {
				t.Fatalf("QuarantineStaleSending olderThan = %v, want 135s (3x lease default)", olderThan)
			}
			if limit != 7 {
				t.Fatalf("QuarantineStaleSending limit = %d, want 7", limit)
			}
			return 3, nil
		},
	}
	consumer := NewConsumer(repository, slog.Default(),
		WithWorkerID("worker-1"),
		WithLease(45*time.Second),
		WithRecoveryBatchSize(7),
	)
	consumer.now = func() time.Time { return now }

	leasedRowsBefore := testutil.ToFloat64(alarmDispatchRecoveryRowsTotal.WithLabelValues(recoveryTypeLeased))
	sendingRowsBefore := testutil.ToFloat64(alarmDispatchRecoveryRowsTotal.WithLabelValues(recoveryTypeSending))

	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() error = %v", err)
	}

	if repository.recoverExpiredLeasedCalls != 1 {
		t.Fatalf("RecoverExpiredLeased calls = %d, want 1", repository.recoverExpiredLeasedCalls)
	}
	if repository.quarantineStaleSendingCalls != 1 {
		t.Fatalf("QuarantineStaleSending calls = %d, want 1", repository.quarantineStaleSendingCalls)
	}
	if repository.claimDueCalls != 1 {
		t.Fatalf("ClaimDue calls = %d, want 1", repository.claimDueCalls)
	}
	if got := testutil.ToFloat64(alarmDispatchRecoveryRowsTotal.WithLabelValues(recoveryTypeLeased)); got != leasedRowsBefore+2 {
		t.Fatalf("leased recovery rows metric = %v, want %v", got, leasedRowsBefore+2)
	}
	if got := testutil.ToFloat64(alarmDispatchRecoveryRowsTotal.WithLabelValues(recoveryTypeSending)); got != sendingRowsBefore+3 {
		t.Fatalf("sending recovery rows metric = %v, want %v", got, sendingRowsBefore+3)
	}
	if got := testutil.ToFloat64(alarmDispatchRecoveryLastSuccessTimestamp); got != float64(now.Unix()) {
		t.Fatalf("recovery success timestamp = %v, want %v", got, now.Unix())
	}
}

func TestConsumerDrainBatch_SendingRecoveryFailureUsesFailureLabelAndStillClaims(t *testing.T) {
	now := time.Date(2026, 5, 12, 4, 0, 0, 0, time.UTC)
	repository := &consumerTestRepository{
		recoverExpiredLeasedFunc: func(context.Context, int) (int, error) {
			return 2, nil
		},
		quarantineStaleSendingFunc: func(context.Context, time.Duration, int) (int, error) {
			return 0, errors.New("postgres unavailable")
		},
	}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))
	consumer.now = func() time.Time { return now }

	leasedRowsBefore := testutil.ToFloat64(alarmDispatchRecoveryRowsTotal.WithLabelValues(recoveryTypeLeased))
	sendingRowsBefore := testutil.ToFloat64(alarmDispatchRecoveryRowsTotal.WithLabelValues(recoveryTypeSending))
	leasedFailuresBefore := testutil.ToFloat64(alarmDispatchRecoveryFailedTotal.WithLabelValues(recoveryTypeLeased))
	sendingFailuresBefore := testutil.ToFloat64(alarmDispatchRecoveryFailedTotal.WithLabelValues(recoveryTypeSending))
	successTimestampBefore := testutil.ToFloat64(alarmDispatchRecoveryLastSuccessTimestamp)

	if _, err := consumer.DrainBatch(context.Background(), 10); err != nil {
		t.Fatalf("DrainBatch() error = %v, want recovery warning only", err)
	}

	if repository.claimDueCalls != 1 {
		t.Fatalf("ClaimDue calls = %d, want 1", repository.claimDueCalls)
	}
	if got := testutil.ToFloat64(alarmDispatchRecoveryRowsTotal.WithLabelValues(recoveryTypeLeased)); got != leasedRowsBefore+2 {
		t.Fatalf("leased recovery rows metric = %v, want %v", got, leasedRowsBefore+2)
	}
	if got := testutil.ToFloat64(alarmDispatchRecoveryRowsTotal.WithLabelValues(recoveryTypeSending)); got != sendingRowsBefore {
		t.Fatalf("sending recovery rows metric = %v, want unchanged %v", got, sendingRowsBefore)
	}
	if got := testutil.ToFloat64(alarmDispatchRecoveryFailedTotal.WithLabelValues(recoveryTypeLeased)); got != leasedFailuresBefore {
		t.Fatalf("leased recovery failure metric = %v, want unchanged %v", got, leasedFailuresBefore)
	}
	if got := testutil.ToFloat64(alarmDispatchRecoveryFailedTotal.WithLabelValues(recoveryTypeSending)); got != sendingFailuresBefore+1 {
		t.Fatalf("sending recovery failure metric = %v, want %v", got, sendingFailuresBefore+1)
	}
	if got := testutil.ToFloat64(alarmDispatchRecoveryLastSuccessTimestamp); got != successTimestampBefore {
		t.Fatalf("recovery success timestamp = %v, want unchanged %v", got, successTimestampBefore)
	}
}

func TestConsumerMarkDispatchedPassesWorkerID(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

	err := consumer.MarkDispatched(context.Background(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 42}})
	if err != nil {
		t.Fatalf("MarkDispatched() error = %v", err)
	}

	if repository.markSentWorkerID != "worker-1" {
		t.Fatalf("MarkSent workerID = %q, want worker-1", repository.markSentWorkerID)
	}
}

func TestConsumerMarkDispatchedPropagatesPostSendOwnershipChange(t *testing.T) {
	t.Parallel()

	partialErr := &PartialTransitionError{Action: "mark sent", Updated: 0, Expected: 1}
	repository := &consumerTestRepository{
		markSentFunc: func(context.Context, []int64, string) error {
			return partialErr
		},
	}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

	err := consumer.MarkDispatched(context.Background(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 42}})
	if !errors.Is(err, partialErr) {
		t.Fatalf("MarkDispatched() error = %v, want %v", err, partialErr)
	}
}

func TestConsumerMarkSendingPassesWorkerID(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

	err := consumer.MarkSending(context.Background(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 42}})
	if err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}

	if repository.markSendingWorkerID != "worker-1" {
		t.Fatalf("MarkSending workerID = %q, want worker-1", repository.markSendingWorkerID)
	}
}

func TestConsumerDrainBatchLoadsDistinctEventsAndRehydratesDeliveryContext(t *testing.T) {
	t.Parallel()

	eventPayload := mustMarshalTestEnvelope(t, &domain.AlarmQueueEnvelope{
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
	repository := &consumerTestRepository{
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
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

	envelopes, err := consumer.DrainBatch(context.Background(), 10)
	if err != nil {
		t.Fatalf("DrainBatch() error = %v", err)
	}

	if repository.loadEventsCalls != 1 {
		t.Fatalf("LoadEventsByID calls = %d, want 1", repository.loadEventsCalls)
	}
	if got := repository.loadedEventIDs; len(got) != 1 || got[0] != 7 {
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

	eventPayload := mustMarshalTestEnvelope(t, &domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			Channel:      &domain.Channel{ID: "channel-1"},
			Stream:       &domain.Stream{ID: "stream-1", ChannelID: "channel-1"},
			MinutesUntil: 10,
		},
		Version: 1,
	})
	repository := &consumerTestRepository{
		claimDueFunc: func(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
			return []*Record{
				{ID: 43, EventID: 8, RoomID: "room-1", AttemptCount: 2},
			}, nil
		},
		events: map[int64]EventRecord{
			8: {ID: 8, Payload: eventPayload},
		},
	}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

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

func mustMarshalTestEnvelope(t *testing.T, envelope *domain.AlarmQueueEnvelope) []byte {
	t.Helper()

	payload, err := json.Marshal(&envelope)
	if err != nil {
		t.Fatalf("marshal test envelope: %v", err)
	}
	return payload
}

type consumerTestRepository struct {
	claimDueFunc                func(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error)
	markSendingFunc             func(ctx context.Context, ids []int64, workerID string, extendLease time.Duration) error
	markSentFunc                func(ctx context.Context, ids []int64, workerID string) error
	recoverExpiredLeasedFunc    func(context.Context, int) (int, error)
	quarantineStaleSendingFunc  func(context.Context, time.Duration, int) (int, error)
	events                      map[int64]EventRecord
	claimDueCalls               int
	loadEventsCalls             int
	loadedEventIDs              []int64
	recoverExpiredLeasedCalls   int
	quarantineStaleSendingCalls int
	quarantineOlderThan         time.Duration
	markSendingWorkerID         string
	markSentWorkerID            string
}

func (r *consumerTestRepository) InsertPending(context.Context, *domain.AlarmQueueEnvelope) (*Record, InsertResult, error) {
	return nil, "", nil
}

func (r *consumerTestRepository) InsertBatch(context.Context, PublishBatchInput) (PublishBatchResult, error) {
	return PublishBatchResult{}, nil
}

func (r *consumerTestRepository) ClaimDue(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
	r.claimDueCalls++
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
	if r.markSentFunc != nil {
		return r.markSentFunc(ctx, ids, workerID)
	}
	return nil
}

func (r *consumerTestRepository) ScheduleRetry(context.Context, []RetryUpdate, string) error {
	return nil
}

func (r *consumerTestRepository) ScheduleSendingRetry(context.Context, []RetryUpdate, string) error {
	return nil
}

func (r *consumerTestRepository) MoveToDLQ(context.Context, []TerminalUpdate, string) error {
	return nil
}

func (r *consumerTestRepository) Quarantine(context.Context, []TerminalUpdate, string) error {
	return nil
}

func (r *consumerTestRepository) ReleaseLeased(context.Context, []int64, string) error {
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
	if r.quarantineStaleSendingFunc != nil {
		return r.quarantineStaleSendingFunc(ctx, olderThan, limit)
	}
	return 0, nil
}
