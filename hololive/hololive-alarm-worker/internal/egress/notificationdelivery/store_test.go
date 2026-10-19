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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

const (
	testWorkerA = "test-worker-A"
	testWorkerB = "test-worker-B"
	testLease   = 60 * time.Second
)

// outboxFixture는 운영과 같은 두 저장소(shared producer 적재 + worker Store)를 한 DB에 묶어 경계 계약을 검증한다.
const deliveryStatusQuarantined domain.DeliveryOutboxStatus = "QUARANTINED"

type outboxFixture struct {
	*Store
	*delivery.OutboxRepository
}

func testRepository(t *testing.T) *outboxFixture {
	t.Helper()

	pool := dbtest.NewPool(t)

	return &outboxFixture{
		Store:            NewStore(pool),
		OutboxRepository: delivery.NewOutboxRepositoryFromPool(pool, slog.New(slog.DiscardHandler)),
	}
}

func buildOutboxBatchItems(count int) []delivery.OutboxItem {
	items := make([]delivery.OutboxItem, 0, count)
	for i := range count {
		items = append(items, delivery.OutboxItem{
			Kind:      domain.DeliveryKindMemberNewsWeekly,
			PeriodKey: "2026-W08",
			RoomID:    fmt.Sprintf("room-batch-%d", i),
			Message:   fmt.Sprintf("batch-msg-%d", i),
		})
	}

	return items
}

func claimItems(ctx context.Context, t *testing.T, repository *outboxFixture, workerID string, limit int) []domain.NotificationDeliveryOutbox {
	t.Helper()

	items, err := repository.fetchReadyAndLock(ctx, workerID, limit, testLease, nil, nil)
	if err != nil {
		t.Fatalf("fetch ready and lock: %v", err)
	}

	return items
}

func fetchAndLockItems(ctx context.Context, t *testing.T, repository *outboxFixture) []domain.NotificationDeliveryOutbox {
	t.Helper()

	return claimItems(ctx, t, repository, testWorkerA, 1)
}

func markOutboxSending(ctx context.Context, t *testing.T, repository *outboxFixture, item *domain.NotificationDeliveryOutbox) {
	t.Helper()

	if err := repository.MarkSending(ctx, item.ID, testWorkerA, item.AttemptCount, testLease); err != nil {
		t.Fatalf("mark sending: %v", err)
	}
}

// requireFenceMiss는 err가 action 전이의 단일 행 fence 미스이고 관측 원인이 reason인지 확인한다.
func requireFenceMiss(t *testing.T, err error, action string, reason sendfence.Reason) {
	t.Helper()

	miss, ok := errors.AsType[*sendfence.MissError](err)
	if !ok {
		t.Fatalf("%s error = %v, want *sendfence.MissError", action, err)
	}

	if miss.Action != action || len(miss.Rows) != 1 || miss.Rows[0].Reason != reason {
		t.Fatalf("%s miss = %+v, want one row with reason %s", action, miss, reason)
	}
}

func countByStatus(ctx context.Context, t *testing.T, repository *outboxFixture, status domain.DeliveryOutboxStatus) int64 {
	t.Helper()

	var count int64

	err := repository.pool.QueryRow(ctx, "SELECT count(id) FROM notification_delivery_outbox WHERE status = $1", status).Scan(&count)
	if err != nil {
		t.Fatalf("count by status %s: %v", status, err)
	}

	return count
}

