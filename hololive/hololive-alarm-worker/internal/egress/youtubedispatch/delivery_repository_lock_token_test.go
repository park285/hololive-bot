package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
)

func TestDeliveryRepositoryMarkPermanentFailureBatchIfLockedSkipsRowsRelockedByAnotherWorker(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	originalLockedAt := now
	replacementLockedAt := now.Add(time.Millisecond)

	row := deliveryTestDeliveryModel{
		OutboxID:      1,
		RoomID:        "room-relocked-permanent-failure",
		Status:        string(store.DeliveryStatusSending),
		AttemptCount:  0,
		NextAttemptAt: now,
		CreatedAt:     now,
		LockedAt:      &originalLockedAt,
	}
	require.NoError(t, insertDeliveryTestRows(db, &row).Error)

	require.NoError(t, updateDeliveryTestRowsWhere(db,
		&deliveryTestDeliveryModel{},
		map[string]any{"locked_at": replacementLockedAt},
		"id = ?", row.ID,
	).Error)

	repository := store.NewDeliveryRepository(db, slog.New(slog.DiscardHandler))
	err := repository.MarkPermanentFailureBatchIfLocked(ctx, []store.LockToken{
		store.NewLockToken(row.ID, &originalLockedAt),
	}, 3, "auth")
	require.NoError(t, err)

	var updated deliveryTestDeliveryModel

	require.NoError(t, firstDeliveryTestRow(db, &updated, row.ID).Error)
	require.Equal(t, string(store.DeliveryStatusSending), updated.Status)
	require.Equal(t, 0, updated.AttemptCount)
	require.NotNil(t, updated.LockedAt)
	require.Equal(t, replacementLockedAt, updated.LockedAt.UTC())
	require.Empty(t, updated.Error)
}
