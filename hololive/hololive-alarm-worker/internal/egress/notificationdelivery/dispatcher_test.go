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

package notificationdelivery

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testRoomID       = "room-1"
	testRoomA        = "room-a"
	testFirstMessage = "first"
)

// mockDeliveryRepository: deliveryRepository mock 구현.
type mockDeliveryRepository struct {
	saveRequestFn            func(ctx context.Context, id int64, workerID string, attemptCount int, request *deliverypayload.Request) error
	reissueFn                func(ctx context.Context, id int64, workerID string, attemptCount, generation int, exhaust bool) error
	fetchAndLockFn           func(ctx context.Context, workerID string, batchSize int, lease time.Duration) ([]domain.NotificationDeliveryOutbox, error)
	markSendingFn            func(ctx context.Context, id int64, workerID string, attemptCount int, lease time.Duration) error
	markQuarantinedFn        func(ctx context.Context, id int64, workerID string, attemptCount int, reason string) error
	markSentFn               func(ctx context.Context, id int64, workerID string, attemptCount int) error
	markFailedFn             func(ctx context.Context, id int64, workerID string, attemptCount, maxRetries int, backoff time.Duration, errMsg string) error
	quarantineStaleSendingFn func(ctx context.Context, olderThan time.Duration, limit int) (int64, error)
	countFailedFn            func(ctx context.Context) (int64, error)
	cleanupFn                func(ctx context.Context, olderThan time.Duration) (int64, error)
	claimedIDs               map[int64]struct{}
}

// fenceMiss는 Store가 단일 행 전이에서 돌려주는 것과 같은 모양의 fence 미스다.
func fenceMiss(action string, id int64, reason sendfence.Reason, status domain.DeliveryOutboxStatus) error {
	return &sendfence.MissError{Action: action, Expected: 1, Rows: []sendfence.RowMiss{{ID: id, Reason: reason, Status: string(status)}}}
}

func (m *mockDeliveryRepository) reissueFailedRequest(ctx context.Context, id int64, worker string, attemptCount, generation int, exhaust bool, maxRetries int, backoff time.Duration, reason string) error {
	if m.reissueFn != nil {
		return m.reissueFn(ctx, id, worker, attemptCount, generation, exhaust)
	}

	return m.MarkFailed(ctx, id, worker, attemptCount, maxRetries, backoff, reason)
}

func (m *mockDeliveryRepository) saveRequest(ctx context.Context, id int64, worker string, attemptCount int, request *deliverypayload.Request) error {
	if m.saveRequestFn != nil {
		return m.saveRequestFn(ctx, id, worker, attemptCount, request)
	}

	return nil
}

func (m *mockDeliveryRepository) markRequestExhausted(ctx context.Context, id int64, worker string, attemptCount int, reason string) error {
	return m.MarkFailed(ctx, id, worker, attemptCount, 1, 0, reason)
}

func (m *mockDeliveryRepository) FetchAndLock(ctx context.Context, workerID string, batchSize int, lease time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
	if m.fetchAndLockFn != nil {
		out, err := m.fetchAndLockFn(ctx, workerID, batchSize, lease)
		if err != nil {
			return out, fmt.Errorf("fetch and lock fn: %w", err)
		}

		return out, nil
	}

	return nil, nil
}

func (m *mockDeliveryRepository) fetchReadyAndLock(ctx context.Context, workerID string, batchSize int, lease time.Duration, activeRooms []string, processedIDs []int64) ([]domain.NotificationDeliveryOutbox, error) {
	items, err := m.FetchAndLock(ctx, workerID, batchSize, lease)
	if err != nil {
		return nil, err
	}

	if m.claimedIDs == nil {
		m.claimedIDs = make(map[int64]struct{})
	}

	blocked := make(map[string]struct{}, len(activeRooms))
	for _, roomID := range activeRooms {
		blocked[roomID] = struct{}{}
	}

	ready := make([]domain.NotificationDeliveryOutbox, 0, batchSize)

	for i := range items {
		item := &items[i]

		if slices.Contains(processedIDs, item.ID) {
			continue
		}

		if _, claimed := m.claimedIDs[item.ID]; claimed {
			continue
		}

		if _, active := blocked[item.RoomID]; active {
			continue
		}

		ready = append(ready, *item)
		m.claimedIDs[item.ID] = struct{}{}
		blocked[item.RoomID] = struct{}{}

		if len(ready) == batchSize {
			break
		}
	}

	return ready, nil
}

func (m *mockDeliveryRepository) MarkSending(ctx context.Context, id int64, workerID string, attemptCount int, lease time.Duration) error {
	if m.markSendingFn != nil {
		return m.markSendingFn(ctx, id, workerID, attemptCount, lease)
	}

	return nil
}

