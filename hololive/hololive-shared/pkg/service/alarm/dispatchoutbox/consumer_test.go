package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestConsumerDrainBatch_QuarantinesStaleSendingBeforeClaiming(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{
		claimDueFunc: func(_ context.Context, _ string, _ int, _ time.Duration) ([]*Record, error) {
			return nil, nil
		},
	}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"), WithLease(30*time.Second))

	if _, err := consumer.DrainBatch(t.Context(), 10); err != nil {
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

			if _, err := consumer.DrainBatch(t.Context(), 10); err != nil {
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
		claimDueFunc: func(_ context.Context, _ string, _ int, _ time.Duration) ([]*Record, error) {
			return nil, nil
		},
	}
	now := time.Date(2026, time.May, 12, 3, 0, 0, 0, time.UTC)
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"), WithRecoveryInterval(30*time.Second))

	consumer.now = func() time.Time { return now }

	for range 100 {
		if _, err := consumer.DrainBatch(t.Context(), 10); err != nil {
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

	if _, err := consumer.DrainBatch(t.Context(), 10); err != nil {
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
	now := time.Date(2026, time.May, 12, 3, 0, 0, 0, time.UTC)
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"), WithRecoveryInterval(30*time.Second))

	consumer.now = func() time.Time { return now }

	if _, err := consumer.DrainBatch(t.Context(), 10); err != nil {
		t.Fatalf("DrainBatch() error = %v, want recovery warning only", err)
	}

	if _, err := consumer.DrainBatch(t.Context(), 10); err != nil {
		t.Fatalf("DrainBatch() second error = %v, want throttled recovery warning only", err)
	}

	if repository.recoverExpiredLeasedCalls != 1 {
		t.Fatalf("RecoverExpiredLeased calls = %d, want 1 after failed recovery throttle", repository.recoverExpiredLeasedCalls)
	}
}

func TestConsumerDrainBatch_RecoveryRowsUseLeasedAndSendingMetricLabels(t *testing.T) {
	now := time.Date(2026, time.May, 12, 3, 0, 0, 0, time.UTC)
	repository := &consumerTestRepository{
		recoverExpiredLeasedFunc: func(_ context.Context, limit int) (int, error) {
			if limit != 7 {
				t.Fatalf("RecoverExpiredLeased limit = %d, want 7", limit)
			}

			return 2, nil
		},
		quarantineStaleSendingFunc: func(_ context.Context, olderThan time.Duration, limit int) (int, error) {
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

	if _, err := consumer.DrainBatch(t.Context(), 10); err != nil {
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
	now := time.Date(2026, time.May, 12, 4, 0, 0, 0, time.UTC)
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

	if _, err := consumer.DrainBatch(t.Context(), 10); err != nil {
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

	err := consumer.MarkDispatched(t.Context(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 42}})
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

	err := consumer.MarkDispatched(t.Context(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 42}})
	if !errors.Is(err, partialErr) {
		t.Fatalf("MarkDispatched() error = %v, want %v", err, partialErr)
	}
}

func TestConsumerMarkSendingPassesWorkerID(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

	err := consumer.MarkSending(t.Context(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 42}})
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
			Channel:      &domain.Channel{ID: testChannelID},
			Stream:       &domain.Stream{ID: testStreamID, ChannelID: testChannelID},
			MinutesUntil: 10,
		},
		ClaimKeys:  []string{"event-claim"},
		EnqueuedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    1,
	})
	repository := &consumerTestRepository{
		claimDueFunc: func(_ context.Context, _ string, _ int, _ time.Duration) ([]*Record, error) {
			return []*Record{
				{ID: 41, EventID: 7, RoomID: testRoomID, DeliveryContext: []byte(`{"users":["alice"]}`), ClaimKeys: []string{"claim-1"}},
				{ID: 42, EventID: 7, RoomID: testOtherRoomID, DeliveryContext: []byte(`{"users":["bob","charlie"]}`), ClaimKeys: []string{"claim-2"}},
			}, nil
		},
		events: map[int64]EventRecord{
			7: {ID: 7, Payload: eventPayload},
		},
	}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

	envelopes, err := consumer.DrainBatch(t.Context(), 10)
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

	if envelopes[0].Notification.RoomID != testRoomID || envelopes[0].Notification.Users[0] != testUserName {
		t.Fatalf("first envelope not rehydrated: %+v", envelopes[0].Notification)
	}

	if envelopes[1].Notification.RoomID != testOtherRoomID || len(envelopes[1].Notification.Users) != 2 {
		t.Fatalf("second envelope not rehydrated: %+v", envelopes[1].Notification)
	}
}

func TestConsumerDrainBatchRestoresAttemptCountForRetryRows(t *testing.T) {
	t.Parallel()

	eventPayload := mustMarshalTestEnvelope(t, &domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType:    domain.AlarmTypeLive,
			Channel:      &domain.Channel{ID: testChannelID},
			Stream:       &domain.Stream{ID: testStreamID, ChannelID: testChannelID},
			MinutesUntil: 10,
		},
		Version: 1,
	})
	repository := &consumerTestRepository{
		claimDueFunc: func(_ context.Context, _ string, _ int, _ time.Duration) ([]*Record, error) {
			return []*Record{
				{ID: 43, EventID: 8, RoomID: testRoomID, AttemptCount: 2},
			}, nil
		},
		events: map[int64]EventRecord{
			8: {ID: 8, Payload: eventPayload},
		},
	}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

	envelopes, err := consumer.DrainBatch(t.Context(), 10)
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

	payload, err := jsonv2.Marshal(&envelope)
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
	routeFailuresFunc           func(context.Context, []FailureUpdate, string) error
	routeSendingFailuresFunc    func(context.Context, []FailureUpdate, string) error
	requeuePreSendFunc          func(context.Context, []FailureUpdate, string) error
	routedFailureUpdates        []FailureUpdate
	quarantineUpdates           []TerminalUpdate
	movedDLQUpdates             []TerminalUpdate
	routeFailuresCalls          int
	routeSendingFailuresCalls   int
	requeuePreSendCalls         int
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
	var missing *Record

	return missing, "", nil
}

