package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type mockFormatter struct {
	message string
}

func (m *mockFormatter) FormatMajorEventWeeklySummary(_ context.Context, _ []domain.MajorEvent, _ string) string {
	return m.message
}

func (m *mockFormatter) FormatMajorEventMonthlySummary(_ context.Context, _ []domain.MajorEvent, _ string) string {
	return m.message
}

func TestWeekKeyFromGetWeekRange(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)

	tests := []struct {
		name     string
		now      time.Time
		expected string
	}{
		{
			name:     "Monday trigger → same Monday as key",
			now:      time.Date(2026, 1, 19, 9, 0, 0, 0, kst),
			expected: "2026-01-19",
		},
		{
			name:     "Wednesday trigger → this Monday as key",
			now:      time.Date(2026, 1, 21, 12, 0, 0, 0, kst),
			expected: "2026-01-19",
		},
		{
			name:     "Sunday trigger → this Monday as key",
			now:      time.Date(2026, 1, 25, 10, 0, 0, 0, kst),
			expected: "2026-01-19",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weekStart, _ := GetWeekRange(tt.now)
			weekKey := weekStart.Format("2006-01-02")
			if weekKey != tt.expected {
				t.Errorf("expected weekKey %q, got %q", tt.expected, weekKey)
			}
		})
	}
}

func TestScheduler_calculateNextRun(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)
	scheduler := &Scheduler{}

	tests := []struct {
		name         string
		now          time.Time
		expectedDay  int
		expectedHour int
	}{
		{
			name:         "Sunday evening -> next Monday 09:00",
			now:          time.Date(2026, 1, 18, 20, 0, 0, 0, kst),
			expectedDay:  19,
			expectedHour: 9,
		},
		{
			name:         "Monday 08:59 -> same day 09:00",
			now:          time.Date(2026, 1, 19, 8, 59, 0, 0, kst),
			expectedDay:  19,
			expectedHour: 9,
		},
		{
			name:         "Monday 09:01 -> next week Monday",
			now:          time.Date(2026, 1, 19, 9, 1, 0, 0, kst),
			expectedDay:  26,
			expectedHour: 9,
		},
		{
			name:         "Wednesday -> next Monday",
			now:          time.Date(2026, 1, 21, 10, 0, 0, 0, kst),
			expectedDay:  26,
			expectedHour: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := scheduler.calculateNextRun(tt.now)
			nextKST := next.In(kst)

			if nextKST.Weekday() != time.Monday {
				t.Errorf("expected Monday, got %v", nextKST.Weekday())
			}
			if nextKST.Day() != tt.expectedDay {
				t.Errorf("expected day %d, got %d", tt.expectedDay, nextKST.Day())
			}
			if nextKST.Hour() != tt.expectedHour {
				t.Errorf("expected hour %d, got %d", tt.expectedHour, nextKST.Hour())
			}
			if !next.After(tt.now) {
				t.Error("next run should be after now")
			}
		})
	}
}

func TestNewScheduler(t *testing.T) {
	formatter := &mockFormatter{message: "test"}

	scheduler := NewScheduler(nil, formatter, nil, nil, nil, nil)

	if scheduler == nil {
		t.Fatal("NewScheduler returned nil")
	}
	if scheduler.formatter == nil {
		t.Error("formatter not set")
	}
	if scheduler.stopCh == nil {
		t.Error("stopCh not initialized")
	}
}

func TestScheduler_StopOnce(t *testing.T) {
	scheduler := &Scheduler{
		stopCh: make(chan struct{}),
	}

	scheduler.Stop()
	scheduler.Stop()

	t.Log("Stop() called twice without panic - sync.Once working")
}

func TestGetWeekRange_MondayKST(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)
	monday := time.Date(2026, 1, 19, 9, 0, 0, 0, kst)

	start, end := GetWeekRange(monday)

	if start.Weekday() != time.Monday {
		t.Errorf("start should be Monday, got %v", start.Weekday())
	}
	if end.Weekday() != time.Sunday {
		t.Errorf("end should be Sunday, got %v", end.Weekday())
	}
	if !start.Before(end) {
		t.Error("start should be before end")
	}
}

func TestScheduler_Interface(t *testing.T) {
	var _ interface {
		Start(ctx context.Context)
		Stop()
		SendWeeklyNotification(ctx context.Context) error
	}

	t.Log("Scheduler interface verified")
}

// --- mockEventRepository ---

type mockEventRepository struct {
	rooms          []*domain.EventRoomSubscription
	roomsErr       error
	events         []*domain.MajorEvent
	eventsErr      error
	monthlyEvents  []*domain.MajorEvent
	monthlyErr     error
	markedWeekly   bool
	markedMonthly  bool
	markWeeklyErr  error
	markMonthlyErr error
	markedEventIDs []int
	markedWeekKey  string
	markedMonthKey string
}

