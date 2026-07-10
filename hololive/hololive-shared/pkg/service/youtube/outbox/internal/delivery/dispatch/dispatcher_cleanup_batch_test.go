package dispatch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCleanupOutbox_DeletesTerminalRowsInBatches(t *testing.T) {
	db := newDeliveryPool(t)
	cm := cleanupTestClaimManager(db, &Config{
		CleanupAfter: 7 * 24 * time.Hour,
		LockTimeout:  5 * time.Minute,
	})
	ctx := context.Background()

	veryOld := time.Now().UTC().Add(-30 * 24 * time.Hour)
	for i := range 3 {
		row := &domain.YouTubeNotificationOutbox{
			Kind: domain.OutboxKindNewVideo, ChannelID: "ch-batch", ContentID: fmt.Sprintf("batch-sent-%d", i),
			Payload: "{}", Status: domain.OutboxStatusSent,
			NextAttemptAt: veryOld, CreatedAt: veryOld, SentAt: &veryOld,
		}
		require.NoError(t, insertDeliveryTestRows(db, row).Error)
	}

	deleted, err := cm.deleteTerminalOutboxBatches(ctx, veryOld.Add(24*time.Hour), 1)

	require.NoError(t, err)
	assert.Equal(t, int64(3), deleted, "배치 크기보다 많은 terminal outbox도 루프로 전량 삭제해야 한다")

	var remaining int64
	require.NoError(t, countDeliveryTestRowsWhere(db, &domain.YouTubeNotificationOutbox{}, &remaining, "channel_id = ?", "ch-batch").Error)
	assert.Zero(t, remaining)
}
