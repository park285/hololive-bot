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
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

// mockDeliveryRepo: deliveryRepository mock 구현
type mockDeliveryRepo struct {
	fetchAndLockFn  func(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.NotificationDeliveryOutbox, error)
	markSentFn      func(ctx context.Context, id int64) error
	markFailedFn    func(ctx context.Context, id int64, maxRetries int, backoff time.Duration, errMsg string) error
	countByStatusFn func(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error)
	cleanupFn       func(ctx context.Context, olderThan time.Duration) (int64, error)
}

func (m *mockDeliveryRepo) FetchAndLock(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
	if m.fetchAndLockFn != nil {
		return m.fetchAndLockFn(ctx, batchSize, lockTimeout)
	}
	return nil, nil
}

func (m *mockDeliveryRepo) MarkSent(ctx context.Context, id int64) error {
	if m.markSentFn != nil {
		return m.markSentFn(ctx, id)
	}
	return nil
}

func (m *mockDeliveryRepo) MarkFailed(ctx context.Context, id int64, maxRetries int, backoff time.Duration, errMsg string) error {
	if m.markFailedFn != nil {
		return m.markFailedFn(ctx, id, maxRetries, backoff, errMsg)
	}
	return nil
}

func (m *mockDeliveryRepo) CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error) {
	if m.countByStatusFn != nil {
		return m.countByStatusFn(ctx, status)
	}
	return 0, nil
}

func (m *mockDeliveryRepo) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
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
	b, _ := json.Marshal(outboxPayload{Message: msg})
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
	repo := &mockDeliveryRepo{}
	dispatcher := NewDispatcher(repo, sender, dispatcherLogger(), DispatcherConfig{})
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

	repo := &mockDeliveryRepo{
		fetchAndLockFn: func(_ context.Context, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 1, RoomID: "room-a", Payload: makePayload(t, "hello-a")},
				{ID: 2, RoomID: "room-b", Payload: makePayload(t, "hello-b")},
			}, nil
		},
		markSentFn: func(_ context.Context, id int64) error {
			mu.Lock()
			defer mu.Unlock()
			sentIDs = append(sentIDs, id)
			return nil
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

	d := NewDispatcher(repo, sender, dispatcherLogger(), DefaultDispatcherConfig())
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

	repo := &mockDeliveryRepo{
		fetchAndLockFn: func(_ context.Context, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 10, RoomID: "room-x", Payload: "invalid-json{{{"},
			}, nil
		},
		markFailedFn: func(_ context.Context, id int64, _ int, _ time.Duration, errMsg string) error {
			failedID = id
			failedMsg = errMsg
			return nil
		},
		countByStatusFn: func(_ context.Context, _ domain.DeliveryOutboxStatus) (int64, error) {
			return 0, nil
		},
		cleanupFn: func(_ context.Context, _ time.Duration) (int64, error) {
			return 0, nil
		},
	}

	sender := &mockSender{}

	d := NewDispatcher(repo, sender, dispatcherLogger(), DefaultDispatcherConfig())
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

	repo := &mockDeliveryRepo{
		fetchAndLockFn: func(_ context.Context, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 20, RoomID: "room-y", Payload: makePayload(t, "hello")},
			}, nil
		},
		markFailedFn: func(_ context.Context, id int64, _ int, _ time.Duration, _ string) error {
			failedID = id
			return nil
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

	d := NewDispatcher(repo, sender, dispatcherLogger(), DefaultDispatcherConfig())
	d.processOnce(context.Background())

	if failedID != 20 {
		t.Fatalf("expected failed ID=20, got %d", failedID)
	}
}

func TestDispatcher_ContextCancel_StopsGoroutine(t *testing.T) {
	var fetchCount atomic.Int32

	repo := &mockDeliveryRepo{
		fetchAndLockFn: func(_ context.Context, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			fetchCount.Add(1)
			return nil, nil
		},
	}

	sender := &mockSender{}

	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	d := NewDispatcher(repo, sender, dispatcherLogger(), cfg)
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

func TestDispatcher_StartProcessesOnceBeforeFirstTick(t *testing.T) {
	var fetchCount atomic.Int32
	firstFetch := make(chan struct{})
	var closeFirstFetch sync.Once

	repo := &mockDeliveryRepo{
		fetchAndLockFn: func(_ context.Context, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			fetchCount.Add(1)
			closeFirstFetch.Do(func() {
				close(firstFetch)
			})
			return nil, nil
		},
	}

	cfg := DefaultDispatcherConfig()
	cfg.PollInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := NewDispatcher(repo, &mockSender{}, dispatcherLogger(), cfg)
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

	repo := &mockDeliveryRepo{
		fetchAndLockFn: func(_ context.Context, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 1, RoomID: "room-a", Payload: makePayload(t, "hello-a")},
				{ID: 2, RoomID: "room-b", Payload: makePayload(t, "hello-b")},
				{ID: 3, RoomID: "room-c", Payload: makePayload(t, "hello-c")},
				{ID: 4, RoomID: "room-d", Payload: makePayload(t, "hello-d")},
			}, nil
		},
		markSentFn: func(_ context.Context, _ int64) error {
			sentCount.Add(1)
			return nil
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

	cfg := DefaultDispatcherConfig()
	cfg.MaxConcurrent = 2

	d := NewDispatcher(repo, sender, dispatcherLogger(), cfg)
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
