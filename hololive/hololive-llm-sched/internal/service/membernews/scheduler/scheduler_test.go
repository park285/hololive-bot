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

package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type mockDigestService struct {
	rooms      []model.SubscribedRoom
	digests    map[string]*model.Digest
	digestErrs map[string]error
}

func (m *mockDigestService) GenerateRoomDigest(_ context.Context, roomID string, _ model.Period) (*model.Digest, error) {
	if err, ok := m.digestErrs[roomID]; ok && err != nil {
		return nil, err
	}
	if digest, ok := m.digests[roomID]; ok {
		return digest, nil
	}
	return &model.Digest{
		Headline: "H",
		TopItems: []model.SummaryItem{{Member: "A", Category: "event", Title: "T", DateText: "2026-02-20", Summary: "S", SourceURL: "https://hololive.hololivepro.com/news/1"}},
	}, nil
}

func (m *mockDigestService) ListSubscribedRooms(_ context.Context) ([]model.SubscribedRoom, error) {
	return m.rooms, nil
}

type mockFormatter struct{}

func (mockFormatter) FormatMemberNewsDigest(_ context.Context, digest *model.Digest) string {
	if digest == nil {
		return ""
	}
	return digest.Headline
}

// mockNotificationLocker: delivery.NotificationLocker 구현
type mockNotificationLocker struct {
	acquireToken    string
	acquireAcquired bool
	acquireErr      error

	mu           sync.Mutex
	releaseCalls []string
}

func (m *mockNotificationLocker) TryAcquire(_ context.Context, _ string, _ time.Duration) (string, bool, error) {
	return m.acquireToken, m.acquireAcquired, m.acquireErr
}

func (m *mockNotificationLocker) Release(_ context.Context, lockKey, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseCalls = append(m.releaseCalls, lockKey)
	return nil
}

func (m *mockNotificationLocker) ClaimRoom(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (m *mockNotificationLocker) ReleaseRoomClaims(_ context.Context, _ []string) error {
	return nil
}

// mockOutboxRepo: outboxEnqueuer 구현
type mockOutboxRepo struct {
	mu            sync.Mutex
	enqueuedItems []enqueueRecord
	enqueueErr    map[string]error // roomID → error
}

type enqueueRecord struct {
	Kind      domain.DeliveryOutboxKind
	PeriodKey string
	RoomID    string
	Message   string
}

func newMockOutboxRepo() *mockOutboxRepo {
	return &mockOutboxRepo{
		enqueueErr: make(map[string]error),
	}
}

func (m *mockOutboxRepo) Enqueue(_ context.Context, kind domain.DeliveryOutboxKind, periodKey, roomID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.enqueueErr[roomID]; ok {
		return err
	}
	m.enqueuedItems = append(m.enqueuedItems, enqueueRecord{
		Kind:      kind,
		PeriodKey: periodKey,
		RoomID:    roomID,
		Message:   message,
	})
	return nil
}

func TestScheduler_LockAlreadyHeldSkipsExecution(t *testing.T) {
	service := &mockDigestService{rooms: []model.SubscribedRoom{{RoomID: "room-1"}}}
	locker := &mockNotificationLocker{acquireAcquired: false}
	outbox := newMockOutboxRepo()
	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)

	scheduler := NewScheduler(service, mockFormatter{}, locker, outbox, nil)
	scheduler.SetClock(func() time.Time { return now })

	if err := scheduler.SendWeeklyDigest(context.Background()); err != nil {
		t.Fatalf("expected no error when lock held, got %v", err)
	}
	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("expected no enqueued items, got %d", len(outbox.enqueuedItems))
	}
}

func TestScheduler_EnqueueSuccessForAllRooms(t *testing.T) {
	service := &mockDigestService{rooms: []model.SubscribedRoom{{RoomID: "room-1"}, {RoomID: "room-2"}}}
	locker := &mockNotificationLocker{acquireToken: "tok", acquireAcquired: true}
	outbox := newMockOutboxRepo()
	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)

	scheduler := NewScheduler(service, mockFormatter{}, locker, outbox, nil)
	scheduler.SetClock(func() time.Time { return now })

	if err := scheduler.SendWeeklyDigest(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(outbox.enqueuedItems) != 2 {
		t.Errorf("expected 2 enqueued items, got %d", len(outbox.enqueuedItems))
	}
}

func TestScheduler_AllEnqueueFailureReturnsError(t *testing.T) {
	service := &mockDigestService{rooms: []model.SubscribedRoom{{RoomID: "room-1"}}}
	locker := &mockNotificationLocker{acquireToken: "tok", acquireAcquired: true}
	outbox := newMockOutboxRepo()
	outbox.enqueueErr["room-1"] = errors.New("db error")
	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)

	scheduler := NewScheduler(service, mockFormatter{}, locker, outbox, nil)
	scheduler.SetClock(func() time.Time { return now })

	if err := scheduler.SendWeeklyDigest(context.Background()); err == nil {
		t.Fatalf("expected error when all rooms fail to enqueue")
	}
}