func TestFetchReadyAndLockClaimsDistinctRooms(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	for i := range 3 {
		if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room"+string(rune('a'+i)), "msg"); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	items := claimItems(ctx, t, repository, testWorkerA, 2)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// locked_at이 설정되어야 함
	for _, item := range items {
		if !item.LockedAt.Valid {
			t.Fatalf("expected locked_at to be set for item %d", item.ID)
		}
	}
}

func TestFetchReadyAndLock_OrdersDueBeforeCreatedAt(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "older-created", "msg"); err != nil {
		t.Fatalf("enqueue older-created: %v", err)
	}

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "earlier-due", "msg"); err != nil {
		t.Fatalf("enqueue earlier-due: %v", err)
	}

	now := time.Now()
	if _, err := repository.pool.Exec(ctx, `
		UPDATE notification_delivery_outbox
		SET created_at = $1, next_attempt_at = $2
		WHERE room_id = 'older-created'
	`, now.Add(-2*time.Hour), now.Add(-5*time.Minute)); err != nil {
		t.Fatalf("shape older-created due fixture: %v", err)
	}

	if _, err := repository.pool.Exec(ctx, `
		UPDATE notification_delivery_outbox
		SET created_at = $1, next_attempt_at = $2
		WHERE room_id = 'earlier-due'
	`, now.Add(-1*time.Hour), now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("shape earlier-due fixture: %v", err)
	}

	items := claimItems(ctx, t, repository, testWorkerA, 1)
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}

	if items[0].RoomID != "earlier-due" {
		t.Fatalf("claimed room = %q, want earlier-due", items[0].RoomID)
	}
}

func TestMarkSent_RejectsOwnedPendingRow(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(ctx, t, repository)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	requireFenceMiss(t, repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].AttemptCount), actionMarkSent, sendfence.ReasonStatusChanged)

	var (
		status        domain.DeliveryOutboxStatus
		lockedBy      *string
		lockExpiresAt *time.Time
		sentAt        *time.Time
	)

	if scanErr := repository.pool.QueryRow(ctx,
		"SELECT status, locked_by, lock_expires_at, sent_at FROM notification_delivery_outbox WHERE id = $1",
		items[0].ID,
	).Scan(&status, &lockedBy, &lockExpiresAt, &sentAt); scanErr != nil {
		t.Fatalf("load row: %v", scanErr)
	}

	if status != domain.DeliveryStatusPending {
		t.Fatalf("status = %s, want PENDING", status)
	}

	if lockedBy == nil || *lockedBy != testWorkerA || lockExpiresAt == nil {
		t.Fatalf("claim lease must stay with worker A, locked_by=%v lock_expires_at=%v", lockedBy, lockExpiresAt)
	}

	if sentAt != nil {
		t.Fatalf("sent_at = %s, want NULL", *sentAt)
	}

	markOutboxSending(ctx, t, repository, &items[0])

	if err := repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].AttemptCount); err != nil {
		t.Fatalf("the same owner must settle after entering SENDING: %v", err)
	}
}

func TestMarkSent_FromSending(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(ctx, t, repository)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	markOutboxSending(ctx, t, repository, &items[0])

	if err := repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].AttemptCount); err != nil {
		t.Fatalf("MarkSent must accept SENDING owner transition: %v", err)
	}

	if sent := countByStatus(ctx, t, repository, domain.DeliveryStatusSent); sent != 1 {
		t.Fatalf("expected 1 sent, got %d", sent)
	}

	if sending := countByStatus(ctx, t, repository, deliveryStatusSending); sending != 0 {
		t.Fatalf("expected 0 sending after sent, got %d", sending)
	}
}

func TestMarkSent_ClearsLock(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(ctx, t, repository)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	if !items[0].LockedAt.Valid {
		t.Fatal("expected locked_at set after claim")
	}

	markOutboxSending(ctx, t, repository, &items[0])

	if err := repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].AttemptCount); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	var clearedLocks int

	if err := repository.pool.QueryRow(ctx,
		"SELECT count(*) FROM notification_delivery_outbox WHERE id = $1 AND locked_at IS NULL",
		items[0].ID,
	).Scan(&clearedLocks); err != nil {
		t.Fatalf("query locked_at: %v", err)
	}

	if clearedLocks != 1 {
		t.Fatalf("MarkSent must clear locked_at, got %d cleared", clearedLocks)
	}
}