func (r *consumerTestRepository) InsertBatch(context.Context, PublishBatchInput) (PublishBatchResult, error) {
	return PublishBatchResult{}, nil
}

func (r *consumerTestRepository) ClaimDue(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error) {
	r.claimDueCalls++
	if r.claimDueFunc != nil {
		out, err := r.claimDueFunc(ctx, workerID, limit, lease)
		if err != nil {
			return out, fmt.Errorf("claim due func: %w", err)
		}

		return out, nil
	}

	return nil, nil
}

func (r *consumerTestRepository) LoadEventsByID(_ context.Context, eventIDs []int64) (map[int64]EventRecord, error) {
	r.loadEventsCalls++

	r.loadedEventIDs = append([]int64(nil), eventIDs...)

	return r.events, nil
}

func (r *consumerTestRepository) MarkSending(ctx context.Context, ids []int64, workerID string, extendLease time.Duration) error {
	r.markSendingWorkerID = workerID
	if r.markSendingFunc != nil {
		if err := r.markSendingFunc(ctx, ids, workerID, extendLease); err != nil {
			return fmt.Errorf("mark sending func: %w", err)
		}

		return nil
	}

	return nil
}

func (r *consumerTestRepository) MarkSent(ctx context.Context, ids []int64, workerID string) error {
	r.markSentWorkerID = workerID
	if r.markSentFunc != nil {
		if err := r.markSentFunc(ctx, ids, workerID); err != nil {
			return fmt.Errorf("mark sent func: %w", err)
		}

		return nil
	}

	return nil
}

func (r *consumerTestRepository) RouteFailures(ctx context.Context, updates []FailureUpdate, workerID string) error {
	r.routeFailuresCalls++

	r.routedFailureUpdates = append([]FailureUpdate(nil), updates...)

	if r.routeFailuresFunc != nil {
		if err := r.routeFailuresFunc(ctx, updates, workerID); err != nil {
			return fmt.Errorf("route failures func: %w", err)
		}

		return nil
	}

	return nil
}

func (r *consumerTestRepository) RouteSendingFailures(ctx context.Context, updates []FailureUpdate, workerID string) error {
	r.routeSendingFailuresCalls++

	r.routedFailureUpdates = append([]FailureUpdate(nil), updates...)

	if r.routeSendingFailuresFunc != nil {
		if err := r.routeSendingFailuresFunc(ctx, updates, workerID); err != nil {
			return fmt.Errorf("route sending failures func: %w", err)
		}

		return nil
	}

	return nil
}

func (r *consumerTestRepository) RequeuePreSend(ctx context.Context, updates []FailureUpdate, workerID string) error {
	r.requeuePreSendCalls++

	r.routedFailureUpdates = append([]FailureUpdate(nil), updates...)

	if r.requeuePreSendFunc != nil {
		if err := r.requeuePreSendFunc(ctx, updates, workerID); err != nil {
			return fmt.Errorf("requeue pre send func: %w", err)
		}

		return nil
	}

	return nil
}

