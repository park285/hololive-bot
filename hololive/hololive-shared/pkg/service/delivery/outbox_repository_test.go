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
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	testWorkerA = "test-worker-A"
	testWorkerB = "test-worker-B"
	testLockTTL = 5 * time.Minute
	testLease   = 60 * time.Second
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	return dbtest.NewPool(t)
}

func testRepository(t *testing.T) *OutboxRepository {
	t.Helper()
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewOutboxRepositoryFromPool(db, logger)
}

func buildOutboxBatchItems(count int) []OutboxItem {
	items := make([]OutboxItem, 0, count)
	for i := range count {
		items = append(items, OutboxItem{
			Kind:      domain.DeliveryKindMemberNewsWeekly,
			PeriodKey: "2026-W08",
			RoomID:    fmt.Sprintf("room-batch-%d", i),
			Message:   fmt.Sprintf("batch-msg-%d", i),
		})
	}
	return items
}

func fetchLockedIDs(t *testing.T, repository *OutboxRepository, ctx context.Context, batchSize int) []int64 {
	t.Helper()
	locked, err := repository.FetchAndLock(ctx, testWorkerA, batchSize, testLockTTL, testLease)
	if err != nil {
		t.Fatalf("fetch and lock: %v", err)
	}
	ids := make([]int64, 0, len(locked))
	for i := range locked {
		ids = append(ids, locked[i].ID)
	}
	return ids
}

func fetchAndLockItems(t *testing.T, repository *OutboxRepository, ctx context.Context) []domain.NotificationDeliveryOutbox {
	t.Helper()
	items, err := repository.FetchAndLock(ctx, testWorkerA, 1, testLockTTL, testLease)
	if err != nil {
		t.Fatalf("fetch and lock: %v", err)
	}
	return items
}

func markOutboxSending(t *testing.T, repository *OutboxRepository, ctx context.Context, item *domain.NotificationDeliveryOutbox) {
	t.Helper()
	ok, err := repository.MarkSending(ctx, item.ID, testWorkerA, testLease)
	if err != nil {
		t.Fatalf("mark sending: %v", err)
	}
	if !ok {
		t.Fatal("mark sending fenced unexpectedly")
	}
}

func countByStatus(t *testing.T, repository *OutboxRepository, ctx context.Context, status domain.DeliveryOutboxStatus) int64 {
	t.Helper()
	count, err := repository.CountByStatus(ctx, status)
	if err != nil {
		t.Fatalf("count by status %s: %v", status, err)
	}
	return count
}

func TestEnqueue_Idempotent_PendingNoOp(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	// 첫 번째 Enqueue
	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg1"); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}

	// 동일 content_id로 다시 Enqueue → PENDING이므로 무시
	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg2"); err != nil {
		t.Fatalf("second enqueue: %v", err)
	}

	// payload가 변경되지 않아야 함 (ON CONFLICT 조건: status=FAILED만 갱신)
	cnt := countByStatus(t, repository, ctx, domain.DeliveryStatusPending)
	if cnt != 1 {
		t.Fatalf("expected 1 pending, got %d", cnt)
	}
}

func TestEnqueue_FailedRetry(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMajorEventWeekly, "2026-W08", "room1", "msg1"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if _, err := repository.pool.Exec(ctx, "UPDATE notification_delivery_outbox SET status = 'FAILED' WHERE content_id = $1", "2026-W08:room1"); err != nil {
		t.Fatalf("set failed status: %v", err)
	}

	// 재 Enqueue → FAILED이므로 갱신
	if err := repository.Enqueue(ctx, domain.DeliveryKindMajorEventWeekly, "2026-W08", "room1", "retry-msg"); err != nil {
		t.Fatalf("retry enqueue: %v", err)
	}

	cnt := countByStatus(t, repository, ctx, domain.DeliveryStatusPending)
	if cnt != 1 {
		t.Fatalf("expected 1 pending after retry, got %d", cnt)
	}
}

