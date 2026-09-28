package telemetry

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type fetchLockTelemetrySeed struct {
	deliveryID    int64
	eventAt       time.Time
	nextAttemptAt time.Time
	lockedAt      *time.Time
	loggedAt      *time.Time
}

func insertFetchLockTelemetryRow(t *testing.T, pool *pgxpool.Pool, seed fetchLockTelemetrySeed) int64 {
	t.Helper()

	var id int64

	if err := pool.QueryRow(t.Context(), `
		INSERT INTO youtube_notification_delivery_telemetry
			(delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id, alarm_type,
			 dedupe_key, delivery_mode, send_result, event_at, next_attempt_at, locked_at, logged_at)
		VALUES ($1, 1, 1, 'UC_fetch_lock', 'post-fetch-lock', 'post-fetch-lock', 'room-1', $2,
			 $3, 'grouped', 'success', $4, $5, $6, $7)
		RETURNING id
	`, seed.deliveryID, string(domain.AlarmTypeCommunity), fmt.Sprintf("dedupe-fetch-lock-%d", seed.deliveryID),
		seed.eventAt, seed.nextAttemptAt, seed.lockedAt, seed.loggedAt).Scan(&id); err != nil {
		t.Fatalf("insert telemetry row delivery_id=%d: %v", seed.deliveryID, err)
	}

	return id
}

func fetchedTelemetryIDs(rows []domain.YouTubeNotificationDeliveryTelemetry) []int64 {
	ids := make([]int64, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}

	return ids
}

// 한 문장 lease 획득이 기존 적격 조건(미기록·재시도 시각 도래·lease 만료)과 (event_at, id) 순서, 배치 한도를 지키는지 확인한다.
func TestFetchAndLockPendingLeasesEligibleRowsInEventOrder(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	base := now.Add(-time.Hour)
	expiredLock := now.Add(-2 * time.Hour)
	freshLock := now.Add(-time.Second)
	loggedAt := now.Add(-time.Minute)

	third := insertFetchLockTelemetryRow(t, pool, fetchLockTelemetrySeed{deliveryID: 1, eventAt: base.Add(3 * time.Minute), nextAttemptAt: base})
	first := insertFetchLockTelemetryRow(t, pool, fetchLockTelemetrySeed{deliveryID: 2, eventAt: base.Add(time.Minute), nextAttemptAt: base})
	second := insertFetchLockTelemetryRow(t, pool, fetchLockTelemetrySeed{deliveryID: 3, eventAt: base.Add(2 * time.Minute), nextAttemptAt: base, lockedAt: &expiredLock})
	insertFetchLockTelemetryRow(t, pool, fetchLockTelemetrySeed{deliveryID: 4, eventAt: base, nextAttemptAt: base, lockedAt: &freshLock})
	insertFetchLockTelemetryRow(t, pool, fetchLockTelemetrySeed{deliveryID: 5, eventAt: base, nextAttemptAt: base, loggedAt: &loggedAt})
	insertFetchLockTelemetryRow(t, pool, fetchLockTelemetrySeed{deliveryID: 6, eventAt: base, nextAttemptAt: now.Add(time.Hour)})

	batch, err := repository.FetchAndLockPending(ctx, 2, time.Minute)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	if got, want := fetchedTelemetryIDs(batch), []int64{first, second}; !slices.Equal(got, want) {
		t.Fatalf("first fetch ids = %v, want %v", got, want)
	}

	for i := range batch {
		if batch[i].LockedAt == nil || batch[i].LockedAt.Before(now.Add(-time.Second)) {
			t.Fatalf("row %d locked_at = %v, want a fresh lease", batch[i].ID, batch[i].LockedAt)
		}

		if batch[i].DeliveryID == 0 || batch[i].PostID != "post-fetch-lock" || batch[i].AlarmType != domain.AlarmTypeCommunity {
			t.Fatalf("row %d scanned with shifted columns: %+v", batch[i].ID, batch[i])
		}
	}

	rest, err := repository.FetchAndLockPending(ctx, 10, time.Minute)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}

	if got, want := fetchedTelemetryIDs(rest), []int64{third}; !slices.Equal(got, want) {
		t.Fatalf("second fetch ids = %v, want %v", got, want)
	}

	none, err := repository.FetchAndLockPending(ctx, 10, time.Minute)
	if err != nil {
		t.Fatalf("third fetch: %v", err)
	}

	if len(none) != 0 {
		t.Fatalf("third fetch ids = %v, want none while leases are fresh", fetchedTelemetryIDs(none))
	}
}

// 다른 트랜잭션이 행 잠금을 쥔 행은 기다리지 않고 건너뛰고, 잠금이 풀리면 다음 호출에서 획득한다.
func TestFetchAndLockPendingSkipsRowsLockedByAnotherTransaction(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	base := now.Add(-time.Hour)

	held := insertFetchLockTelemetryRow(t, pool, fetchLockTelemetrySeed{deliveryID: 1, eventAt: base, nextAttemptAt: base})
	free := insertFetchLockTelemetryRow(t, pool, fetchLockTelemetrySeed{deliveryID: 2, eventAt: base.Add(time.Minute), nextAttemptAt: base})

	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocker: %v", err)
	}

	t.Cleanup(func() {
		if rollbackErr := blocker.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback blocker: %v", rollbackErr)
		}
	})

	var lockedID int64

	err = blocker.QueryRow(ctx, `
		SELECT id FROM youtube_notification_delivery_telemetry WHERE id = $1 FOR UPDATE
	`, held).Scan(&lockedID)
	if err != nil {
		t.Fatalf("hold row lock: %v", err)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	skipped, err := repository.FetchAndLockPending(fetchCtx, 10, time.Minute)
	if err != nil {
		t.Fatalf("fetch while another transaction holds a row lock: %v", err)
	}

	if got, want := fetchedTelemetryIDs(skipped), []int64{free}; !slices.Equal(got, want) {
		t.Fatalf("fetch ids while locked = %v, want %v", got, want)
	}

	err = blocker.Rollback(ctx)
	if err != nil {
		t.Fatalf("release blocker: %v", err)
	}

	released, err := repository.FetchAndLockPending(ctx, 10, time.Minute)
	if err != nil {
		t.Fatalf("fetch after release: %v", err)
	}

	if got, want := fetchedTelemetryIDs(released), []int64{held}; !slices.Equal(got, want) {
		t.Fatalf("fetch ids after release = %v, want %v", got, want)
	}
}