func (m *mockDeliveryRepository) MarkSent(ctx context.Context, id int64, workerID string, attemptCount int) error {
	if m.markSentFn != nil {
		return m.markSentFn(ctx, id, workerID, attemptCount)
	}

	return nil
}

func (m *mockDeliveryRepository) MarkQuarantined(ctx context.Context, id int64, workerID string, attemptCount int, reason string) error {
	if m.markQuarantinedFn != nil {
		return m.markQuarantinedFn(ctx, id, workerID, attemptCount, reason)
	}

	return nil
}

func (m *mockDeliveryRepository) MarkFailed(ctx context.Context, id int64, workerID string, attemptCount, maxRetries int, backoff time.Duration, errMsg string) error {
	if m.markFailedFn != nil {
		return m.markFailedFn(ctx, id, workerID, attemptCount, maxRetries, backoff, errMsg)
	}

	return nil
}

func (m *mockDeliveryRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int64, error) {
	if m.quarantineStaleSendingFn != nil {
		out, err := m.quarantineStaleSendingFn(ctx, olderThan, limit)
		if err != nil {
			return out, fmt.Errorf("quarantine stale sending fn: %w", err)
		}

		return out, nil
	}

	return 0, nil
}

func (m *mockDeliveryRepository) CountFailed(ctx context.Context) (int64, error) {
	if m.countFailedFn != nil {
		out, err := m.countFailedFn(ctx)
		if err != nil {
			return out, fmt.Errorf("count failed fn: %w", err)
		}

		return out, nil
	}

	return 0, nil
}

func (m *mockDeliveryRepository) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
	if m.cleanupFn != nil {
		out, err := m.cleanupFn(ctx, olderThan)
		if err != nil {
			return out, fmt.Errorf("cleanup fn: %w", err)
		}

		return out, nil
	}

	return 0, nil
}

// mockSender: MessageSender mock 구현.
type mockSender struct {
	sendFn                func(ctx context.Context, roomID, message string) error
	sendWithClientRequest func(ctx context.Context, roomID, message, clientRequestID string) error
}

func (m *mockSender) SendMessage(ctx context.Context, roomID, message string) error {
	if m.sendFn != nil {
		if err := m.sendFn(ctx, roomID, message); err != nil {
			return fmt.Errorf("send fn: %w", err)
		}

		return nil
	}

	return nil
}

func (m *mockSender) SendMessageWithClientRequestID(ctx context.Context, roomID, message, clientRequestID string) error {
	if m.sendWithClientRequest != nil {
		if err := m.sendWithClientRequest(ctx, roomID, message, clientRequestID); err != nil {
			return fmt.Errorf("send with client request: %w", err)
		}

		return nil
	}

	if err := m.SendMessage(ctx, roomID, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func makePayload(t *testing.T, msg string) string {
	t.Helper()

	b, err := jsonv2.Marshal(deliverypayload.Payload{Message: msg})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	return string(b)
}

func dispatcherLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
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
	dispatcher := mustNewDispatcher(t, repository, sender, dispatcherLogger(), &DispatcherConfig{})
	item := &domain.NotificationDeliveryOutbox{
		ID:        42,
		Kind:      domain.DeliveryKindMemberNewsWeekly,
		ContentID: "member-news-2026w20",
		RoomID:    testRoomID,
		Payload:   makePayload(t, "hello"),
	}

	dispatcher.processItem(t.Context(), item)
	dispatcher.processItem(t.Context(), item)

	otherRoom := *item

	otherRoom.RoomID = "room-2"
	dispatcher.processItem(t.Context(), &otherRoom)

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
	var (
		mu        sync.Mutex
		sentIDs   []int64
		sentRooms []string
	)

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 1, RoomID: testRoomA, Payload: makePayload(t, "hello-a")},
				{ID: 2, RoomID: "room-b", Payload: makePayload(t, "hello-b")},
			}, nil
		},
		markSentFn: func(_ context.Context, id int64, _ string, _ int) error {
			mu.Lock()
			defer mu.Unlock()

			sentIDs = append(sentIDs, id)

			return nil
		},
		countFailedFn: func(context.Context) (int64, error) {
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

	defaultCfg := testDispatcherConfig()

	d := mustNewDispatcher(t, repository, sender, dispatcherLogger(), &defaultCfg)
	d.processReady(t.Context())

	if len(sentIDs) != 2 {
		t.Fatalf("expected 2 items marked sent, got %d", len(sentIDs))
	}

	if len(sentRooms) != 2 {
		t.Fatalf("expected 2 rooms sent, got %d", len(sentRooms))
	}
}