func TestEnqueueBatch(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{name: "empty", count: 0},
		{name: "single", count: 1},
		{name: "fifty", count: 50},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repository := testRepository(t)
			ctx := context.Background()

			if err := repository.EnqueueBatch(ctx, buildOutboxBatchItems(tc.count)); err != nil {
				t.Fatalf("enqueue batch: %v", err)
			}

			pending, err := repository.CountByStatus(ctx, domain.DeliveryStatusPending)
			if err != nil {
				t.Fatalf("count pending: %v", err)
			}
			if pending != int64(tc.count) {
				t.Fatalf("pending count = %d, want %d", pending, tc.count)
			}
		})
	}
}

func TestFetchAndLock(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	for i := range 3 {
		if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room"+string(rune('a'+i)), "msg"); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	items, err := repository.FetchAndLock(ctx, testWorkerA, 2, testLockTTL, testLease)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
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

func TestMarkSent(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(t, repository, ctx)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	if _, err := repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].LockedAt.Time); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	cnt := countByStatus(t, repository, ctx, domain.DeliveryStatusSent)
	if cnt != 1 {
		t.Fatalf("expected 1 sent, got %d", cnt)
	}
}

func TestMarkSent_FromSending(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(t, repository, ctx)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}
	markOutboxSending(t, repository, ctx, &items[0])

	ok, err := repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].LockedAt.Time)
	if err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if !ok {
		t.Fatal("MarkSent must accept SENDING owner transition")
	}
	if sent := countByStatus(t, repository, ctx, domain.DeliveryStatusSent); sent != 1 {
		t.Fatalf("expected 1 sent, got %d", sent)
	}
	if sending := countByStatus(t, repository, ctx, deliveryStatusSending); sending != 0 {
		t.Fatalf("expected 0 sending after sent, got %d", sending)
	}
}

func TestMarkSent_ClearsLock(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items := fetchAndLockItems(t, repository, ctx)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}
	if !items[0].LockedAt.Valid {
		t.Fatal("expected locked_at set after FetchAndLock")
	}

	if _, err := repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].LockedAt.Time); err != nil {
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
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items := fetchAndLockItems(t, repository, ctx)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}
	id := items[0].ID

	if _, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET status = 'FAILED', locked_at = NULL WHERE id = $1", id,
	); err != nil {
		t.Fatalf("force failed: %v", err)
	}

	if _, err := repository.MarkSent(ctx, id, testWorkerA, items[0].LockedAt.Time); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	if sent := countByStatus(t, repository, ctx, domain.DeliveryStatusSent); sent != 0 {
		t.Fatalf("late MarkSent must not resurrect a FAILED row to SENT, sent=%d", sent)
	}
	if failed := countByStatus(t, repository, ctx, domain.DeliveryStatusFailed); failed != 1 {
		t.Fatalf("row must remain FAILED, failed=%d", failed)
	}
}

func TestMarkFailed_DoesNotResurrectSentRow(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	items := fetchAndLockItems(t, repository, ctx)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}
	id := items[0].ID

	if _, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET status = 'SENT', locked_at = NULL WHERE id = $1", id,
	); err != nil {
		t.Fatalf("force sent: %v", err)
	}

	if _, err := repository.MarkFailed(ctx, id, testWorkerA, items[0].LockedAt.Time, 3, time.Minute, "late failure"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	if failed := countByStatus(t, repository, ctx, domain.DeliveryStatusFailed); failed != 0 {
		t.Fatalf("late MarkFailed must not resurrect a SENT row, failed=%d", failed)
	}
	if sent := countByStatus(t, repository, ctx, domain.DeliveryStatusSent); sent != 1 {
		t.Fatalf("row must remain SENT, sent=%d", sent)
	}
}

func TestMarkFailed_WithBackoff(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(t, repository, ctx)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	// maxRetries=3, 첫 실패 → 아직 PENDING 유지
	if _, err := repository.MarkFailed(ctx, items[0].ID, testWorkerA, items[0].LockedAt.Time, 3, time.Minute, "send error"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	pending := countByStatus(t, repository, ctx, domain.DeliveryStatusPending)
	if pending != 1 {
		t.Fatalf("expected 1 pending after first failure, got %d", pending)
	}
}

func TestMarkFailed_FromSending(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(t, repository, ctx)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}
	markOutboxSending(t, repository, ctx, &items[0])

	ok, err := repository.MarkFailed(ctx, items[0].ID, testWorkerA, items[0].LockedAt.Time, 3, time.Minute, "send error")
	if err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if !ok {
		t.Fatal("MarkFailed must accept SENDING owner transition")
	}
	if pending := countByStatus(t, repository, ctx, domain.DeliveryStatusPending); pending != 1 {
		t.Fatalf("expected 1 pending after retry, got %d", pending)
	}
	if sending := countByStatus(t, repository, ctx, deliveryStatusSending); sending != 0 {
		t.Fatalf("expected 0 sending after retry, got %d", sending)
	}
}

