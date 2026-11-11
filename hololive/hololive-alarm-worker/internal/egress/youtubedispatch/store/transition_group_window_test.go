package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPrepareClaimedRejectsIncompleteGroupWindow(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "window-prepare", "room-window-prepare")
	seedLogicalGroupWindowNoise(t, pool, "window-prepare", "room-window-prepare", 11)

	claimed, err := transition.ClaimPending(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, ownerID, claimed[0].ID)

	outboxes := loadTransitionTestOutboxes(t, pool, claimed)

	_, err = transition.PrepareClaimed(t.Context(), claimed, outboxes)
	require.Error(t, err)
	require.Equal(t, claimed[0].Status, loadHandoffTestDelivery(t, pool, ownerID).status)
	require.Equal(t, domain.OutboxStatusPending, loadHandoffTestDelivery(t, pool, followerID).status)
}

func TestStaleQuarantineRejectsIncompleteGroupWindow(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "window-stale", "room-window-stale")
	seedLogicalGroupWindowNoise(t, pool, "window-stale", "room-window-stale", 11)

	_, err := pool.Exec(t.Context(), `UPDATE youtube_notification_delivery
		SET status='SENDING', created_at=clock_timestamp()-INTERVAL '2 days',
		    locked_at=clock_timestamp()-INTERVAL '1 day', locked_by='youtube-dispatcher:test', row_version=1
		WHERE id=$1`, ownerID)
	require.NoError(t, err)

	_, err = transition.QuarantineStaleLogicalGroups(t.Context(), 1)
	require.Error(t, err)
	require.Equal(t, DeliveryStatusSending, loadHandoffTestDelivery(t, pool, ownerID).status)
	require.Equal(t, domain.OutboxStatusPending, loadHandoffTestDelivery(t, pool, followerID).status)

	var ledgerRows int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_notification_delivery_ledger
		WHERE source_delivery_id=$1`, ownerID).Scan(&ledgerRows))
	require.Zero(t, ledgerRows)
}

// stale sweep에는 claim 행이 없으므로 조회 창에 걸린 깨진 정체성 sibling은 오류가 아니라 그룹 밖으로 건너뛴다.
func TestStaleQuarantineSkipsMalformedSiblingIdentity(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	createdAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)

	owner := seedGroupQueryOutbox(t, pool, domain.OutboxKindNewShort, "stale-malformed", `{"canonical_post_id":"short:stale-malformed"}`)
	ownerDelivery := seedGroupQueryDelivery(t, pool, owner.ID, "room-stale-malformed", createdAt)
	malformed := seedGroupQueryOutbox(t, pool, domain.OutboxKindNewShort, " stale-malformed ", `{"canonical_post_id":null}`)
	malformedDelivery := seedGroupQueryDelivery(t, pool, malformed.ID, "room-stale-malformed", createdAt)
	markStaleSendingOwner(t, pool, ownerDelivery.ID)

	result, err := transition.QuarantineStaleLogicalGroups(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Equal(t, 1, result.QuarantinedDeliveries)
	require.Empty(t, result.Blocked)
	require.Equal(t, DeliveryStatusQuarantined, loadHandoffTestDelivery(t, pool, ownerDelivery.ID).status)
	require.Equal(t, domain.OutboxStatusPending, loadHandoffTestDelivery(t, pool, malformedDelivery.ID).status)
}

// stale sweep도 claim 경로처럼 완전한 조회 창 안에서 한 키의 행이 LogicalGroupLimit를 넘으면 아무 행도 바꾸지 않는다.
func TestStaleQuarantineRejectsGroupBeyondLogicalLimit(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)

	transition.config.LogicalGroupLimit = 1

	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "limit-stale", "room-limit-stale")
	markStaleSendingOwner(t, pool, ownerID)

	_, err := transition.QuarantineStaleLogicalGroups(t.Context(), 1)
	require.ErrorContains(t, err, "exceeds limit 1")
	require.Equal(t, DeliveryStatusSending, loadHandoffTestDelivery(t, pool, ownerID).status)
	require.Equal(t, domain.OutboxStatusPending, loadHandoffTestDelivery(t, pool, followerID).status)
}

func markStaleSendingOwner(t *testing.T, pool *pgxpool.Pool, deliveryID int64) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `UPDATE youtube_notification_delivery
		SET status='SENDING', locked_at=clock_timestamp()-INTERVAL '1 day',
		    locked_by='youtube-dispatcher:test', row_version=1
		WHERE id=$1`, deliveryID)
	require.NoError(t, err)
}
