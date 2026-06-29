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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
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
	locked, err := repository.FetchAndLock(ctx, batchSize, 5*time.Minute)
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
	items, err := repository.FetchAndLock(ctx, 1, 5*time.Minute)
	if err != nil {
		t.Fatalf("fetch and lock: %v", err)
	}
	return items
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

	items, err := repository.FetchAndLock(ctx, 2, 5*time.Minute)
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

	if err := repository.MarkSent(ctx, items[0].ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	cnt := countByStatus(t, repository, ctx, domain.DeliveryStatusSent)
	if cnt != 1 {
		t.Fatalf("expected 1 sent, got %d", cnt)
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

	if err := repository.MarkSent(ctx, items[0].ID); err != nil {
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

	if err := repository.MarkSent(ctx, id); err != nil {
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

	if err := repository.MarkFailed(ctx, id, 3, time.Minute, "late failure"); err != nil {
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
	if err := repository.MarkFailed(ctx, items[0].ID, 3, time.Minute, "send error"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	pending := countByStatus(t, repository, ctx, domain.DeliveryStatusPending)
	if pending != 1 {
		t.Fatalf("expected 1 pending after first failure, got %d", pending)
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

	if err := repository.MarkSent(ctx, items[0].ID); err != nil {
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
