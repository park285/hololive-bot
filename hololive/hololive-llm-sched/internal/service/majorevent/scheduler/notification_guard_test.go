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
	"fmt"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
)

// --- mock OutboxRepository (enqueue 기록용) ---

type mockOutboxRepo struct {
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

// --- mock NotificationLocker ---

type mockNotificationLocker struct {
	acquireToken    string
	acquireAcquired bool
	acquireErr      error

	releaseCalls []string
}

func (m *mockNotificationLocker) TryAcquire(_ context.Context, _ string, _ time.Duration) (string, bool, error) {
	return m.acquireToken, m.acquireAcquired, m.acquireErr
}

func (m *mockNotificationLocker) Release(_ context.Context, lockKey, _ string) error {
	m.releaseCalls = append(m.releaseCalls, lockKey)
	return nil
}

func (m *mockNotificationLocker) ClaimRoom(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (m *mockNotificationLocker) ReleaseRoomClaims(_ context.Context, _ []string) error {
	return nil
}

var testLogger = sharedlogging.NewLogger

// === enqueueToRooms 단위 테스트 ===

func TestEnqueueToRooms_AllSuccess(t *testing.T) {
	repo := newMockOutboxRepo()
	rooms := []roomTarget{{roomID: "room1"}, {roomID: "room2"}, {roomID: "room3"}}

	result := enqueueToRooms(context.Background(), repo, rooms, domain.DeliveryKindMajorEventWeekly, "2026-01-24", "msg", testLogger())

	if result.Sent != 3 {
		t.Errorf("expected 3 sent, got %d", result.Sent)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", result.Failed)
	}
	if result.Attempted != 3 {
		t.Errorf("expected 3 attempted, got %d", result.Attempted)
	}
	if len(repo.enqueuedItems) != 3 {
		t.Errorf("expected 3 enqueued items, got %d", len(repo.enqueuedItems))
	}
}

func TestEnqueueToRooms_PartialFailure(t *testing.T) {
	repo := newMockOutboxRepo()
	repo.enqueueErr["room2"] = fmt.Errorf("db error")
	rooms := []roomTarget{{roomID: "room1"}, {roomID: "room2"}, {roomID: "room3"}}

	result := enqueueToRooms(context.Background(), repo, rooms, domain.DeliveryKindMajorEventWeekly, "2026-01-24", "msg", testLogger())

	if result.Sent != 2 {
		t.Errorf("expected 2 sent, got %d", result.Sent)
	}
	if result.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", result.Failed)
	}
	if len(result.FailedRooms) != 1 || result.FailedRooms[0] != "room2" {
		t.Errorf("expected FailedRooms=[room2], got %v", result.FailedRooms)
	}
}

func TestEnqueueToRooms_AllFail(t *testing.T) {
	repo := newMockOutboxRepo()
	repo.enqueueErr["room1"] = fmt.Errorf("db error")
	repo.enqueueErr["room2"] = fmt.Errorf("db error")
	rooms := []roomTarget{{roomID: "room1"}, {roomID: "room2"}}

	result := enqueueToRooms(context.Background(), repo, rooms, domain.DeliveryKindMajorEventWeekly, "2026-01-24", "msg", testLogger())

	if result.Failed != 2 {
		t.Errorf("expected 2 failed, got %d", result.Failed)
	}
	if result.Sent != 0 {
		t.Errorf("expected 0 sent, got %d", result.Sent)
	}
}

func TestEnqueueToRooms_VerifiesKindAndPeriodKey(t *testing.T) {
	repo := newMockOutboxRepo()
	rooms := []roomTarget{{roomID: "room1"}}

	enqueueToRooms(context.Background(), repo, rooms, domain.DeliveryKindMajorEventMonthly, "2026-02", "test msg", testLogger())

	if len(repo.enqueuedItems) != 1 {
		t.Fatalf("expected 1 item, got %d", len(repo.enqueuedItems))
	}
	item := repo.enqueuedItems[0]
	if item.Kind != domain.DeliveryKindMajorEventMonthly {
		t.Errorf("expected kind %s, got %s", domain.DeliveryKindMajorEventMonthly, item.Kind)
	}
	if item.PeriodKey != "2026-02" {
		t.Errorf("expected periodKey 2026-02, got %s", item.PeriodKey)
	}
	if item.Message != "test msg" {
		t.Errorf("expected message 'test msg', got %s", item.Message)
	}
}