func (r *consumerTestRepository) MoveToDLQ(_ context.Context, updates []TerminalUpdate, _ string) error {
	r.movedDLQUpdates = append(r.movedDLQUpdates, updates...)
	return nil
}

func (r *consumerTestRepository) Quarantine(_ context.Context, updates []TerminalUpdate, _ string) error {
	r.quarantineUpdates = append(r.quarantineUpdates, updates...)
	return nil
}

func (r *consumerTestRepository) ReleaseLeased(context.Context, []int64, string) error {
	return nil
}

func (r *consumerTestRepository) RecoverExpiredLeased(ctx context.Context, limit int) (int, error) {
	r.recoverExpiredLeasedCalls++
	if r.recoverExpiredLeasedFunc != nil {
		out, err := r.recoverExpiredLeasedFunc(ctx, limit)
		if err != nil {
			return out, fmt.Errorf("recover expired leased func: %w", err)
		}

		return out, nil
	}

	return 0, nil
}

func (r *consumerTestRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	r.quarantineStaleSendingCalls++

	r.quarantineOlderThan = olderThan

	if r.quarantineStaleSendingFunc != nil {
		out, err := r.quarantineStaleSendingFunc(ctx, olderThan, limit)
		if err != nil {
			return out, fmt.Errorf("quarantine stale sending func: %w", err)
		}

		return out, nil
	}

	return 0, nil
}

func TestConsumerRouteFailuresConvertsEnvelopesToTargetedUpdates(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))
	nextVisible := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	retryEnvelope := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 11,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			LastError:     "send failed",
			NextVisibleAt: nextVisible.Format(time.RFC3339Nano),
		},
	}
	dlqEnvelope := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 22,
		Retry:            &domain.AlarmQueueRetryMetadata{Attempt: 3, LastError: "exhausted"},
	}
	skipped := domain.AlarmQueueEnvelope{DispatchOutboxID: 0}

	if err := consumer.RouteFailures(t.Context(), []domain.AlarmQueueEnvelope{retryEnvelope, skipped}, []domain.AlarmQueueEnvelope{dlqEnvelope}); err != nil {
		t.Fatalf("RouteFailures() error = %v", err)
	}

	if repository.routeFailuresCalls != 1 || repository.routeSendingFailuresCalls != 0 {
		t.Fatalf("route calls = %d/%d, want 1 RouteFailures only", repository.routeFailuresCalls, repository.routeSendingFailuresCalls)
	}

	updates := repository.routedFailureUpdates
	if len(updates) != 2 {
		t.Fatalf("updates = %d, want 2 (zero-id envelope skipped)", len(updates))
	}

	if updates[0].ID != 11 || updates[0].TargetStatus != StatusRetry || updates[0].AttemptCount != 1 {
		t.Fatalf("retry update = %+v, want id=11 target=retry attempt=1", updates[0])
	}

	if !updates[0].NextAttemptAt.Equal(nextVisible) {
		t.Fatalf("retry NextAttemptAt = %v, want %v", updates[0].NextAttemptAt, nextVisible)
	}

	if updates[1].ID != 22 || updates[1].TargetStatus != StatusDLQ || updates[1].AttemptCount != 3 {
		t.Fatalf("dlq update = %+v, want id=22 target=dlq attempt=3", updates[1])
	}

	if updates[1].Error != "exhausted" {
		t.Fatalf("dlq update error = %q, want %q", updates[1].Error, "exhausted")
	}
}

