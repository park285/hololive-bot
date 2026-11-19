package youtubedispatch

import (
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func cleanupTestClaimManager(t *testing.T, db *pgxpool.Pool, cfg *Config) *ClaimManager {
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
		Owner: testTransitionOwner, MaxRetries: 3, RetryBackoff: time.Minute, LockTimeout: lockTimeout,
		ClaimFreshnessWindow: claimFreshnessWindow, LogicalGroupLimit: 100,
	})
	require.NoError(t, err)

	return newClaimManager(db, logger, cfg, store.NewDeliveryRepository(db, logger), transition, nil, nil, nil)
}

func outboxRowCount(t *testing.T, db *pgxpool.Pool, id int64) int64 {
	t.Helper()

	var count int64

	require.NoError(t, countDeliveryTestRowsWhere(db, &domain.YouTubeNotificationOutbox{}, &count, "id = $1", id).Error)

	return count
}

func TestLifecycleCleanupRemovesOnlyUnclaimableOrphan(t *testing.T) {
	db := newDeliveryPool(t)
	cm := cleanupTestClaimManager(t, db, &Config{
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

func TestPendingExpiryCountsCommittedStaleBeforeProjectionFailure(t *testing.T) {
	db := newDeliveryPool(t)
	ctx := t.Context()
	cm := cleanupTestClaimManager(t, db, &Config{
		BatchSize: 10, ClaimFreshnessWindow: 2 * time.Hour, LockTimeout: time.Minute,
	})
	totals := &workercontract.Counters{}
	dispatcher := &Dispatcher{claim: cm}
	dispatcher.SetWorkerInstrumentation(nil, totals)

	var outboxID int64

	require.NoError(t, db.QueryRow(ctx, `INSERT INTO youtube_notification_outbox
		(kind,channel_id,content_id,payload,created_at)
		VALUES ('NEW_VIDEO','expiry-channel','expiry-discard','{}',now()-INTERVAL '3 hours') RETURNING id`).Scan(&outboxID))

	_, err := db.Exec(ctx, `INSERT INTO youtube_notification_delivery (outbox_id,room_id,status)
		VALUES ($1,'expiry-room','PENDING')`, outboxID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `CREATE FUNCTION reject_expiry_projection() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'projection unavailable'; END $$;
		CREATE TRIGGER reject_expiry_projection BEFORE UPDATE ON youtube_notification_outbox
		FOR EACH ROW EXECUTE FUNCTION reject_expiry_projection()`)
	require.NoError(t, err)
	cm.expirePendingDeliveries(ctx)

	var status string

	require.NoError(t, db.QueryRow(ctx, `SELECT status FROM youtube_notification_delivery WHERE outbox_id=$1`, outboxID).Scan(&status))
	require.Equal(t, "FAILED", status)

	expected := &workercontract.Counters{}
	expected.RecordDiscard(workercontract.DiscardStale)
	require.Equal(t, expected.Snapshot(), totals.Snapshot())
	cm.expirePendingDeliveries(ctx)
	require.Equal(t, expected.Snapshot(), totals.Snapshot(), "repeat expiry does not double count")
}
