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

package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	dispatchstate "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

func cleanupTestClaimManager(db *deliveryTestDB, cfg *dispatchstate.Config) *ClaimManager {
	logger := slog.Default()

	return &ClaimManager{
		db:       store.AsDeliveryDB(db),
		config:   *cfg,
		logger:   logger,
		delivery: store.NewDeliveryRepository(db, logger),
	}
}

func outboxRowCount(t *testing.T, db *deliveryTestDB, id int64) int64 {
	t.Helper()

	var count int64

	require.NoError(t, countDeliveryTestRowsWhere(db, &domain.YouTubeNotificationOutbox{}, &count, "id = ?", id).Error)

	return count
}

func TestCompatibilityCleanupPreservesOutboxWhileLedgerStateIsIncomplete(t *testing.T) {
	db := newDeliveryPool(t)
	cm := cleanupTestClaimManager(db, &dispatchstate.Config{
		CleanupAfter:         7 * 24 * time.Hour,
		ClaimFreshnessWindow: 2 * time.Hour,
		LockTimeout:          5 * time.Minute,
	})
	ctx := t.Context()

	now := time.Now().UTC()
	veryOld := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-5 * time.Minute)
	liveLock := now.Add(-1 * time.Minute)

	newPending := func(contentID string, createdAt time.Time, lockedAt *time.Time) *domain.YouTubeNotificationOutbox {
		row := &domain.YouTubeNotificationOutbox{
			Kind: domain.OutboxKindNewVideo, ChannelID: "ch-clean", ContentID: contentID,
			Payload: `{"id":"` + contentID + `"}`, Status: domain.OutboxStatusPending,
			NextAttemptAt: createdAt, CreatedAt: createdAt, LockedAt: lockedAt,
		}
		require.NoError(t, insertDeliveryTestRows(db, row).Error)

		return row
	}

	orphan := newPending("orphan-old", veryOld, nil)
	freshPending := newPending("fresh-pending", recent, nil)
	lockedOrphan := newPending("locked-orphan", veryOld, &liveLock)

	orphanWithDelivery := newPending("orphan-with-delivery", veryOld, nil)
	require.NoError(t, insertDeliveryTestRows(db, &domain.YouTubeNotificationDelivery{
		OutboxID: orphanWithDelivery.ID, RoomID: testRoomOne, Status: domain.OutboxStatusPending,
	}).Error)

	cm.cleanupOutbox(ctx)

	assert.Equal(t, int64(1), outboxRowCount(t, db, orphan.ID), "incomplete ledger에서는 orphan cleanup도 동결")
	assert.Equal(t, int64(1), outboxRowCount(t, db, freshPending.ID), "incomplete ledger에서는 최근 PENDING도 보존")
	assert.Equal(t, int64(1), outboxRowCount(t, db, lockedOrphan.ID), "incomplete ledger에서는 lock 상태와 무관하게 보존")
	assert.Equal(t, int64(1), outboxRowCount(t, db, orphanWithDelivery.ID), "incomplete ledger에서는 delivery가 있는 PENDING도 보존")

	var deliveryCount int64

	require.NoError(t, countDeliveryTestRowsWhere(db, &domain.YouTubeNotificationDelivery{}, &deliveryCount, "outbox_id = ?", orphanWithDelivery.ID).Error)
	assert.Equal(t, int64(1), deliveryCount, "보존된 PENDING의 delivery 행도 CASCADE로 삭제되지 않음")
}

func TestCompatibilityCleanupRemainsFrozenWithCompletedLedgerState(t *testing.T) {
	db := newDeliveryPool(t)
	cm := cleanupTestClaimManager(db, &dispatchstate.Config{
		CleanupAfter:         1 * time.Hour,
		ClaimFreshnessWindow: 2 * time.Hour,
		LockTimeout:          5 * time.Minute,
	})
	ctx := t.Context()

	now := time.Now().UTC()
	_, err := db.Exec(ctx, `
		INSERT INTO youtube_notification_delivery_ledger_state (
			singleton, schema_version, delivery_high_water_id, outbox_high_water_id,
			delivery_cursor_id, delivery_verify_cursor_id, outbox_cursor_id,
			legacy_coverage_start_at, coverage_verified_at, started_at, completed_at, updated_at
		) VALUES (true, $1, 0, 0, 0, 0, 0, $2, $3, $4, $5, $6)
	`, store.LedgerSchemaVersion, now, now, now, now, now)
	require.NoError(t, err)

	betweenCutoffs := now.Add(-90 * time.Minute)
	beyondFreshness := now.Add(-3 * time.Hour)

	newPending := func(contentID string, createdAt time.Time) *domain.YouTubeNotificationOutbox {
		row := &domain.YouTubeNotificationOutbox{
			Kind: domain.OutboxKindNewVideo, ChannelID: "ch-max", ContentID: contentID,
			Payload: `{"id":"` + contentID + `"}`, Status: domain.OutboxStatusPending,
			NextAttemptAt: createdAt, CreatedAt: createdAt,
		}
		require.NoError(t, insertDeliveryTestRows(db, row).Error)

		return row
	}

	stillClaimable := newPending("still-claimable", betweenCutoffs)
	pastFreshness := newPending("past-freshness", beyondFreshness)

	cm.cleanupOutbox(ctx)

	assert.Equal(t, int64(1), outboxRowCount(t, db, stillClaimable.ID),
		"compatibility binary는 completed state가 있어도 cleanup을 활성화하지 않음")
	assert.Equal(t, int64(1), outboxRowCount(t, db, pastFreshness.ID),
		"compatibility binary는 T08 ledger-aware cleanup 전까지 모든 outbox cleanup을 동결")
}