func TestConsumerRouteFailuresPartialObservesAppliedSubsets(t *testing.T) {
	repository := &consumerTestRepository{}

	repository.routeFailuresFunc = func(context.Context, []FailureUpdate, string) error {
		return &PartialTransitionError{
			Action:       "route dispatch delivery failures",
			Updated:      2,
			Expected:     3,
			UnappliedIDs: []int64{22},
		}
	}

	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))
	nextVisible := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	retryApplied := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 11,
		Retry:            &domain.AlarmQueueRetryMetadata{Attempt: 1, LastError: "boom", NextVisibleAt: nextVisible},
	}
	retryUnapplied := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 22,
		Retry:            &domain.AlarmQueueRetryMetadata{Attempt: 1, LastError: "boom", NextVisibleAt: nextVisible},
	}
	dlqApplied := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 33,
		Retry:            &domain.AlarmQueueRetryMetadata{Attempt: 3, LastError: "exhausted"},
	}

	retryBefore := testutil.ToFloat64(alarmDispatchPGRetryScheduledTotal)
	dlqBefore := testutil.ToFloat64(alarmDispatchPGDLQTotal)

	err := consumer.RouteFailures(t.Context(),
		[]domain.AlarmQueueEnvelope{retryApplied, retryUnapplied},
		[]domain.AlarmQueueEnvelope{dlqApplied})
	if _, ok := errors.AsType[*PartialTransitionError](err); !ok {
		t.Fatalf("RouteFailures() error = %T %v, want *PartialTransitionError", err, err)
	}

	if got := testutil.ToFloat64(alarmDispatchPGRetryScheduledTotal); got != retryBefore+1 {
		t.Errorf("retry_scheduled = %v, want %v (applied retry subset only)", got, retryBefore+1)
	}

	if got := testutil.ToFloat64(alarmDispatchPGDLQTotal); got != dlqBefore+1 {
		t.Errorf("dlq_total = %v, want %v (applied dlq subset)", got, dlqBefore+1)
	}
}

func TestConsumerRouteFailuresInfraErrorObservesNothing(t *testing.T) {
	repository := &consumerTestRepository{}

	repository.routeFailuresFunc = func(context.Context, []FailureUpdate, string) error {
		return errors.New("connection reset")
	}

	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))
	retry := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 11,
		Retry:            &domain.AlarmQueueRetryMetadata{Attempt: 1, LastError: "boom"},
	}

	retryBefore := testutil.ToFloat64(alarmDispatchPGRetryScheduledTotal)
	dlqBefore := testutil.ToFloat64(alarmDispatchPGDLQTotal)

	if err := consumer.RouteFailures(t.Context(), []domain.AlarmQueueEnvelope{retry}, nil); err == nil {
		t.Fatal("RouteFailures() error = nil, want infra error")
	}

	if got := testutil.ToFloat64(alarmDispatchPGRetryScheduledTotal); got != retryBefore {
		t.Errorf("retry_scheduled moved on infra error: %v -> %v", retryBefore, got)
	}

	if got := testutil.ToFloat64(alarmDispatchPGDLQTotal); got != dlqBefore {
		t.Errorf("dlq_total moved on infra error: %v -> %v", dlqBefore, got)
	}
}

func TestConsumerRouteSendingFailuresUsesSendingVariant(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))
	envelope := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 7,
		Retry:            &domain.AlarmQueueRetryMetadata{Attempt: 1, LastError: "502"},
	}

	if err := consumer.RouteSendingFailures(t.Context(), []domain.AlarmQueueEnvelope{envelope}, nil); err != nil {
		t.Fatalf("RouteSendingFailures() error = %v", err)
	}

	if repository.routeSendingFailuresCalls != 1 || repository.routeFailuresCalls != 0 {
		t.Fatalf("route calls = %d/%d, want 1 RouteSendingFailures only", repository.routeFailuresCalls, repository.routeSendingFailuresCalls)
	}

	if len(repository.routedFailureUpdates) != 1 || repository.routedFailureUpdates[0].TargetStatus != StatusRetry {
		t.Fatalf("updates = %+v, want single retry-targeted update", repository.routedFailureUpdates)
	}
}

func TestConsumerRequeuePreSendPreservesAttemptCount(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))
	envelope := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 7,
		Retry:            &domain.AlarmQueueRetryMetadata{Attempt: 2, LastError: "mark sending"},
	}

	if err := consumer.RequeuePreSend(t.Context(), []domain.AlarmQueueEnvelope{envelope}); err != nil {
		t.Fatalf("RequeuePreSend() error = %v", err)
	}

	if repository.requeuePreSendCalls != 1 || repository.routeSendingFailuresCalls != 0 || repository.routeFailuresCalls != 0 {
		t.Fatalf("route calls = pre-send:%d sending:%d leased:%d", repository.requeuePreSendCalls, repository.routeSendingFailuresCalls, repository.routeFailuresCalls)
	}

	if len(repository.routedFailureUpdates) != 1 || repository.routedFailureUpdates[0].AttemptCount != 2 {
		t.Fatalf("updates = %+v, want unchanged attempt_count=2", repository.routedFailureUpdates)
	}
}

