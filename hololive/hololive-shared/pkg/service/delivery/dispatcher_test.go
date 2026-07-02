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

package delivery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/pkg/json"
)

// mockDeliveryRepository: deliveryRepository mock 구현
type mockDeliveryRepository struct {
	fetchAndLockFn  func(ctx context.Context, workerID string, batchSize int, lockTimeout, lease time.Duration) ([]domain.NotificationDeliveryOutbox, error)
	markSentFn      func(ctx context.Context, id int64, workerID string, lockedAt time.Time) (bool, error)
	markFailedFn    func(ctx context.Context, id int64, workerID string, lockedAt time.Time, maxRetries int, backoff time.Duration, errMsg string) (bool, error)
	countByStatusFn func(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error)
	cleanupFn       func(ctx context.Context, olderThan time.Duration) (int64, error)
}

func (m *mockDeliveryRepository) FetchAndLock(ctx context.Context, workerID string, batchSize int, lockTimeout, lease time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
	if m.fetchAndLockFn != nil {
		return m.fetchAndLockFn(ctx, workerID, batchSize, lockTimeout, lease)
	}
	return nil, nil
}

func (m *mockDeliveryRepository) MarkSent(ctx context.Context, id int64, workerID string, lockedAt time.Time) (bool, error) {
	if m.markSentFn != nil {
		return m.markSentFn(ctx, id, workerID, lockedAt)
	}
	return true, nil
}

func (m *mockDeliveryRepository) MarkFailed(ctx context.Context, id int64, workerID string, lockedAt time.Time, maxRetries int, backoff time.Duration, errMsg string) (bool, error) {
	if m.markFailedFn != nil {
		return m.markFailedFn(ctx, id, workerID, lockedAt, maxRetries, backoff, errMsg)
	}
	return true, nil
}

func (m *mockDeliveryRepository) CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error) {
	if m.countByStatusFn != nil {
		return m.countByStatusFn(ctx, status)
	}
	return 0, nil
}

func (m *mockDeliveryRepository) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	if m.cleanupFn != nil {
		return m.cleanupFn(ctx, olderThan)
	}
	return 0, nil
}

// mockSender: MessageSender mock 구현
type mockSender struct {
	sendFn                func(ctx context.Context, roomID, message string) error
	sendWithClientRequest func(ctx context.Context, roomID, message, clientRequestID string) error
}

func (m *mockSender) SendMessage(ctx context.Context, roomID, message string) error {
	if m.sendFn != nil {
		return m.sendFn(ctx, roomID, message)
	}
	return nil
}

func (m *mockSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if m.sendWithClientRequest != nil {
		return m.sendWithClientRequest(ctx, roomID, message, clientRequestID)
	}
	return m.SendMessage(ctx, roomID, message)
}

func makePayload(t *testing.T, msg string) string {
	t.Helper()
	b, err := json.Marshal(outboxPayload{Message: msg})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return string(b)
}

func dispatcherLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProcessItemPassesStableClientRequestID(t *testing.T) {
	var gotIDs []string
	sender := &mockSender{
		sendWithClientRequest: func(_ context.Context, _, _, clientRequestID string) error {
			gotIDs = append(gotIDs, clientRequestID)
			return nil
		},
	}
	repository := &mockDeliveryRepository{}
	dispatcher := NewDispatcher(repository, sender, dispatcherLogger(), DispatcherConfig{})
	item := &domain.NotificationDeliveryOutbox{
		ID:        42,
		Kind:      domain.DeliveryKindMemberNewsWeekly,
		ContentID: "member-news-2026w20",
		RoomID:    "room-1",
		Payload:   makePayload(t, "hello"),
	}

	dispatcher.processItem(context.Background(), item)
	dispatcher.processItem(context.Background(), item)
	otherRoom := *item
	otherRoom.RoomID = "room-2"
	dispatcher.processItem(context.Background(), &otherRoom)

	if len(gotIDs) != 3 {
		t.Fatalf("clientRequestIDs count = %d, want 3", len(gotIDs))
	}
	if gotIDs[0] == "" || !strings.HasPrefix(gotIDs[0], "hololive-delivery:") {
		t.Fatalf("clientRequestID = %q, want hololive-delivery prefix", gotIDs[0])
	}
	if gotIDs[0] != gotIDs[1] {
		t.Fatalf("clientRequestID repeat = %q, want %q", gotIDs[1], gotIDs[0])
	}
	if gotIDs[2] == gotIDs[0] {
		t.Fatalf("clientRequestID for different room reused %q", gotIDs[2])
	}
}