func TestMarkSentBatch(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{name: "empty", count: 0},
		{name: "single", count: 1},
		{name: "fifty", count: 50},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repository := testRepository(t)
			ctx := context.Background()

			ids := enqueueAndFetchLockedIDs(t, repository, ctx, tc.count)

			if err := repository.MarkSentBatch(ctx, ids); err != nil {
				t.Fatalf("mark sent batch: %v", err)
			}

			assertStatusCount(t, repository, ctx, domain.DeliveryStatusSent, tc.count)
		})
	}
}

func TestMarkFailedBatch(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{name: "empty", count: 0},
		{name: "single", count: 1},
		{name: "fifty", count: 50},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repository := testRepository(t)
			ctx := context.Background()
			reason := "batch send failed"

			ids := enqueueAndFetchLockedIDs(t, repository, ctx, tc.count)

			if err := repository.MarkFailedBatch(ctx, ids, reason); err != nil {
				t.Fatalf("mark failed batch: %v", err)
			}

			assertStatusCount(t, repository, ctx, domain.DeliveryStatusFailed, tc.count)
		})
	}
}

func enqueueAndFetchLockedIDs(t *testing.T, repository *OutboxRepository, ctx context.Context, count int) []int64 {
	t.Helper()

	if count == 0 {
		return nil
	}
	if err := repository.EnqueueBatch(ctx, buildOutboxBatchItems(count)); err != nil {
		t.Fatalf("enqueue batch: %v", err)
	}
	ids := fetchLockedIDs(t, repository, ctx, count+1)
	if len(ids) != count {
		t.Fatalf("locked ids = %d, want %d", len(ids), count)
	}
	return ids
}

func assertStatusCount(t *testing.T, repository *OutboxRepository, ctx context.Context, status domain.DeliveryOutboxStatus, want int) {
	t.Helper()

	got, err := repository.CountByStatus(ctx, status)
	if err != nil {
		t.Fatalf("count %s: %v", status, err)
	}
	if got != int64(want) {
		t.Fatalf("%s count = %d, want %d", status, got, want)
	}
}

func TestCleanup(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(t, repository, ctx)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	if _, err := repository.MarkSent(ctx, items[0].ID, testWorkerA, items[0].LockedAt.Time); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	if _, err := repository.pool.Exec(ctx, "UPDATE notification_delivery_outbox SET sent_at = $1 WHERE id = $2",
		time.Now().Add(-10*24*time.Hour), items[0].ID); err != nil {
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
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room-fail", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	_, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET status = 'FAILED', created_at = $1 WHERE content_id = $2",
		time.Now().Add(-10*24*time.Hour), "2026-W08:room-fail",
	)
	if err != nil {
		t.Fatalf("set old failed status: %v", err)
	}

	failed := countByStatus(t, repository, ctx, domain.DeliveryStatusFailed)
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

	remaining := countByStatus(t, repository, ctx, domain.DeliveryStatusFailed)
	if remaining != 0 {
		t.Fatalf("expected 0 failed after cleanup, got %d", remaining)
	}
}

func TestCountByStatus(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	for i := range 3 {
		if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room"+string(rune('a'+i)), "msg"); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	cnt, err := repository.CountByStatus(ctx, domain.DeliveryStatusPending)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 3 {
		t.Fatalf("expected 3 pending, got %d", cnt)
	}
}

func expireLease(t *testing.T, repository *OutboxRepository, ctx context.Context, id int64) {
	t.Helper()
	if _, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET lock_expires_at = $1 WHERE id = $2",
		time.Now().Add(-time.Minute), id,
	); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
}

