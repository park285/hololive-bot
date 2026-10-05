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
	"log/slog"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const testRoomID = "room-1"

func testRepository(t *testing.T) *OutboxRepository {
	t.Helper()

	return NewOutboxRepositoryFromPool(dbtest.NewPool(t), slog.New(slog.DiscardHandler))
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

func countByStatus(ctx context.Context, t *testing.T, repository *OutboxRepository, status domain.DeliveryOutboxStatus) int64 {
	t.Helper()

	var count int64

	if err := repository.pool.QueryRow(ctx, "SELECT count(id) FROM notification_delivery_outbox WHERE status = $1", status).Scan(&count); err != nil {
		t.Fatalf("count by status %s: %v", status, err)
	}

	return count
}

func payloadMessage(ctx context.Context, t *testing.T, repository *OutboxRepository, contentID string) string {
	t.Helper()

	var message string

	if err := repository.pool.QueryRow(ctx, "SELECT payload->>'message' FROM notification_delivery_outbox WHERE content_id = $1", contentID).Scan(&message); err != nil {
		t.Fatalf("load payload message: %v", err)
	}

	return message
}

func TestEnqueue_Idempotent_PendingNoOp(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg1"); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}

	// 동일 content_id로 다시 Enqueue → PENDING이므로 무시
	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "msg2"); err != nil {
		t.Fatalf("second enqueue: %v", err)
	}

	if cnt := countByStatus(ctx, t, repository, domain.DeliveryStatusPending); cnt != 1 {
		t.Fatalf("expected 1 pending, got %d", cnt)
	}

	if got := payloadMessage(ctx, t, repository, "2026-W08:room1"); got != "msg1" {
		t.Fatalf("pending payload message = %q, want msg1", got)
	}
}

func TestEnqueue_FailedRetry(t *testing.T) {
	repository := testRepository(t)
	ctx := t.Context()

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

	if cnt := countByStatus(ctx, t, repository, domain.DeliveryStatusPending); cnt != 1 {
		t.Fatalf("expected 1 pending after retry, got %d", cnt)
	}

	if got := payloadMessage(ctx, t, repository, "2026-W08:room1"); got != "retry-msg" {
		t.Fatalf("rearmed payload message = %q, want retry-msg", got)
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
			ctx := t.Context()

			if err := repository.EnqueueBatch(ctx, buildOutboxBatchItems(tc.count)); err != nil {
				t.Fatalf("enqueue batch: %v", err)
			}

			if pending := countByStatus(ctx, t, repository, domain.DeliveryStatusPending); pending != int64(tc.count) {
				t.Fatalf("pending count = %d, want %d", pending, tc.count)
			}
		})
	}
}