func TestProcessOnce_E2E(t *testing.T) {
	var mu sync.Mutex
	var sentIDs []int64
	var sentRooms []string

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 1, RoomID: "room-a", Payload: makePayload(t, "hello-a")},
				{ID: 2, RoomID: "room-b", Payload: makePayload(t, "hello-b")},
			}, nil
		},
		markSentFn: func(_ context.Context, id int64, _ string, _ time.Time) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			sentIDs = append(sentIDs, id)
			return true, nil
		},
		countByStatusFn: func(_ context.Context, _ domain.DeliveryOutboxStatus) (int64, error) {
			return 0, nil
		},
		cleanupFn: func(_ context.Context, _ time.Duration) (int64, error) {
			return 0, nil
		},
	}

	sender := &mockSender{
		sendFn: func(_ context.Context, roomID, _ string) error {
			mu.Lock()
			defer mu.Unlock()
			sentRooms = append(sentRooms, roomID)
			return nil
		},
	}

	d := NewDispatcher(repository, sender, dispatcherLogger(), DefaultDispatcherConfig())
	d.processOnce(context.Background())

	if len(sentIDs) != 2 {
		t.Fatalf("expected 2 items marked sent, got %d", len(sentIDs))
	}
	if len(sentRooms) != 2 {
		t.Fatalf("expected 2 rooms sent, got %d", len(sentRooms))
	}
}

func TestProcessOnce_UnmarshalFailure_MarkFailed(t *testing.T) {
	var failedID int64
	var failedMsg string

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 10, RoomID: "room-x", Payload: "invalid-json{{{"},
			}, nil
		},
		markFailedFn: func(_ context.Context, id int64, _ string, _ time.Time, _ int, _ time.Duration, errMsg string) (bool, error) {
			failedID = id
			failedMsg = errMsg
			return true, nil
		},
		countByStatusFn: func(_ context.Context, _ domain.DeliveryOutboxStatus) (int64, error) {
			return 0, nil
		},
		cleanupFn: func(_ context.Context, _ time.Duration) (int64, error) {
			return 0, nil
		},
	}

	sender := &mockSender{}

	d := NewDispatcher(repository, sender, dispatcherLogger(), DefaultDispatcherConfig())
	d.processOnce(context.Background())

	if failedID != 10 {
		t.Fatalf("expected failed ID=10, got %d", failedID)
	}
	if failedMsg == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestProcessOnce_SenderFailure_MarkFailed(t *testing.T) {
	var failedID int64

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 20, RoomID: "room-y", Payload: makePayload(t, "hello")},
			}, nil
		},
		markFailedFn: func(_ context.Context, id int64, _ string, _ time.Time, _ int, _ time.Duration, _ string) (bool, error) {
			failedID = id
			return true, nil
		},
		countByStatusFn: func(_ context.Context, _ domain.DeliveryOutboxStatus) (int64, error) {
			return 0, nil
		},
		cleanupFn: func(_ context.Context, _ time.Duration) (int64, error) {
			return 0, nil
		},
	}

	sender := &mockSender{
		sendFn: func(_ context.Context, _, _ string) error {
			return errors.New("kakao API error")
		},
	}

	d := NewDispatcher(repository, sender, dispatcherLogger(), DefaultDispatcherConfig())
	d.processOnce(context.Background())

	if failedID != 20 {
		t.Fatalf("expected failed ID=20, got %d", failedID)
	}
}

func TestDispatcher_ContextCancel_StopsGoroutine(t *testing.T) {
	var fetchCount atomic.Int32

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			fetchCount.Add(1)
			return nil, nil
		},
	}

	sender := &mockSender{}

	config := DefaultDispatcherConfig()
	config.PollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	d := NewDispatcher(repository, sender, dispatcherLogger(), config)
	d.Start(ctx)

	// 초기 실행 + ticker 몇 회 대기
	time.Sleep(50 * time.Millisecond)
	cancel()

	// cancel 후 count 고정 확인
	time.Sleep(30 * time.Millisecond)
	countAfterCancel := fetchCount.Load()
	time.Sleep(30 * time.Millisecond)
	countFinal := fetchCount.Load()

	if countFinal != countAfterCancel {
		t.Fatalf("goroutine leaked: count grew from %d to %d after cancel", countAfterCancel, countFinal)
	}
	if countAfterCancel == 0 {
		t.Fatal("expected at least 1 fetch call")
	}
}

