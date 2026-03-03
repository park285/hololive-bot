package delivery

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
	sendFn func(ctx context.Context, roomID, message string) error
}

func (m *mockSender) SendMessage(ctx context.Context, roomID, message string) error {
	if m.sendFn != nil {
		return m.sendFn(ctx, roomID, message)
	}
	return nil
}

func makePayload(t *testing.T, msg string) string {
	t.Helper()
	b, _ := json.Marshal(outboxPayload{Message: msg})
	return string(b)
}

func dispatcherLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProcessOnce_E2E(t *testing.T) {
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