func TestMarkSent_DoesNotResurrectFailedRow(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(ctx, t, repository)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	id := items[0].ID

	if _, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET status = 'FAILED', locked_at = NULL WHERE id = $1", id,
	); err != nil {
		t.Fatalf("force failed: %v", err)
	}

	requireFenceMiss(t, repository.MarkSent(ctx, id, testWorkerA, items[0].AttemptCount), actionMarkSent, sendfence.ReasonStatusChanged)

	if sent := countByStatus(ctx, t, repository, domain.DeliveryStatusSent); sent != 0 {
		t.Fatalf("late MarkSent must not resurrect a FAILED row to SENT, sent=%d", sent)
	}

	if failed := countByStatus(ctx, t, repository, domain.DeliveryStatusFailed); failed != 1 {
		t.Fatalf("row must remain FAILED, failed=%d", failed)
	}
}

func TestMarkFailed_DoesNotResurrectSentRow(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(ctx, t, repository)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	id := items[0].ID

	if _, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET status = 'SENT', locked_at = NULL WHERE id = $1", id,
	); err != nil {
		t.Fatalf("force sent: %v", err)
	}

	requireFenceMiss(t, repository.MarkFailed(ctx, id, testWorkerA, 0, 3, time.Minute, "late failure"), actionMarkFailed, sendfence.ReasonStatusChanged)

	if failed := countByStatus(ctx, t, repository, domain.DeliveryStatusFailed); failed != 0 {
		t.Fatalf("late MarkFailed must not resurrect a SENT row, failed=%d", failed)
	}

	if sent := countByStatus(ctx, t, repository, domain.DeliveryStatusSent); sent != 1 {
		t.Fatalf("row must remain SENT, sent=%d", sent)
	}
}

func TestMarkFailed_WithBackoff(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(ctx, t, repository)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	// maxRetries=3, 첫 실패 → 아직 PENDING 유지
	if err := repository.MarkFailed(ctx, items[0].ID, testWorkerA, 0, 3, time.Minute, "send error"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	pending := countByStatus(ctx, t, repository, domain.DeliveryStatusPending)
	if pending != 1 {
		t.Fatalf("expected 1 pending after first failure, got %d", pending)
	}
}

func TestMarkFailed_FromSending(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(ctx, t, repository)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	markOutboxSending(ctx, t, repository, &items[0])

	if err := repository.MarkFailed(ctx, items[0].ID, testWorkerA, 0, 3, time.Minute, "send error"); err != nil {
		t.Fatalf("MarkFailed must accept SENDING owner transition: %v", err)
	}

	if pending := countByStatus(ctx, t, repository, domain.DeliveryStatusPending); pending != 1 {
		t.Fatalf("expected 1 pending after retry, got %d", pending)
	}

	if sending := countByStatus(ctx, t, repository, deliveryStatusSending); sending != 0 {
		t.Fatalf("expected 0 sending after retry, got %d", sending)
	}
}

func TestCleanup(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(ctx, t, repository)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	markOutboxSending(ctx, t, repository, &items[0])

	if err := repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].AttemptCount); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	if _, err := repository.pool.Exec(ctx, "UPDATE notification_delivery_outbox SET sent_at = clock_timestamp() - INTERVAL '10 days' WHERE id = $1",
		items[0].ID); err != nil {
		t.Fatalf("backdate sent_at: %v", err)
	}

	cleaned, err := repository.Cleanup(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if cleaned != 1 {
		t.Fatalf("expected 1 cleaned, got %d", cleaned)
	}
}