func TestConsumerRequeueRoutesAllEnvelopesAsRetry(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))
	envelopes := []domain.AlarmQueueEnvelope{
		{DispatchOutboxID: 1, Retry: &domain.AlarmQueueRetryMetadata{Attempt: 1}},
		{DispatchOutboxID: 2, Retry: &domain.AlarmQueueRetryMetadata{Attempt: 3}},
	}

	if err := consumer.Requeue(t.Context(), envelopes); err != nil {
		t.Fatalf("Requeue() error = %v", err)
	}

	if repository.routeFailuresCalls != 1 {
		t.Fatalf("RouteFailures calls = %d, want 1", repository.routeFailuresCalls)
	}

	for _, update := range repository.routedFailureUpdates {
		if update.TargetStatus != StatusRetry {
			t.Fatalf("requeue update = %+v, want retry target for every row", update)
		}
	}
}

func TestConsumerRouteFailuresPropagatesRepositoryError(t *testing.T) {
	t.Parallel()

	repositoryErr := errors.New("route down")
	repository := &consumerTestRepository{
		routeFailuresFunc: func(context.Context, []FailureUpdate, string) error {
			return repositoryErr
		},
	}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

	err := consumer.RouteFailures(t.Context(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 1, Retry: &domain.AlarmQueueRetryMetadata{Attempt: 1}}}, nil)
	if !errors.Is(err, repositoryErr) {
		t.Fatalf("RouteFailures() error = %v, want %v", err, repositoryErr)
	}
}

func TestConsumerRouteFailuresSanitizesErrorAndPassesCodeThrough(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))
	envelope := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 5,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       1,
			LastError:     "post https://iris.internal/reply?auth=abc123 failed: Bearer tok456",
			LastErrorCode: ErrorCodeHTTP5xx,
		},
	}

	if err := consumer.RouteFailures(t.Context(), []domain.AlarmQueueEnvelope{envelope}, nil); err != nil {
		t.Fatalf("RouteFailures() error = %v", err)
	}

	if len(repository.routedFailureUpdates) != 1 {
		t.Fatalf("updates = %d, want 1", len(repository.routedFailureUpdates))
	}

	update := repository.routedFailureUpdates[0]
	if update.ErrorCode != ErrorCodeHTTP5xx {
		t.Fatalf("ErrorCode = %q, want %q (metadata code must pass through unre-classified)", update.ErrorCode, ErrorCodeHTTP5xx)
	}

	if strings.Contains(update.Error, "abc123") || strings.Contains(update.Error, "tok456") {
		t.Fatalf("Error = %q, credential leaked through chokepoint", update.Error)
	}

	if !strings.Contains(update.Error, "[redacted]") {
		t.Fatalf("Error = %q, want redaction marker", update.Error)
	}
}

func TestConsumerQuarantineStoresSanitizedCauseAndCode(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))
	cause := fmt.Errorf("send karing content list: %w", &iris.HTTPError{StatusCode: 500, URL: "https://iris.internal/reply?auth=abc123"})

	if err := consumer.Quarantine(t.Context(), []domain.AlarmQueueEnvelope{{DispatchOutboxID: 9}}, cause); err != nil {
		t.Fatalf("Quarantine() error = %v", err)
	}

	if len(repository.quarantineUpdates) != 1 {
		t.Fatalf("updates = %d, want 1", len(repository.quarantineUpdates))
	}

	update := repository.quarantineUpdates[0]
	if update.ID != 9 {
		t.Fatalf("ID = %d, want 9", update.ID)
	}

	if update.ErrorCode != ErrorCodeHTTP5xx {
		t.Fatalf("ErrorCode = %q, want %q", update.ErrorCode, ErrorCodeHTTP5xx)
	}

	if strings.Contains(update.Error, "auth=abc123") {
		t.Fatalf("Error = %q, query string leaked", update.Error)
	}
}

func TestConsumerMoveRecordToDLQUsesPayloadCode(t *testing.T) {
	t.Parallel()

	repository := &consumerTestRepository{}
	consumer := NewConsumer(repository, slog.Default(), WithWorkerID("worker-1"))

	if err := consumer.moveRecordToDLQ(t.Context(), 3, "invalid payload: unexpected EOF", "move invalid payload to dlq"); err != nil {
		t.Fatalf("moveRecordToDLQ() error = %v", err)
	}

	if len(repository.movedDLQUpdates) != 1 {
		t.Fatalf("updates = %d, want 1", len(repository.movedDLQUpdates))
	}

	update := repository.movedDLQUpdates[0]
	if update.ErrorCode != ErrorCodePayload {
		t.Fatalf("ErrorCode = %q, want %q", update.ErrorCode, ErrorCodePayload)
	}

	if update.Error != "invalid payload: unexpected EOF" {
		t.Fatalf("Error = %q, want original message preserved", update.Error)
	}
}
