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

//go:build integration

package delivery

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	if err != nil {
		t.Fatalf("failed to connect to test DB: %v", err)
	}

	// 테이블 + 인덱스 생성 (AutoMigrate는 UNIQUE INDEX를 생성하지 않음)
	if err := db.AutoMigrate(&domain.NotificationDeliveryOutbox{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	for _, ddl := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_ndo_kind_content ON notification_delivery_outbox(kind, content_id)",
		"CREATE INDEX IF NOT EXISTS idx_ndo_pending_next ON notification_delivery_outbox(next_attempt_at, created_at) WHERE status = 'PENDING'",
		"CREATE INDEX IF NOT EXISTS idx_ndo_sent_cleanup ON notification_delivery_outbox(COALESCE(sent_at, created_at)) WHERE status IN ('SENT', 'FAILED')",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("failed to create index: %v", err)
		}
	}

	// 테스트 전 데이터 클리어
	db.Exec("DELETE FROM notification_delivery_outbox")

	t.Cleanup(func() {
		db.Exec("DELETE FROM notification_delivery_outbox")
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})

	return db
}

func testRepository(t *testing.T) *OutboxRepository {
	t.Helper()
	db := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewOutboxRepository(db, logger)
}

func buildOutboxBatchItems(count int) []OutboxItem {
	items := make([]OutboxItem, 0, count)
	for i := 0; i < count; i++ {
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
	for _, item := range locked {
		ids = append(ids, item.ID)
	}
	return ids
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
	cnt, _ := repository.CountByStatus(ctx, domain.DeliveryStatusPending)
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

	// 수동으로 FAILED 상태로 변경
	repository.db.Exec("UPDATE notification_delivery_outbox SET status = 'FAILED' WHERE content_id = ?", "2026-W08:room1")

	// 재 Enqueue → FAILED이므로 갱신
	if err := repository.Enqueue(ctx, domain.DeliveryKindMajorEventWeekly, "2026-W08", "room1", "retry-msg"); err != nil {
		t.Fatalf("retry enqueue: %v", err)
	}

	cnt, _ := repository.CountByStatus(ctx, domain.DeliveryStatusPending)
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

	for i := 0; i < 3; i++ {
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

	items, _ := repository.FetchAndLock(ctx, 1, 5*time.Minute)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	if err := repository.MarkSent(ctx, items[0].ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	cnt, _ := repository.CountByStatus(ctx, domain.DeliveryStatusSent)
	if cnt != 1 {
		t.Fatalf("expected 1 sent, got %d", cnt)
	}
}

func TestMarkFailed_WithBackoff(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items, _ := repository.FetchAndLock(ctx, 1, 5*time.Minute)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	// maxRetries=3, 첫 실패 → 아직 PENDING 유지
	if err := repository.MarkFailed(ctx, items[0].ID, 3, time.Minute, "send error"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	pending, _ := repository.CountByStatus(ctx, domain.DeliveryStatusPending)
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

			ids := make([]int64, 0)
			if tc.count > 0 {
				if err := repository.EnqueueBatch(ctx, buildOutboxBatchItems(tc.count)); err != nil {
					t.Fatalf("enqueue batch: %v", err)
				}
				ids = fetchLockedIDs(t, repository, ctx, tc.count+1)
				if len(ids) != tc.count {
					t.Fatalf("locked ids = %d, want %d", len(ids), tc.count)
				}
			}

			if err := repository.MarkSentBatch(ctx, ids); err != nil {
				t.Fatalf("mark sent batch: %v", err)
			}

			sent, err := repository.CountByStatus(ctx, domain.DeliveryStatusSent)
			if err != nil {
				t.Fatalf("count sent: %v", err)
			}
			if sent != int64(tc.count) {
				t.Fatalf("sent count = %d, want %d", sent, tc.count)
			}
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

			ids := make([]int64, 0)
			if tc.count > 0 {
				if err := repository.EnqueueBatch(ctx, buildOutboxBatchItems(tc.count)); err != nil {
					t.Fatalf("enqueue batch: %v", err)
				}
				ids = fetchLockedIDs(t, repository, ctx, tc.count+1)
				if len(ids) != tc.count {
					t.Fatalf("locked ids = %d, want %d", len(ids), tc.count)
				}
			}

			if err := repository.MarkFailedBatch(ctx, ids, reason); err != nil {
				t.Fatalf("mark failed batch: %v", err)
			}

			failed, err := repository.CountByStatus(ctx, domain.DeliveryStatusFailed)
			if err != nil {
				t.Fatalf("count failed: %v", err)
			}
			if failed != int64(tc.count) {
				t.Fatalf("failed count = %d, want %d", failed, tc.count)
			}
		})
	}
}

func TestCleanup(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	items, _ := repository.FetchAndLock(ctx, 1, 5*time.Minute)
	if len(items) == 0 {
		t.Fatal("no items fetched")
	}

	if err := repository.MarkSent(ctx, items[0].ID); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	// sent_at을 과거로 변경
	repository.db.Exec("UPDATE notification_delivery_outbox SET sent_at = ? WHERE id = ?",
		time.Now().Add(-10*24*time.Hour), items[0].ID)

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

	// FAILED 상태로 변경 (sent_at은 NULL 유지) + created_at을 과거로
	repository.db.Exec(
		"UPDATE notification_delivery_outbox SET status = 'FAILED', created_at = ? WHERE content_id = ?",
		time.Now().Add(-10*24*time.Hour), "2026-W08:room-fail",
	)

	failed, _ := repository.CountByStatus(ctx, domain.DeliveryStatusFailed)
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

	remaining, _ := repository.CountByStatus(ctx, domain.DeliveryStatusFailed)
	if remaining != 0 {
		t.Fatalf("expected 0 failed after cleanup, got %d", remaining)
	}
}

func TestCountByStatus(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
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