func TestCleanup_FailedItems(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room-fail", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	_, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET status = 'FAILED', created_at = clock_timestamp() - INTERVAL '10 days' WHERE content_id = $1",
		"2026-W08:room-fail",
	)
	if err != nil {
		t.Fatalf("set old failed status: %v", err)
	}

	failed := countByStatus(ctx, t, repository, domain.DeliveryStatusFailed)
	if failed != 1 {
		t.Fatalf("expected 1 failed, got %d", failed)
	}

	cleaned, err := repository.Cleanup(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if cleaned != 1 {
		t.Fatalf("expected 1 failed item cleaned, got %d", cleaned)
	}

	remaining := countByStatus(ctx, t, repository, domain.DeliveryStatusFailed)
	if remaining != 0 {
		t.Fatalf("expected 0 failed after cleanup, got %d", remaining)
	}
}

func TestCountByStatus(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	for i := range 3 {
		if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room"+string(rune('a'+i)), "msg"); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	if cnt := countByStatus(ctx, t, repository, domain.DeliveryStatusPending); cnt != 3 {
		t.Fatalf("expected 3 pending, got %d", cnt)
	}
}

func expireLease(ctx context.Context, t *testing.T, repository *outboxFixture, id int64) {
	t.Helper()

	if _, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET lock_expires_at = clock_timestamp() - INTERVAL '1 minute' WHERE id = $1",
		id,
	); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
}

func lockedByOf(ctx context.Context, t *testing.T, repository *outboxFixture, id int64) *string {
	t.Helper()

	var lockedBy *string

	if err := repository.pool.QueryRow(ctx,
		"SELECT locked_by FROM notification_delivery_outbox WHERE id = $1", id,
	).Scan(&lockedBy); err != nil {
		t.Fatalf("query locked_by: %v", err)
	}

	return lockedBy
}

func reclaimByWorkerB(ctx context.Context, t *testing.T, repository *outboxFixture, id int64) {
	t.Helper()

	items := claimItems(ctx, t, repository, testWorkerB, 1)
	if len(items) != 1 || items[0].ID != id {
		t.Fatalf("worker B must reclaim row %d, got %+v", id, items)
	}
}

func TestMarkSent_FenceRejectsStaleWorkerAfterReclaim(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(ctx, t, repository)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}

	id := itemsA[0].ID

	expireLease(ctx, t, repository, id)

	reclaimByWorkerB(ctx, t, repository, id)

	if err := repository.MarkSending(ctx, id, testWorkerB, 0, testLease); err != nil {
		t.Fatalf("worker B mark sending: %v", err)
	}

	requireFenceMiss(t, repository.MarkSent(ctx, id, testWorkerA, itemsA[0].AttemptCount), actionMarkSent, sendfence.ReasonOwnerChanged)

	if sent := countByStatus(ctx, t, repository, domain.DeliveryStatusSent); sent != 0 {
		t.Fatalf("stale MarkSent must not mark SENT, sent=%d", sent)
	}

	if sending := countByStatus(ctx, t, repository, deliveryStatusSending); sending != 1 {
		t.Fatalf("row must stay SENDING under B's ownership, sending=%d", sending)
	}

	if err := repository.MarkSent(ctx, id, testWorkerB, 0); err != nil {
		t.Fatalf("current lease holder B MarkSent must succeed: %v", err)
	}

	if sent := countByStatus(ctx, t, repository, domain.DeliveryStatusSent); sent != 1 {
		t.Fatalf("B MarkSent must mark SENT, sent=%d", sent)
	}
}

func TestMarkFailed_FenceRejectsStaleWorkerAfterReclaim(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(ctx, t, repository)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}

	id := itemsA[0].ID

	expireLease(ctx, t, repository, id)
	reclaimByWorkerB(ctx, t, repository, id)

	requireFenceMiss(t, repository.MarkFailed(ctx, id, testWorkerA, 0, 3, time.Minute, "stale worker A failure"), actionMarkFailed, sendfence.ReasonOwnerChanged)

	var attemptCount int

	if err := repository.pool.QueryRow(ctx,
		"SELECT attempt_count FROM notification_delivery_outbox WHERE id = $1", id,
	).Scan(&attemptCount); err != nil {
		t.Fatalf("query attempt_count: %v", err)
	}

	if attemptCount != 0 {
		t.Fatalf("fenced MarkFailed must not bump attempt_count, got %d", attemptCount)
	}
}

