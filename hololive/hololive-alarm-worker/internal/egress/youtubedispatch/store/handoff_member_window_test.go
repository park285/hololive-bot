package store

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestHandoffSettlementClosesWhenOwnerFallsOutsideLockedWindow(t *testing.T) {
	requireHandoffWindowRejection(t, 12)
}

func TestHandoffSettlementClosesWhenFollowerFallsOutsideLockedWindow(t *testing.T) {
	requireHandoffWindowRejection(t, 11)
}

// 후보 별칭이 조회 창을 채우면 owner가 잠겼더라도 전체 논리 그룹의 정산을 허용하면 안 된다.
func requireHandoffWindowRejection(t *testing.T, aliasRows int) {
	t.Helper()

	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "handoff-window-omission", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	seedLogicalGroupWindowNoise(t, pool, unit.contentID, unit.roomID, aliasRows)

	result, err := transition.ReissueAfterHandoff(t.Context(), &claim, handoffcheck.CodeFailed, "iris reply status failed")
	require.ErrorIs(t, err, ErrHandoffCheckClosed)
	require.Equal(t, ApplyConflict, result.Outcome)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.ownerID).status)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.followerID).status)

	status, source := loadHandoffTestLedger(t, pool, unit)
	require.Equal(t, LedgerStatusSent, status)
	require.Equal(t, unit.ownerID, source)

	request := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.Zero(t, request.Generation)
	require.NotNil(t, request.CheckedAt)
}

func seedLogicalGroupWindowNoise(t *testing.T, pool *pgxpool.Pool, contentID, roomID string, aliasRows int) {
	t.Helper()

	// NEW_VIDEO는 content_id가 정본이다. 여기서는 payload 후보로만 걸리는 다른 키가 한도 창을 차지한다.
	_, err := pool.Exec(t.Context(), `
		WITH inserted AS (
			INSERT INTO youtube_notification_outbox(kind, channel_id, content_id, payload, status, attempt_count, created_at, terminal_at)
			SELECT 'NEW_VIDEO', 'channel-transition', 'group-window-other-'||n,
			       jsonb_build_object('canonical_post_id', $1::TEXT), 'FAILED', 3,
			       clock_timestamp()-INTERVAL '1 day', clock_timestamp()-INTERVAL '1 hour'
			FROM generate_series(1, $3::INTEGER) AS n
			RETURNING id
		)
		INSERT INTO youtube_notification_delivery(outbox_id, room_id, status, attempt_count, created_at)
		SELECT id, $2, 'FAILED', 3, clock_timestamp()-INTERVAL '1 day' FROM inserted
	`, contentID, roomID, aliasRows)
	require.NoError(t, err)
}
