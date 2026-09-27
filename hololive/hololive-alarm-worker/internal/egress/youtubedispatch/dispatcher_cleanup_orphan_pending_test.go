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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func cleanupTestClaimManager(t *testing.T, db *pgxpool.Pool, cfg *dispatchstate.Config) *ClaimManager {
	t.Helper()

	logger := slog.Default()
	lockTimeout := cfg.LockTimeout

	if lockTimeout <= 0 {
		lockTimeout = time.Minute
	}

	claimFreshnessWindow := cfg.ClaimFreshnessWindow
	if claimFreshnessWindow <= 0 {
		claimFreshnessWindow = 2 * time.Hour
	}

	transition, err := store.NewTransitionStore(db, logger, store.TransitionConfig{
		MaxRetries: 3, RetryBackoff: time.Minute, LockTimeout: lockTimeout,
		ClaimFreshnessWindow: claimFreshnessWindow, LogicalGroupLimit: 100,
	})
	require.NoError(t, err)

	return &ClaimManager{
		db:         store.AsDeliveryDB(db),
		config:     *cfg,
		logger:     logger,
		delivery:   store.NewDeliveryRepository(db, logger),
		transition: transition,
	}
}

func outboxRowCount(t *testing.T, db *pgxpool.Pool, id int64) int64 {
	t.Helper()

	var count int64

	require.NoError(t, countDeliveryTestRowsWhere(db, &domain.YouTubeNotificationOutbox{}, &count, "id = ?", id).Error)

	return count
}

func TestLifecycleCleanupRemovesOnlyUnclaimableOrphan(t *testing.T) {
	db := newDeliveryPool(t)
	cm := cleanupTestClaimManager(t, db, &dispatchstate.Config{
		CleanupAfter:         1 * time.Hour,
		ClaimFreshnessWindow: 2 * time.Hour,
		LockTimeout:          5 * time.Minute,
	})
	ctx := t.Context()

	now := time.Now().UTC()
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
		"primary freshness window 안의 PENDING outbox는 보존")
	assert.Equal(t, int64(0), outboxRowCount(t, db, pastFreshness.ID),
		"primary가 다시 claim할 수 없는 delivery 없는 PENDING outbox는 정리")
}