func TestDispatcher_RunFetchesOnPeriodicTickAndStopsOnCancel(t *testing.T) {
	var fetchCount atomic.Int32
	firstFetch := make(chan struct{})
	secondFetch := make(chan struct{})
	var closeFirstFetch sync.Once
	var closeSecondFetch sync.Once

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			switch fetchCount.Add(1) {
			case 1:
				closeFirstFetch.Do(func() {
					close(firstFetch)
				})
			case 2:
				closeSecondFetch.Do(func() {
					close(secondFetch)
				})
			}
			return nil, nil
		},
	}

	config := DefaultDispatcherConfig()
	config.PollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	d := NewDispatcher(repository, &mockSender{}, dispatcherLogger(), config)
	done := make(chan struct{})
	go func() {
		d.run(ctx)
		close(done)
	}()

	select {
	case <-firstFetch:
	case <-time.After(250 * time.Millisecond):
		cancel()
		t.Fatal("dispatcher did not fetch before first ticker interval")
	}

	select {
	case <-secondFetch:
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatalf("fetch count = %d, want periodic tick fetch", fetchCount.Load())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("dispatcher run loop did not stop after context cancellation")
	}
}

func TestDispatcher_StartProcessesOnceBeforeFirstTick(t *testing.T) {
	var fetchCount atomic.Int32
	firstFetch := make(chan struct{})
	var closeFirstFetch sync.Once

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			fetchCount.Add(1)
			closeFirstFetch.Do(func() {
				close(firstFetch)
			})
			return nil, nil
		},
	}

	config := DefaultDispatcherConfig()
	config.PollInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := NewDispatcher(repository, &mockSender{}, dispatcherLogger(), config)
	d.Start(ctx)

	select {
	case <-firstFetch:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("dispatcher did not process before first ticker interval")
	}

	cancel()
	time.Sleep(30 * time.Millisecond)

	if got := fetchCount.Load(); got != 1 {
		t.Fatalf("fetch count = %d, want 1 before first ticker interval", got)
	}
}

func TestProcessOnce_RespectsMaxConcurrent(t *testing.T) {
	var current atomic.Int32
	var maxRunning atomic.Int32
	var sentCount atomic.Int32

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 1, RoomID: "room-a", Payload: makePayload(t, "hello-a")},
				{ID: 2, RoomID: "room-b", Payload: makePayload(t, "hello-b")},
				{ID: 3, RoomID: "room-c", Payload: makePayload(t, "hello-c")},
				{ID: 4, RoomID: "room-d", Payload: makePayload(t, "hello-d")},
			}, nil
		},
		markSentFn: func(_ context.Context, _ int64, _ string, _ time.Time) (bool, error) {
			sentCount.Add(1)
			return true, nil
		},
		countByStatusFn: func(_ context.Context, _ domain.DeliveryOutboxStatus) (int64, error) {
			return 0, nil
		},
		cleanupFn: func(_ context.Context, _ time.Duration) (int64, error) {
			return 0, nil
		},
	}

	sender := &mockSender{
		sendFn: func(_ context.Context, _, _ string) error {
			running := current.Add(1)
			for {
				existing := maxRunning.Load()
				if running <= existing || maxRunning.CompareAndSwap(existing, running) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			current.Add(-1)
			return nil
		},
	}

	config := DefaultDispatcherConfig()
	config.MaxConcurrent = 2

	d := NewDispatcher(repository, sender, dispatcherLogger(), config)
	d.processOnce(context.Background())

	if sentCount.Load() != 4 {
		t.Fatalf("expected 4 items marked sent, got %d", sentCount.Load())
	}
	if maxRunning.Load() > 2 {
		t.Fatalf("expected max concurrency <= 2, got %d", maxRunning.Load())
	}
	if maxRunning.Load() != 2 {
		t.Fatalf("expected dispatcher to use configured concurrency 2, got %d", maxRunning.Load())
	}
}