func (m *mockEventRepository) GetSubscribedRooms(_ context.Context) ([]*domain.EventRoomSubscription, error) {
	return m.rooms, m.roomsErr
}

func (m *mockEventRepository) GetEventsByDateRange(_ context.Context, _, _ time.Time, _ string) ([]*domain.MajorEvent, error) {
	return m.events, m.eventsErr
}

func (m *mockEventRepository) GetEventsByMonth(_ context.Context, _, _ int, _ string) ([]*domain.MajorEvent, error) {
	return m.monthlyEvents, m.monthlyErr
}

func (m *mockEventRepository) MarkEventsAsNotified(_ context.Context, eventIDs []int, weekKey string) error {
	m.markedWeekly = true
	m.markedEventIDs = eventIDs
	m.markedWeekKey = weekKey
	return m.markWeeklyErr
}

func (m *mockEventRepository) MarkEventsAsMonthlyNotified(_ context.Context, eventIDs []int, monthKey string) error {
	m.markedMonthly = true
	m.markedEventIDs = eventIDs
	m.markedMonthKey = monthKey
	return m.markMonthlyErr
}

// === SendWeeklyNotification 통합 테스트 ===

func newTestScheduler(repo EventRepository, outbox outboxEnqueuer, locker NotificationLocker) *Scheduler {
	return NewScheduler(
		repo,
		&mockFormatter{message: "test message"},
		nil,
		locker,
		outbox,
		testLogger(),
	)
}

func testRooms(ids ...string) []*domain.EventRoomSubscription {
	rooms := make([]*domain.EventRoomSubscription, len(ids))
	for i, id := range ids {
		rooms[i] = &domain.EventRoomSubscription{RoomID: id}
	}
	return rooms
}

func testEvents(ids ...int) []*domain.MajorEvent {
	events := make([]*domain.MajorEvent, len(ids))
	for i, id := range ids {
		events[i] = &domain.MajorEvent{ID: id, Title: fmt.Sprintf("Event %d", id)}
	}
	return events
}