func TestScheduler_CalculateNextRunMonday0900KST(t *testing.T) {
	scheduler := NewScheduler(nil, nil, nil, nil, nil)

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before monday target same day",
			now:  time.Date(2026, 2, 16, 8, 30, 0, 0, model.KST), // Monday
			want: time.Date(2026, 2, 16, 9, 0, 0, 0, model.KST),
		},
		{
			name: "exact monday target next week",
			now:  time.Date(2026, 2, 16, 9, 0, 0, 0, model.KST),
			want: time.Date(2026, 2, 23, 9, 0, 0, 0, model.KST),
		},
		{
			name: "sunday moves next day monday",
			now:  time.Date(2026, 2, 15, 23, 0, 0, 0, model.KST), // Sunday
			want: time.Date(2026, 2, 16, 9, 0, 0, 0, model.KST),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scheduler.calculateNextRun(tt.now)
			if !got.Equal(tt.want) {
				t.Fatalf("calculateNextRun(%s) = %s, want %s", tt.now, got, tt.want)
			}
		})
	}
}

func TestScheduler_PartialEnqueueFailure(t *testing.T) {
	service := &mockDigestService{
		rooms: []model.SubscribedRoom{
			{RoomID: "room-fail"},
			{RoomID: "room-ok"},
		},
	}
	locker := &mockNotificationLocker{acquireToken: "tok", acquireAcquired: true}
	outbox := newMockOutboxRepo()
	outbox.enqueueErr["room-fail"] = errors.New("db error")
	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)

	scheduler := NewScheduler(service, mockFormatter{}, locker, outbox, nil)
	scheduler.SetClock(func() time.Time { return now })

	if err := scheduler.SendWeeklyDigest(context.Background()); err != nil {
		t.Fatalf("expected no error for partial failure with at least one success, got %v", err)
	}
	if len(outbox.enqueuedItems) != 1 {
		t.Errorf("expected 1 enqueued item, got %d", len(outbox.enqueuedItems))
	}
}

func TestScheduler_NoMembersSkipCountsAsSkipped(t *testing.T) {
	service := &mockDigestService{
		rooms: []model.SubscribedRoom{{RoomID: "room-no-members"}},
		digestErrs: map[string]error{
			"room-no-members": model.ErrNoSubscribedMembers,
		},
	}
	locker := &mockNotificationLocker{acquireToken: "tok", acquireAcquired: true}
	outbox := newMockOutboxRepo()
	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)

	scheduler := NewScheduler(service, mockFormatter{}, locker, outbox, nil)
	scheduler.SetClock(func() time.Time { return now })

	if err := scheduler.SendWeeklyDigest(context.Background()); err != nil {
		t.Fatalf("expected no error for all-skip(no members), got %v", err)
	}
	if len(outbox.enqueuedItems) != 0 {
		t.Errorf("expected no enqueued items for skip, got %d", len(outbox.enqueuedItems))
	}
}

func TestScheduler_LockReleasedOnCompletion(t *testing.T) {
	service := &mockDigestService{rooms: []model.SubscribedRoom{{RoomID: "room-1"}}}
	locker := &mockNotificationLocker{acquireToken: "tok-1", acquireAcquired: true}
	outbox := newMockOutboxRepo()
	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)

	scheduler := NewScheduler(service, mockFormatter{}, locker, outbox, nil)
	scheduler.SetClock(func() time.Time { return now })

	_ = scheduler.SendWeeklyDigest(context.Background())

	if len(locker.releaseCalls) != 1 {
		t.Errorf("expected 1 Release call, got %d", len(locker.releaseCalls))
	}
}

func TestScheduler_LockAcquireGracefulDegradation(t *testing.T) {
	// Graceful degradation: locker는 Valkey 장애 시 (token, true, nil) 반환.
	// 스케줄러는 정상 진행.
	service := &mockDigestService{rooms: []model.SubscribedRoom{{RoomID: "room-1"}}}
	locker := &mockNotificationLocker{acquireToken: "degraded", acquireAcquired: true}
	outbox := newMockOutboxRepo()
	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)

	scheduler := NewScheduler(service, mockFormatter{}, locker, outbox, nil)
	scheduler.SetClock(func() time.Time { return now })

	if err := scheduler.SendWeeklyDigest(context.Background()); err != nil {
		t.Fatalf("expected no error with graceful degradation, got %v", err)
	}
	if len(outbox.enqueuedItems) != 1 {
		t.Errorf("expected 1 enqueued item, got %d", len(outbox.enqueuedItems))
	}
}
