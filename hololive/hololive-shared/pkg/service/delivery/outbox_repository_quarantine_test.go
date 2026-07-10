package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func markStaleSending(t *testing.T, repository *OutboxRepository, ctx context.Context) int64 {
	t.Helper()

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
	return items[0].ID
}

func TestQuarantineStaleSending_SeparatesUnknownOutcomeFromFailed(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()
	id := markStaleSending(t, repository, ctx)

	quarantined, err := repository.QuarantineStaleSending(ctx, time.Minute, 10)
	if err != nil {
		t.Fatalf("quarantine stale sending: %v", err)
	}
	if quarantined != 1 {
		t.Fatalf("quarantined = %d, want 1", quarantined)
	}

	if failed := countByStatus(t, repository, ctx, domain.DeliveryStatusFailed); failed != 0 {
		t.Fatalf("결과 불명(stale SENDING)은 확정 실패(FAILED)와 분리되어야 한다, failed=%d", failed)
	}
	if q := countByStatus(t, repository, ctx, deliveryStatusQuarantined); q != 1 {
		t.Fatalf("stale SENDING must move to QUARANTINED, quarantined=%d", q)
	}

	var status string
	var errText string
	var locksCleared bool
	if err := repository.pool.QueryRow(ctx,
		`SELECT status, error, locked_at IS NULL AND locked_by IS NULL AND lock_expires_at IS NULL
		 FROM notification_delivery_outbox WHERE id = $1`, id,
	).Scan(&status, &errText, &locksCleared); err != nil {
		t.Fatalf("query quarantined row: %v", err)
	}
	if status != string(deliveryStatusQuarantined) {
		t.Fatalf("status = %s, want QUARANTINED", status)
	}
	if !strings.Contains(errText, "stale sending") {
		t.Fatalf("error = %q, want stale sending marker", errText)
	}
	if !locksCleared {
		t.Fatal("stale SENDING quarantine must clear locks")
	}
}

func TestQuarantineStaleSending_QuarantinedRowDoesNotRearmOnEnqueue(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()
	markStaleSending(t, repository, ctx)

	if _, err := repository.QuarantineStaleSending(ctx, time.Minute, 10); err != nil {
		t.Fatalf("quarantine stale sending: %v", err)
	}

	if err := repository.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "2026-W08", "room1", "rearm-msg"); err != nil {
		t.Fatalf("re-enqueue: %v", err)
	}

	if pending := countByStatus(t, repository, ctx, domain.DeliveryStatusPending); pending != 0 {
		t.Fatalf("결과 불명 행의 rearm은 중복 노출 위험이라 차단되어야 한다(rearm은 FAILED 전용), pending=%d", pending)
	}
	if q := countByStatus(t, repository, ctx, deliveryStatusQuarantined); q != 1 {
		t.Fatalf("row must stay QUARANTINED, quarantined=%d", q)
	}
}

func TestCleanup_QuarantinedItems(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()
	id := markStaleSending(t, repository, ctx)

	if _, err := repository.QuarantineStaleSending(ctx, time.Minute, 10); err != nil {
		t.Fatalf("quarantine stale sending: %v", err)
	}
	if _, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET created_at = $1 WHERE id = $2",
		time.Now().Add(-10*24*time.Hour), id,
	); err != nil {
		t.Fatalf("backdate created_at: %v", err)
	}

	cleaned, err := repository.Cleanup(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("QUARANTINED 행도 retention 삭제 대상이어야 무한 누적이 없다, cleaned=%d", cleaned)
	}
}

func TestCleanup_DeletesInBatches(t *testing.T) {
	repository := testRepository(t)
	ctx := context.Background()

	if err := repository.EnqueueBatch(ctx, buildOutboxBatchItems(3)); err != nil {
		t.Fatalf("enqueue batch: %v", err)
	}
	if _, err := repository.pool.Exec(ctx,
		"UPDATE notification_delivery_outbox SET status = 'SENT', sent_at = $1",
		time.Now().Add(-10*24*time.Hour),
	); err != nil {
		t.Fatalf("backdate sent rows: %v", err)
	}

	cleaned, err := repository.cleanupInBatches(ctx, time.Now().Add(-7*24*time.Hour), 1)
	if err != nil {
		t.Fatalf("cleanup in batches: %v", err)
	}
	if cleaned != 3 {
		t.Fatalf("batch loop must drain all eligible rows, cleaned=%d", cleaned)
	}

	var remaining int
	if err := repository.pool.QueryRow(ctx,
		"SELECT count(*) FROM notification_delivery_outbox",
	).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining = %d, want 0", remaining)
	}
}