func setLegacyLock(t *testing.T, repository *OutboxRepository, ctx context.Context, id int64, lockedAtAge time.Duration) time.Time {
	t.Helper()
	var lockedAt time.Time
	if err := repository.pool.QueryRow(ctx,
		"UPDATE notification_delivery_outbox SET locked_at = $1, locked_by = NULL, lock_expires_at = NULL WHERE id = $2 RETURNING locked_at",
		time.Now().Add(-lockedAtAge), id,
	).Scan(&lockedAt); err != nil {
		t.Fatalf("set legacy lock: %v", err)
	}
	return lockedAt
}

func onlyRowID(t *testing.T, repository *OutboxRepository, ctx context.Context) int64 {
	t.Helper()
	var id int64
	if err := repository.pool.QueryRow(ctx, "SELECT id FROM notification_delivery_outbox").Scan(&id); err != nil {
		t.Fatalf("query row id: %v", err)
	}
	return id
}

func lockedByOf(t *testing.T, repository *OutboxRepository, ctx context.Context, id int64) *string {
	t.Helper()
	var lockedBy *string
	if err := repository.pool.QueryRow(ctx,
		"SELECT locked_by FROM notification_delivery_outbox WHERE id = $1", id,
	).Scan(&lockedBy); err != nil {
		t.Fatalf("query locked_by: %v", err)
	}
	return lockedBy
}

func reclaimByWorkerB(t *testing.T, repository *OutboxRepository, ctx context.Context, id int64) domain.NotificationDeliveryOutbox {
	t.Helper()
	items, err := repository.FetchAndLock(ctx, testWorkerB, 1, testLockTTL, testLease)
	if err != nil {
		t.Fatalf("worker B fetch and lock: %v", err)
	}
	if len(items) != 1 || items[0].ID != id {
		t.Fatalf("worker B must reclaim row %d, got %+v", id, items)
	}
	return items[0]
}

func TestMarkSent_FenceRejectsStaleWorkerAfterReclaim(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(t, repository, ctx)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}
	id := itemsA[0].ID
	staleLockedAt := itemsA[0].LockedAt.Time

	expireLease(t, repository, ctx, id)
	itemB := reclaimByWorkerB(t, repository, ctx, id)

	fenced, err := repository.MarkSent(ctx, id, testWorkerA, staleLockedAt)
	if err != nil {
		t.Fatalf("stale worker A mark sent: %v", err)
	}
	if fenced {
		t.Fatal("stale MarkSent must be fenced off after reclaim")
	}
	if sent := countByStatus(t, repository, ctx, domain.DeliveryStatusSent); sent != 0 {
		t.Fatalf("stale MarkSent must not mark SENT, sent=%d", sent)
	}
	if pending := countByStatus(t, repository, ctx, domain.DeliveryStatusPending); pending != 1 {
		t.Fatalf("row must stay PENDING under B's lease, pending=%d", pending)
	}

	okFenced, err := repository.MarkSent(ctx, id, testWorkerB, itemB.LockedAt.Time)
	if err != nil {
		t.Fatalf("worker B mark sent: %v", err)
	}
	if !okFenced {
		t.Fatal("current lease holder B MarkSent must succeed")
	}
	if sent := countByStatus(t, repository, ctx, domain.DeliveryStatusSent); sent != 1 {
		t.Fatalf("B MarkSent must mark SENT, sent=%d", sent)
	}
}

func TestMarkFailed_FenceRejectsStaleWorkerAfterReclaim(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(t, repository, ctx)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}
	id := itemsA[0].ID
	staleLockedAt := itemsA[0].LockedAt.Time

	expireLease(t, repository, ctx, id)
	reclaimByWorkerB(t, repository, ctx, id)

	fenced, err := repository.MarkFailed(ctx, id, testWorkerA, staleLockedAt, 3, time.Minute, "stale worker A failure")
	if err != nil {
		t.Fatalf("stale worker A mark failed: %v", err)
	}
	if fenced {
		t.Fatal("stale MarkFailed must be fenced off after reclaim")
	}

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
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(t, repository, ctx)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}
	id := itemsA[0].ID

	fenced, err := repository.MarkSent(ctx, id, testWorkerB, itemsA[0].LockedAt.Time)
	if err != nil {
		t.Fatalf("foreign worker mark sent: %v", err)
	}
	if fenced {
		t.Fatal("foreign worker MarkSent must be fenced even with a matching locked_at")
	}
	if sent := countByStatus(t, repository, ctx, domain.DeliveryStatusSent); sent != 0 {
		t.Fatalf("foreign MarkSent must not mark SENT, sent=%d", sent)
	}

	okFenced, err := repository.MarkSent(ctx, id, testWorkerA, itemsA[0].LockedAt.Time)
	if err != nil {
		t.Fatalf("owner mark sent: %v", err)
	}
	if !okFenced {
		t.Fatal("owner A MarkSent must succeed")
	}
}