func TestMarkSent_RejectsForeignWorkerHoldingValidLease(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(ctx, t, repository)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}

	id := itemsA[0].ID

	markOutboxSending(ctx, t, repository, &itemsA[0])

	requireFenceMiss(t, repository.MarkSent(ctx, id, testWorkerB, itemsA[0].AttemptCount), actionMarkSent, sendfence.ReasonOwnerChanged)

	if sent := countByStatus(ctx, t, repository, domain.DeliveryStatusSent); sent != 0 {
		t.Fatalf("foreign MarkSent must not mark SENT, sent=%d", sent)
	}

	if err := repository.MarkSent(ctx, id, testWorkerA, itemsA[0].AttemptCount); err != nil {
		t.Fatalf("owner A MarkSent must succeed: %v", err)
	}
}

func TestMarkFailed_RejectsForeignWorkerHoldingValidLease(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(ctx, t, repository)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}

	id := itemsA[0].ID

	requireFenceMiss(t, repository.MarkFailed(ctx, id, testWorkerB, 0, 3, time.Minute, "foreign failure"), actionMarkFailed, sendfence.ReasonOwnerChanged)

	var attemptCount int

	if err := repository.pool.QueryRow(ctx,
		"SELECT attempt_count FROM notification_delivery_outbox WHERE id = $1", id,
	).Scan(&attemptCount); err != nil {
		t.Fatalf("query attempt_count: %v", err)
	}

	if attemptCount != 0 {
		t.Fatalf("fenced MarkFailed must not bump attempt_count, got %d", attemptCount)
	}
}

// TestMarkSending_Fenced는 발송 전이의 각 fence 조건을 하나씩 어긋나게 하고 관측 원인을 확인한다. 각 경우의 mutate는
// claim한 행에 적용하는 SQL이며 빈 값이면 행을 그대로 둔다.
func TestMarkSending_Fenced(t *testing.T) {
	tests := []struct {
		name   string
		worker string
		mutate string
		reason sendfence.Reason
	}{
		{name: "foreign worker locked_by", worker: testWorkerB, reason: sendfence.ReasonOwnerChanged},
		{
			name: "expired lease", worker: testWorkerA, reason: sendfence.ReasonLeaseExpired,
			mutate: "UPDATE notification_delivery_outbox SET lock_expires_at = clock_timestamp() - INTERVAL '1 minute' WHERE id = $1",
		},
		{
			name: "non-pending sending", worker: testWorkerA, reason: sendfence.ReasonStatusChanged,
			mutate: "UPDATE notification_delivery_outbox SET status = 'SENDING', sending_started_at = clock_timestamp() WHERE id = $1",
		},
		{
			name: "non-pending failed", worker: testWorkerA, reason: sendfence.ReasonStatusChanged,
			mutate: "UPDATE notification_delivery_outbox SET status = 'FAILED' WHERE id = $1",
		},
		{
			name: "other claim attempt", worker: testWorkerA, reason: sendfence.ReasonVersionChanged,
			mutate: "UPDATE notification_delivery_outbox SET attempt_count = attempt_count + 1 WHERE id = $1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repository := testRepository(t)
			ctx := t.Context()

			if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
				t.Fatalf("enqueue: %v", err)
			}

			item := fetchAndLockItems(ctx, t, repository)[0]

			if tc.mutate != "" {
				if _, err := repository.pool.Exec(ctx, tc.mutate, item.ID); err != nil {
					t.Fatalf("mutate claimed row: %v", err)
				}
			}

			requireFenceMiss(t, repository.MarkSending(ctx, item.ID, tc.worker, item.AttemptCount, testLease), actionMarkSending, tc.reason)
		})
	}
}