func TestProcessOnce_UnmarshalFailure_MarkFailed(t *testing.T) {
	var (
		failedID  int64
		failedMsg string
	)

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 10, RoomID: "room-x", Payload: "invalid-json{{{"},
			}, nil
		},
		markFailedFn: func(_ context.Context, id int64, _ string, _, _ int, _ time.Duration, errMsg string) error {
			failedID = id
			failedMsg = errMsg

			return nil
		},
		countFailedFn: func(context.Context) (int64, error) {
			return 0, nil
		},
		cleanupFn: func(_ context.Context, _ time.Duration) (int64, error) {
			return 0, nil
		},
	}

	sender := &mockSender{}

	defaultCfg := testDispatcherConfig()

	d := mustNewDispatcher(t, repository, sender, dispatcherLogger(), &defaultCfg)
	d.processReady(t.Context())

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
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 20, RoomID: "room-y", Payload: makePayload(t, "hello")},
			}, nil
		},
		markFailedFn: func(_ context.Context, id int64, _ string, _, _ int, _ time.Duration, _ string) error {
			failedID = id
			return nil
		},
		countFailedFn: func(context.Context) (int64, error) {
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

	defaultCfg := testDispatcherConfig()

	d := mustNewDispatcher(t, repository, sender, dispatcherLogger(), &defaultCfg)
	d.processReady(t.Context())

	if failedID != 20 {
		t.Fatalf("expected failed ID=20, got %d", failedID)
	}
}

func TestProcessItem_MarkSendingFenceSkipsSend(t *testing.T) {
	var (
		sendCalled       bool
		markSentCalled   bool
		markFailedCalled bool
	)

	repository := &mockDeliveryRepository{
		markSendingFn: func(_ context.Context, id int64, _ string, _ int, _ time.Duration) error {
			return fenceMiss(actionMarkSending, id, sendfence.ReasonLeaseExpired, domain.DeliveryStatusPending)
		},
		markSentFn: func(context.Context, int64, string, int) error {
			markSentCalled = true
			return nil
		},
		markFailedFn: func(context.Context, int64, string, int, int, time.Duration, string) error {
			markFailedCalled = true
			return nil
		},
	}
	sender := &mockSender{
		sendFn: func(_ context.Context, _, _ string) error {
			sendCalled = true
			return nil
		},
	}
	dispatcher := mustNewDispatcher(t, repository, sender, dispatcherLogger(), &DispatcherConfig{})
	item := &domain.NotificationDeliveryOutbox{ID: 42, RoomID: testRoomID, Payload: makePayload(t, "hello")}

	dispatcher.processItem(t.Context(), item)

	if sendCalled {
		t.Fatal("sender must not be called when MarkSending fence fails")
	}

	if markSentCalled {
		t.Fatal("MarkSent must not be called when MarkSending fence fails")
	}

	if markFailedCalled {
		t.Fatal("MarkFailed must not be called when MarkSending fence fails")
	}
}

func TestDispatcher_ContextCancel_JoinsLoops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var fetchCount atomic.Int32

		repository := &mockDeliveryRepository{
			fetchAndLockFn: func(_ context.Context, _ string, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
				fetchCount.Add(1)

				return nil, nil
			},
		}

		sender := &mockSender{}

		config := testDispatcherConfig()

		config.PollInterval = 10 * time.Millisecond

		ctx, cancel := context.WithCancel(t.Context())
		d := mustNewDispatcher(t, repository, sender, dispatcherLogger(), &config)
		done := make(chan error, 1)

		go func() { done <- d.Run(ctx) }()

		// 초기 실행 + ticker 몇 회 대기
		synctest.Sleep(50 * time.Millisecond)
		cancel()

		// Run은 claim loop와 유지보수 loop를 모두 join한 뒤에만 반환한다.
		if err := <-done; err != nil {
			t.Fatalf("Run() error = %v", err)
		}

		countAfterCancel := fetchCount.Load()

		synctest.Sleep(30 * time.Millisecond)

		if countFinal := fetchCount.Load(); countFinal != countAfterCancel {
			t.Fatalf("goroutine leaked: count grew from %d to %d after Run returned", countAfterCancel, countFinal)
		}

		if countAfterCancel == 0 {
			t.Fatal("expected at least 1 fetch call")
		}
	})
}