func TestMarkFailed_RejectsForeignWorkerHoldingValidLease(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(t, repository, ctx)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}
	id := itemsA[0].ID

	fenced, err := repository.MarkFailed(ctx, id, testWorkerB, itemsA[0].LockedAt.Time, 3, time.Minute, "foreign failure")
	if err != nil {
		t.Fatalf("foreign worker mark failed: %v", err)
	}
	if fenced {
		t.Fatal("foreign worker MarkFailed must be fenced even with a matching locked_at")
	}

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

func TestMarkSending_Fenced(t *testing.T) {
	tests := []struct {
		name   string
		worker string
		mutate func(t *testing.T, repository *OutboxRepository, ctx context.Context, item *domain.NotificationDeliveryOutbox)
	}{
		{
			name:   "foreign worker locked_by",
			worker: testWorkerB,
			mutate: func(*testing.T, *OutboxRepository, context.Context, *domain.NotificationDeliveryOutbox) {},
		},
		{
			name:   "expired lease",
			worker: testWorkerA,
			mutate: func(t *testing.T, repository *OutboxRepository, ctx context.Context, item *domain.NotificationDeliveryOutbox) {
				expireLease(t, repository, ctx, item.ID)
			},
		},
		{
			name:   "non-pending sending",
			worker: testWorkerA,
			mutate: func(t *testing.T, repository *OutboxRepository, ctx context.Context, item *domain.NotificationDeliveryOutbox) {
				markOutboxSending(t, repository, ctx, item)
			},
		},
		{
			name:   "non-pending failed",
			worker: testWorkerA,
			mutate: func(t *testing.T, repository *OutboxRepository, ctx context.Context, item *domain.NotificationDeliveryOutbox) {
				if _, err := repository.pool.Exec(ctx,
					"UPDATE notification_delivery_outbox SET status = 'FAILED' WHERE id = $1", item.ID,
				); err != nil {
					t.Fatalf("force failed: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repository := testRepository(t)
			ctx := context.Background()

			if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			items := fetchAndLockItems(t, repository, ctx)
			if len(items) == 0 {
				t.Fatal("no items fetched")
			}
			tc.mutate(t, repository, ctx, &items[0])

			ok, err := repository.MarkSending(ctx, items[0].ID, tc.worker, testLease)
			if err != nil {
				t.Fatalf("mark sending: %v", err)
			}
			if ok {
				t.Fatalf("MarkSending must be fenced for %q", tc.name)
			}
		})
	}
}

func TestFetchAndLock_ReclaimsExpiredLease(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(t, repository, ctx)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}
	id := itemsA[0].ID

	got, err := repository.FetchAndLock(ctx, testWorkerB, 1, testLockTTL, testLease)
	if err != nil {
		t.Fatalf("worker B fetch under valid lease: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a valid lease must not be reclaimable, got %d", len(got))
	}

	expireLease(t, repository, ctx, id)
	reclaimByWorkerB(t, repository, ctx, id)

	owner := lockedByOf(t, repository, ctx, id)
	if owner == nil || *owner != testWorkerB {
		t.Fatalf("expired-lease reclaim must set locked_by=%s, got %v", testWorkerB, owner)
	}
}

func TestFetchAndLock_DoesNotReclaimSendingAfterLeaseExpiry(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	itemsA := fetchAndLockItems(t, repository, ctx)
	if len(itemsA) == 0 {
		t.Fatal("worker A fetched no items")
	}
	id := itemsA[0].ID
	markOutboxSending(t, repository, ctx, &itemsA[0])
	expireLease(t, repository, ctx, id)

	got, err := repository.FetchAndLock(ctx, testWorkerB, 1, testLockTTL, testLease)
	if err != nil {
		t.Fatalf("worker B fetch after sending lease expiry: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SENDING row must not be reclaimable, got %d", len(got))
	}
	if sending := countByStatus(t, repository, ctx, deliveryStatusSending); sending != 1 {
		t.Fatalf("row must remain SENDING, sending=%d", sending)
	}
}

func TestQuarantineStaleSending(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items := fetchAndLockItems(t, repository, ctx)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}
	markOutboxSending(t, repository, ctx, &items[0])

	if _, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET sending_started_at = $1 WHERE id = $2",
		time.Now().Add(-2*time.Minute), items[0].ID,
	); err != nil {
		t.Fatalf("backdate sending_started_at: %v", err)
	}

	quarantined, err := repository.QuarantineStaleSending(ctx, time.Minute, 10)
	if err != nil {
		t.Fatalf("quarantine stale sending: %v", err)
	}
	if quarantined != 1 {
		t.Fatalf("quarantined = %d, want 1", quarantined)
	}
	if failed := countByStatus(t, repository, ctx, domain.DeliveryStatusFailed); failed != 1 {
		t.Fatalf("stale SENDING must move to FAILED, failed=%d", failed)
	}
	if pending := countByStatus(t, repository, ctx, domain.DeliveryStatusPending); pending != 0 {
		t.Fatalf("stale SENDING must not be rescheduled as PENDING, pending=%d", pending)
	}

	var status string
	var errText string
	var locksCleared bool
	if err := repository.pool.QueryRow(ctx,
		`SELECT status, error, locked_at IS NULL AND locked_by IS NULL AND lock_expires_at IS NULL
		 FROM notification_delivery_outbox WHERE id = $1`, items[0].ID,
	).Scan(&status, &errText, &locksCleared); err != nil {
		t.Fatalf("query quarantined row: %v", err)
	}
	if status != string(domain.DeliveryStatusFailed) {
		t.Fatalf("status = %s, want FAILED", status)
	}
	if !strings.Contains(errText, "stale sending") {
		t.Fatalf("error = %q, want stale sending marker", errText)
	}
	if !locksCleared {
		t.Fatal("stale SENDING quarantine must clear locks")
	}
}