func TestFetchReadyAndLock_ReclaimsExpiredLease(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(ctx, t, repository)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}

	id := itemsA[0].ID

	if got := claimItems(ctx, t, repository, testWorkerB, 1); len(got) != 0 {
		t.Fatalf("a valid lease must not be reclaimable, got %d", len(got))
	}

	expireLease(ctx, t, repository, id)
	reclaimByWorkerB(ctx, t, repository, id)

	owner := lockedByOf(ctx, t, repository, id)
	if owner == nil || *owner != testWorkerB {
		t.Fatalf("expired-lease reclaim must set locked_by=%s, got %v", testWorkerB, owner)
	}
}

func TestFetchReadyAndLock_DoesNotReclaimSendingAfterLeaseExpiry(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(ctx, t, repository)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}

	id := itemsA[0].ID
	markOutboxSending(ctx, t, repository, &itemsA[0])
	expireLease(ctx, t, repository, id)

	if got := claimItems(ctx, t, repository, testWorkerB, 1); len(got) != 0 {
		t.Fatalf("SENDING row must not be reclaimable, got %d", len(got))
	}

	if sending := countByStatus(ctx, t, repository, deliveryStatusSending); sending != 1 {
		t.Fatalf("row must remain SENDING, sending=%d", sending)
	}
}

type ownerRow struct {
	status   string
	attempts int
	lockedBy *string
	errText  *string
}

func readOwnerRow(ctx context.Context, t *testing.T, repository *outboxFixture, id int64) ownerRow {
	t.Helper()

	var row ownerRow

	if err := repository.pool.QueryRow(ctx,
		"SELECT status, attempt_count, locked_by, error FROM notification_delivery_outbox WHERE id = $1", id,
	).Scan(&row.status, &row.attempts, &row.lockedBy, &row.errText); err != nil {
		t.Fatalf("read owner row: %v", err)
	}

	return row
}

// 같은 worker 이름이 재시도 뒤 다시 claim한 행에서 이전 claim 세대(attempt_count)의 전이는 소유자가 같아도 적용되지 않는다.
func TestPreviousAttemptTransitionsRejectedAfterSameOwnerReclaim(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	first := fetchAndLockItems(ctx, t, repository)[0]
	markOutboxSending(ctx, t, repository, &first)

	if err := repository.MarkFailed(ctx, first.ID, testWorkerA, first.AttemptCount, 3, 0, "retry"); err != nil {
		t.Fatalf("first attempt failure: %v", err)
	}

	second := fetchAndLockItems(ctx, t, repository)[0]
	if second.ID != first.ID || second.AttemptCount != first.AttemptCount+1 {
		t.Fatalf("second claim = id %d attempt %d, want id %d attempt %d", second.ID, second.AttemptCount, first.ID, first.AttemptCount+1)
	}

	requireFenceMiss(t, repository.MarkSending(ctx, first.ID, testWorkerA, first.AttemptCount, testLease), actionMarkSending, sendfence.ReasonVersionChanged)
	markOutboxSending(ctx, t, repository, &second)

	before := readOwnerRow(ctx, t, repository, first.ID)
	requireFenceMiss(t, repository.MarkSent(ctx, first.ID, testWorkerA, first.AttemptCount), actionMarkSent, sendfence.ReasonVersionChanged)
	requireFenceMiss(t, repository.MarkFailed(ctx, first.ID, testWorkerA, first.AttemptCount, 3, time.Minute, "stale failure"), actionMarkFailed, sendfence.ReasonVersionChanged)
	requireFenceMiss(t, repository.MarkQuarantined(ctx, first.ID, testWorkerA, first.AttemptCount, "stale unknown"), actionMarkQuarantined, sendfence.ReasonVersionChanged)

	if after := readOwnerRow(ctx, t, repository, first.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("stale attempt transitions changed the row: before %+v after %+v", before, after)
	}

	if err := repository.MarkSent(ctx, second.ID, testWorkerA, second.AttemptCount); err != nil {
		t.Fatalf("current attempt MarkSent: %v", err)
	}
}