func TestDispatcher_RunFetchesOnPeriodicTickAndStopsOnCancel(t *testing.T) {
	var fetchCount atomic.Int32

	firstFetch := make(chan struct{})
	secondFetch := make(chan struct{})

	var (
		closeFirstFetch  sync.Once
		closeSecondFetch sync.Once
	)

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
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

	config := testDispatcherConfig()

	config.PollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	d := mustNewDispatcher(t, repository, &mockSender{}, dispatcherLogger(), &config)
	done := make(chan struct{})

	go func() {
		if err := d.Run(ctx); err != nil {
			t.Errorf("Run() error = %v", err)
		}

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

func TestDispatcher_RunDoesNotWarnWhenContextCanceled(t *testing.T) {
	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	config := testDispatcherConfig()

	config.PollInterval = time.Hour

	ctx, cancel := context.WithCancel(t.Context())
	d := mustNewDispatcher(t, &mockDeliveryRepository{}, &mockSender{}, logger, &config)
	done := make(chan struct{})

	go func() {
		if err := d.Run(ctx); err != nil {
			t.Errorf("Run() error = %v", err)
		}

		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("dispatcher run loop did not stop after context cancellation")
	}

	if strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("canceled ticker logged a warning: %s", logs.String())
	}
}

func TestDispatcher_RunProcessesOnceBeforeFirstTick(t *testing.T) {
	var fetchCount atomic.Int32

	firstFetch := make(chan struct{})

	var closeFirstFetch sync.Once

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			fetchCount.Add(1)
			closeFirstFetch.Do(func() {
				close(firstFetch)
			})

			return nil, nil
		},
	}

	config := testDispatcherConfig()

	config.PollInterval = time.Hour

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d := mustNewDispatcher(t, repository, &mockSender{}, dispatcherLogger(), &config)
	done := make(chan error, 1)

	go func() { done <- d.Run(ctx) }()

	select {
	case <-firstFetch:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("dispatcher did not process before first ticker interval")
	}

	cancel()

	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := fetchCount.Load(); got != 1 {
		t.Fatalf("fetch count = %d, want 1 before first ticker interval", got)
	}
}

func TestProcessOnce_RespectsMaxConcurrent(t *testing.T) {
	var (
		current    atomic.Int32
		maxRunning atomic.Int32
		sentCount  atomic.Int32
	)

	repository := &mockDeliveryRepository{
		fetchAndLockFn: func(_ context.Context, _ string, _ int, _ time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
			return []domain.NotificationDeliveryOutbox{
				{ID: 1, RoomID: testRoomA, Payload: makePayload(t, "hello-a")},
				{ID: 2, RoomID: "room-b", Payload: makePayload(t, "hello-b")},
				{ID: 3, RoomID: "room-c", Payload: makePayload(t, "hello-c")},
				{ID: 4, RoomID: "room-d", Payload: makePayload(t, "hello-d")},
			}, nil
		},
		markSentFn: func(context.Context, int64, string, int) error {
			sentCount.Add(1)

			return nil
		},
		countFailedFn: func(context.Context) (int64, error) {
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

	config := testDispatcherConfig()

	config.MaxConcurrent = 2

	d := mustNewDispatcher(t, repository, sender, dispatcherLogger(), &config)
	d.processReady(t.Context())

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

func TestProcessOncePreservesOrderWithinRoom(t *testing.T) {
	t.Parallel()

	firstStarted := make(chan struct{})
	otherRoomStarted := make(chan struct{})
	releaseFirst := make(chan struct{})

	var reordered atomic.Bool

	sender := &mockSender{sendFn: func(_ context.Context, _, message string) error {
		switch message {
		case testFirstMessage:
			close(firstStarted)
			<-releaseFirst
		case "second":
			select {
			case <-releaseFirst:
			default:
				reordered.Store(true)
			}
		case "other":
			close(otherRoomStarted)
		}

		return nil
	}}
	items := []domain.NotificationDeliveryOutbox{
		{ID: 1, RoomID: testRoomA, Payload: makePayload(t, testFirstMessage)},
		{ID: 2, RoomID: testRoomA, Payload: makePayload(t, "second")},
		{ID: 3, RoomID: "room-b", Payload: makePayload(t, "other")},
	}
	repo := &mockDeliveryRepository{fetchAndLockFn: func(context.Context, string, int, time.Duration) ([]domain.NotificationDeliveryOutbox, error) {
		return items, nil
	}}
	dispatcher := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &DispatcherConfig{MaxConcurrent: 2})
	done := make(chan struct{})

	go func() {
		dispatcher.processReady(t.Context())
		close(done)
	}()

	<-firstStarted

	select {
	case <-otherRoomStarted:
	case <-time.After(time.Second):
		t.Fatal("different room did not run concurrently")
	}

	close(releaseFirst)
	<-done

	if reordered.Load() {
		t.Fatal("second notification for a room started before the first completed")
	}
}

func (m *mockSender) PrepareMessageRequest(_ context.Context, _, body string) (string, string, error) {
	return body, "text", nil
}

func (m *mockSender) SendPreparedMessage(ctx context.Context, room, body, _, id string) (string, error) {
	return "", m.SendMessageWithClientRequestID(ctx, room, body, id)
}