func TestMarkSent_FallbackFenceForLegacyRow(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	id := onlyRowID(t, repository, ctx)

	legacyLockedAt := setLegacyLock(t, repository, ctx, id, 0)

	fenced, err := repository.MarkSent(ctx, id, testWorkerA, legacyLockedAt.Add(-time.Hour))
	if err != nil {
		t.Fatalf("legacy mismatch mark sent: %v", err)
	}
	if fenced {
		t.Fatal("legacy fallback must fence a mismatched locked_at")
	}

	okFenced, err := repository.MarkSent(ctx, id, testWorkerA, legacyLockedAt)
	if err != nil {
		t.Fatalf("legacy match mark sent: %v", err)
	}
	if !okFenced {
		t.Fatal("legacy fallback MarkSent with a matching locked_at must succeed")
	}
	if sent := countByStatus(t, repository, ctx, domain.DeliveryStatusSent); sent != 1 {
		t.Fatalf("legacy fallback MarkSent must mark SENT, sent=%d", sent)
	}
}

func TestFetchAndLock_ReclaimsLegacyRowViaLockTimeout(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	id := onlyRowID(t, repository, ctx)

	setLegacyLock(t, repository, ctx, id, 0)
	got, err := repository.FetchAndLock(ctx, testWorkerB, 1, testLockTTL, testLease)
	if err != nil {
		t.Fatalf("worker B fetch under fresh legacy lock: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a fresh legacy lock must not be reclaimable within lock timeout, got %d", len(got))
	}

	setLegacyLock(t, repository, ctx, id, 10*time.Minute)
	itemB := reclaimByWorkerB(t, repository, ctx, id)
	if !itemB.LockedAt.Valid {
		t.Fatal("legacy-timeout reclaim must set locked_at")
	}
	owner := lockedByOf(t, repository, ctx, id)
	if owner == nil || *owner != testWorkerB {
		t.Fatalf("legacy-timeout reclaim must set locked_by=%s, got %v", testWorkerB, owner)
	}
}
