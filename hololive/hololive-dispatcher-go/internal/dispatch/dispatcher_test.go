// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dispatch

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
)

func TestDispatcherRunOnce_RenderFailureReleasesClaimKeys(t *testing.T) {
	t.Parallel()

	fakeConsumer := &testQueueConsumer{
		drainBatchFunc: func(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
			return []domain.AlarmQueueEnvelope{
				{
					Notification: domain.AlarmNotification{
						RoomID:       "room-1",
						MinutesUntil: 5,
						Channel:      &domain.Channel{ID: "channel-id", Name: "멤버"},
						Stream: &domain.Stream{
							ID:          "stream-1",
							Title:       "테스트 방송",
							ChannelID:   "channel-id",
							ChannelName: "멤버",
						},
					},
					ClaimKeys: []string{"claim-1", "claim-2"},
				},
			}, nil
		},
	}

	dispatcher, err := NewDispatcher(
		fakeConsumer,
		&testMessageSender{},
		&failingTestRenderer{err: errors.New("render failed")},
		50,
		1,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	if runErr := dispatcher.RunOnce(context.Background()); runErr != nil {
		t.Fatalf("RunOnce() error = %v", runErr)
	}

	if len(fakeConsumer.releasedClaimKeys) != 2 {
		t.Fatalf("expected 2 released claim keys, got %d", len(fakeConsumer.releasedClaimKeys))
	}
	if len(fakeConsumer.requeuedEnvelopes) != 0 {
		t.Fatalf("expected 0 requeued envelopes, got %d", len(fakeConsumer.requeuedEnvelopes))
	}
}

func TestDispatcherRunOnce_SendFailureSchedulesDurableRetryWithMetadata(t *testing.T) {
	t.Parallel()

	fakeConsumer := &testQueueConsumer{
		drainBatchFunc: func(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
			return []domain.AlarmQueueEnvelope{
				testAlarmQueueEnvelope("room-1", "claim-1"),
			}, nil
		},
	}

	fakeSender := &testMessageSender{
		sendMessageFunc: func(ctx context.Context, room, message string, opts ...iris.SendOption) error {
			return errors.New("send failed")
		},
	}

	dispatcher, err := NewDispatcher(
		fakeConsumer,
		fakeSender,
		NewSimpleRenderer(),
		50,
		1,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	now := time.Date(2026, 4, 14, 4, 0, 0, 0, time.UTC)
	dispatcher.now = func() time.Time { return now }
	dispatcher.randFloat64 = func() float64 { return 1 }
	if err := dispatcher.ConfigureRetryPolicy(RetryPolicy{
		MaxAttempts:   3,
		BaseBackoff:   time.Minute,
		MaxBackoff:    5 * time.Minute,
		JitterPercent: 25,
	}); err != nil {
		t.Fatalf("ConfigureRetryPolicy() error = %v", err)
	}

	if runErr := dispatcher.RunOnce(context.Background()); runErr != nil {
		t.Fatalf("RunOnce() error = %v", runErr)
	}

	if len(fakeConsumer.scheduledRetries) != 1 {
		t.Fatalf("scheduled retries = %d, want 1", len(fakeConsumer.scheduledRetries))
	}
	if len(fakeConsumer.releasedClaimKeys) != 0 {
		t.Fatalf("expected released claim keys = 0, got %d", len(fakeConsumer.releasedClaimKeys))
	}
	if len(fakeConsumer.dlqEnvelopes) != 0 {
		t.Fatalf("expected 0 DLQ envelopes, got %d", len(fakeConsumer.dlqEnvelopes))
	}
	if fakeSender.sendCalls != 1 {
		t.Fatalf("send calls = %d, want 1", fakeSender.sendCalls)
	}

	retry := fakeConsumer.scheduledRetries[0].Retry
	if retry == nil {
		t.Fatal("scheduled retry metadata is nil")
	}
	if retry.Attempt != 1 {
		t.Fatalf("retry attempt = %d, want 1", retry.Attempt)
	}
	if retry.RetryAfterMS != int64(75*time.Second/time.Millisecond) {
		t.Fatalf("retry_after_ms = %d, want %d", retry.RetryAfterMS, int64(75*time.Second/time.Millisecond))
	}
	if retry.NextVisibleAt != now.Add(75*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("next_visible_at = %q, want %q", retry.NextVisibleAt, now.Add(75*time.Second).Format(time.RFC3339Nano))
	}
	if retry.LastError != "send failed" {
		t.Fatalf("last_error = %q, want %q", retry.LastError, "send failed")
	}
	if fakeConsumer.scheduledRetries[0].ClaimKeys[0] != "claim-1" {
		t.Fatalf("claim key = %q, want %q", fakeConsumer.scheduledRetries[0].ClaimKeys[0], "claim-1")
	}
}

func TestDispatcherRunOnce_SendFailureMovesToDLQAfterRetryBudgetExhausted(t *testing.T) {
	t.Parallel()

	fakeConsumer := &testQueueConsumer{
		drainBatchFunc: func(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
			envelope := testAlarmQueueEnvelope("room-1", "claim-1")
			envelope.Retry = &domain.AlarmQueueRetryMetadata{
				Attempt:       2,
				RetryAfterMS:  int64((45 * time.Second) / time.Millisecond),
				NextVisibleAt: time.Date(2026, 4, 14, 4, 0, 45, 0, time.UTC).Format(time.RFC3339Nano),
				LastError:     "previous send failed",
			}
			return []domain.AlarmQueueEnvelope{envelope}, nil
		},
	}

	dispatcher, err := NewDispatcher(
		fakeConsumer,
		&testMessageSender{
			sendMessageFunc: func(ctx context.Context, room, message string, opts ...iris.SendOption) error {
				return errors.New("send failed")
			},
		},
		NewSimpleRenderer(),
		50,
		1,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	if err := dispatcher.ConfigureRetryPolicy(RetryPolicy{
		MaxAttempts:   3,
		BaseBackoff:   time.Minute,
		MaxBackoff:    5 * time.Minute,
		JitterPercent: 0,
	}); err != nil {
		t.Fatalf("ConfigureRetryPolicy() error = %v", err)
	}

	if runErr := dispatcher.RunOnce(context.Background()); runErr != nil {
		t.Fatalf("RunOnce() error = %v", runErr)
	}

	if len(fakeConsumer.scheduledRetries) != 0 {
		t.Fatalf("scheduled retries = %d, want 0", len(fakeConsumer.scheduledRetries))
	}
	if len(fakeConsumer.dlqEnvelopes) != 1 {
		t.Fatalf("DLQ envelopes = %d, want 1", len(fakeConsumer.dlqEnvelopes))
	}
	if len(fakeConsumer.releasedClaimKeys) != 1 {
		t.Fatalf("released claim keys = %d, want 1", len(fakeConsumer.releasedClaimKeys))
	}

	retry := fakeConsumer.dlqEnvelopes[0].Retry
	if retry == nil {
		t.Fatal("DLQ envelope retry metadata is nil")
	}
	if retry.Attempt != 3 {
		t.Fatalf("retry attempt = %d, want 3", retry.Attempt)
	}
	if retry.LastError != "send failed" {
		t.Fatalf("last_error = %q, want %q", retry.LastError, "send failed")
	}
}

func TestDispatcherRetryPolicy_BackoffRampsAndCaps(t *testing.T) {
	t.Parallel()

	dispatcher, err := NewDispatcher(
		&testQueueConsumer{},
		&testMessageSender{},
		NewSimpleRenderer(),
		10,
		1,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	dispatcher.randFloat64 = func() float64 { return 1 }
	if err := dispatcher.ConfigureRetryPolicy(RetryPolicy{
		MaxAttempts:   5,
		BaseBackoff:   10 * time.Second,
		MaxBackoff:    25 * time.Second,
		JitterPercent: 50,
	}); err != nil {
		t.Fatalf("ConfigureRetryPolicy() error = %v", err)
	}

	if got := dispatcher.retryBackoffForAttempt(1); got != 15*time.Second {
		t.Fatalf("retryBackoffForAttempt(1) = %v, want %v", got, 15*time.Second)
	}
	if got := dispatcher.retryBackoffForAttempt(3); got != 25*time.Second {
		t.Fatalf("retryBackoffForAttempt(3) = %v, want %v", got, 25*time.Second)
	}
}

func TestDispatcherRunOnce_DrainError(t *testing.T) {
	t.Parallel()

	fakeConsumer := &testQueueConsumer{
		drainBatchFunc: func(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
			return nil, errors.New("valkey unavailable")
		},
	}
	dispatcher, err := NewDispatcher(
		fakeConsumer,
		&testMessageSender{},
		NewSimpleRenderer(),
		50,
		1,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	runErr := dispatcher.RunOnce(context.Background())
	if runErr == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestDispatcherRunOnce_UsesConfiguredParallelism(t *testing.T) {
	t.Parallel()

	fakeConsumer := &testQueueConsumer{
		drainBatchFunc: func(context.Context, int) ([]domain.AlarmQueueEnvelope, error) {
			return []domain.AlarmQueueEnvelope{
				testAlarmQueueEnvelope("room-1", "claim-1"),
				testAlarmQueueEnvelope("room-2", "claim-2"),
				testAlarmQueueEnvelope("room-3", "claim-3"),
			}, nil
		},
	}

	release := make(chan struct{})
	started := make(chan struct{}, 3)
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32

	fakeSender := &testMessageSender{
		sendMessageFunc: func(ctx context.Context, room, message string, opts ...iris.SendOption) error {
			current := inFlight.Add(1)
			for {
				recorded := maxInFlight.Load()
				if current <= recorded || maxInFlight.CompareAndSwap(recorded, current) {
					break
				}
			}
			started <- struct{}{}
			defer inFlight.Add(-1)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		},
	}

	dispatcher, err := NewDispatcher(
		fakeConsumer,
		fakeSender,
		NewSimpleRenderer(),
		50,
		2,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- dispatcher.RunOnce(context.Background())
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for parallel sends to start")
		}
	}

	select {
	case <-started:
		t.Fatal("third send started before a parallel slot was released")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("RunOnce() error = %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunOnce() did not complete in time")
	}

	if got := maxInFlight.Load(); got != 2 {
		t.Fatalf("max parallel sends = %d, want 2", got)
	}
}

func testAlarmQueueEnvelope(roomID, claimKey string) domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			RoomID:       roomID,
			MinutesUntil: 5,
			Channel:      &domain.Channel{ID: "channel-" + roomID, Name: "멤버"},
			Stream: &domain.Stream{
				ID:          "stream-" + roomID,
				Title:       "테스트 방송",
				ChannelID:   "channel-" + roomID,
				ChannelName: "멤버",
			},
		},
		ClaimKeys: []string{claimKey},
	}
}

type testQueueConsumer struct {
	drainBatchFunc    func(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	releaseClaimKeys  func(ctx context.Context, claimKeys []string) error
	scheduleRetryFunc func(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	moveToDLQFunc     func(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	requeueFunc       func(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	releasedClaimKeys []string
	scheduledRetries  []domain.AlarmQueueEnvelope
	dlqEnvelopes      []domain.AlarmQueueEnvelope
	requeuedEnvelopes []domain.AlarmQueueEnvelope
}

func (c *testQueueConsumer) DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
	if c.drainBatchFunc != nil {
		return c.drainBatchFunc(ctx, maxItems)
	}
	return nil, nil
}

func (c *testQueueConsumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	if c.releaseClaimKeys != nil {
		return c.releaseClaimKeys(ctx, claimKeys)
	}
	c.releasedClaimKeys = append(c.releasedClaimKeys, claimKeys...)
	return nil
}

func (c *testQueueConsumer) ScheduleRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if c.scheduleRetryFunc != nil {
		return c.scheduleRetryFunc(ctx, envelopes)
	}
	c.scheduledRetries = append(c.scheduledRetries, envelopes...)
	return nil
}

func (c *testQueueConsumer) MoveToDLQ(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if c.moveToDLQFunc != nil {
		return c.moveToDLQFunc(ctx, envelopes)
	}
	c.dlqEnvelopes = append(c.dlqEnvelopes, envelopes...)
	return nil
}

func (c *testQueueConsumer) Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if c.requeueFunc != nil {
		return c.requeueFunc(ctx, envelopes)
	}
	c.requeuedEnvelopes = append(c.requeuedEnvelopes, envelopes...)
	return nil
}

type testMessageSender struct {
	sendMessageFunc func(ctx context.Context, room, message string, opts ...iris.SendOption) error
	sendCalls       int
}

func (s *testMessageSender) SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error {
	s.sendCalls++
	if s.sendMessageFunc != nil {
		return s.sendMessageFunc(ctx, room, message, opts...)
	}
	return nil
}

type failingTestRenderer struct {
	err error
}

func (r *failingTestRenderer) RenderGroup(ctx context.Context, group NotificationGroup) (string, error) {
	return "", r.err
}