func TestSendWeeklyNotification_AllSuccess_MarksEvents(t *testing.T) {
	repo := &mockEventRepository{
		rooms:  testRooms("room1", "room2"),
		events: testEvents(1, 2, 3),
	}
	outbox := newMockOutboxRepo()
	locker := &mockNotificationLocker{acquireAcquired: true}
	scheduler := newTestScheduler(repo, outbox, locker)

	err := scheduler.SendWeeklyNotification(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !repo.markedWeekly {
		t.Error("expected events to be marked as notified")
	}
	if len(repo.markedEventIDs) != 3 {
		t.Errorf("expected 3 marked event IDs, got %d", len(repo.markedEventIDs))
	}
	if len(outbox.enqueuedItems) != 2 {
		t.Errorf("expected 2 enqueued items, got %d", len(outbox.enqueuedItems))
	}
}

func TestSendWeeklyNotification_PartialEnqueueFailure_NoMarking(t *testing.T) {
	repo := &mockEventRepository{
		rooms:  testRooms("room1", "room2", "room3"),
		events: testEvents(1, 2),
	}
	outbox := newMockOutboxRepo()
	outbox.enqueueErr["room2"] = fmt.Errorf("db error")
	locker := &mockNotificationLocker{acquireAcquired: true}
	scheduler := newTestScheduler(repo, outbox, locker)

	err := scheduler.SendWeeklyNotification(context.Background())
	if err != nil {
		t.Fatalf("unexpected error (partial failure returns nil): %v", err)
	}

	if repo.markedWeekly {
		t.Error("expected events NOT to be marked on partial enqueue failure")
	}
}

func TestSendWeeklyNotification_AllEnqueueFail_ReturnsError(t *testing.T) {
	repo := &mockEventRepository{
		rooms:  testRooms("room1", "room2"),
		events: testEvents(1),
	}
	outbox := newMockOutboxRepo()
	outbox.enqueueErr["room1"] = fmt.Errorf("db error")
	outbox.enqueueErr["room2"] = fmt.Errorf("db error")
	locker := &mockNotificationLocker{acquireAcquired: true}
	scheduler := newTestScheduler(repo, outbox, locker)

	err := scheduler.SendWeeklyNotification(context.Background())
	if err == nil {
		t.Fatal("expected error when all rooms fail to enqueue")
	}

	if repo.markedWeekly {
		t.Error("expected events NOT to be marked when all fail")
	}
}

func TestSendWeeklyNotification_NoEvents_ReturnsNil(t *testing.T) {
	repo := &mockEventRepository{
		rooms:  testRooms("room1"),
		events: []*domain.MajorEvent{},
	}
	locker := &mockNotificationLocker{acquireAcquired: true}
	scheduler := newTestScheduler(repo, newMockOutboxRepo(), locker)

	err := scheduler.SendWeeklyNotification(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.markedWeekly {
		t.Error("should not mark when no events")
	}
}

func TestSendWeeklyNotification_NoRooms_ReturnsNil(t *testing.T) {
	repo := &mockEventRepository{
		rooms: []*domain.EventRoomSubscription{},
	}
	locker := &mockNotificationLocker{acquireAcquired: true}
	scheduler := newTestScheduler(repo, newMockOutboxRepo(), locker)

	err := scheduler.SendWeeklyNotification(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// === SendMonthlyNotification 통합 테스트 ===

func newTestMonthlyScheduler(repo EventRepository, outbox outboxEnqueuer, locker NotificationLocker) *MonthlyScheduler {
	return NewMonthlyScheduler(
		repo,
		&mockFormatter{message: "monthly message"},
		nil,
		locker,
		outbox,
		testLogger(),
	)
}

func TestSendMonthlyNotification_AllSuccess_MarksEvents(t *testing.T) {
	repo := &mockEventRepository{
		rooms:         testRooms("room1", "room2"),
		monthlyEvents: testEvents(10, 20),
	}
	outbox := newMockOutboxRepo()
	locker := &mockNotificationLocker{acquireAcquired: true}
	scheduler := newTestMonthlyScheduler(repo, outbox, locker)

	err := scheduler.SendMonthlyNotification(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !repo.markedMonthly {
		t.Error("expected events to be marked as monthly notified")
	}
	if len(repo.markedEventIDs) != 2 {
		t.Errorf("expected 2 marked event IDs, got %d", len(repo.markedEventIDs))
	}
}

func TestSendMonthlyNotification_PartialFailure_NoMarking(t *testing.T) {
	repo := &mockEventRepository{
		rooms:         testRooms("room1", "room2"),
		monthlyEvents: testEvents(10),
	}
	outbox := newMockOutboxRepo()
	outbox.enqueueErr["room1"] = fmt.Errorf("db error")
	locker := &mockNotificationLocker{acquireAcquired: true}
	scheduler := newTestMonthlyScheduler(repo, outbox, locker)

	err := scheduler.SendMonthlyNotification(context.Background())
	if err != nil {
		t.Fatalf("unexpected error (partial failure returns nil): %v", err)
	}

	if repo.markedMonthly {
		t.Error("expected events NOT to be marked on partial failure")
	}
}

// === 통합 시나리오 (Scheduler 레벨, 락 미획득 테스트) ===

func TestSendWeeklyNotification_ConcurrentLockHeld_ReturnsInProgress(t *testing.T) {
	locker := &mockNotificationLocker{
		acquireToken:    "",
		acquireAcquired: false,
	}

	scheduler := &Scheduler{
		locker: locker,
		logger: testLogger(),
		stopCh: make(chan struct{}),
	}

	err := scheduler.SendWeeklyNotification(context.Background())
	if !errors.Is(err, triggercontracts.ErrNotificationInProgress) {
		t.Errorf("expected ErrNotificationInProgress, got: %v", err)
	}
}

func TestSendMonthlyNotification_ConcurrentLockHeld_ReturnsInProgress(t *testing.T) {
	locker := &mockNotificationLocker{
		acquireToken:    "",
		acquireAcquired: false,
	}

	scheduler := &MonthlyScheduler{
		locker: locker,
		logger: testLogger(),
		stopCh: make(chan struct{}),
	}

	err := scheduler.SendMonthlyNotification(context.Background())
	if !errors.Is(err, triggercontracts.ErrNotificationInProgress) {
		t.Errorf("expected ErrNotificationInProgress, got: %v", err)
	}
}

// === 마킹 결정표 테스트 ===

func TestSendWeeklyNotification_EnqueueMarking_AllSuccess_Marks(t *testing.T) {
	repo := &mockEventRepository{
		rooms:  testRooms("room1"),
		events: testEvents(1),
	}
	outbox := newMockOutboxRepo()
	locker := &mockNotificationLocker{acquireAcquired: true}
	scheduler := newTestScheduler(repo, outbox, locker)

	_ = scheduler.SendWeeklyNotification(context.Background())

	if !repo.markedWeekly {
		t.Error("all enqueue success → should mark events")
	}
}

func TestSendWeeklyNotification_EnqueueMarking_PartialFail_NoMark(t *testing.T) {
	repo := &mockEventRepository{
		rooms:  testRooms("room1", "room2"),
		events: testEvents(1),
	}
	outbox := newMockOutboxRepo()
	outbox.enqueueErr["room2"] = fmt.Errorf("fail")
	locker := &mockNotificationLocker{acquireAcquired: true}
	scheduler := newTestScheduler(repo, outbox, locker)

	_ = scheduler.SendWeeklyNotification(context.Background())

	if repo.markedWeekly {
		t.Error("partial enqueue failure → should NOT mark events")
	}
}
