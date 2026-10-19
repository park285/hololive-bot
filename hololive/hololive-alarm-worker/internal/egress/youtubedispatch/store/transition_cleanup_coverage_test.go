package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	dbtest "github.com/kapu/hololive-dbtest"
)

// 만료된 SENDING owner를 잠근 sweep과 보존 정리가 서로 다른 형제를 쥐고 상대 행을 기다리면 교착한다.
// 정리는 경쟁 중인 논리 그룹의 증거를 보존하면서, 관계없는 종단 그룹은 같은 배치에서 삭제해야 한다.
func TestTransitionCleanupDoesNotWaitForStaleSweepOwnedLogicalGroup(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	terminalAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	cutoff := terminalAt.Add(24 * time.Hour)

	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "video-cleanup-stale-owner", "room-cleanup-stale-owner")
	markTransitionDeliveriesTerminalFailed(t, pool, terminalAt, followerID)

	_, err := pool.Exec(t.Context(), `
		UPDATE youtube_notification_delivery
		SET status = $1, locked_at = clock_timestamp() - INTERVAL '1 day', locked_by = 'youtube-dispatcher:test'
		WHERE id = $2
	`, string(lifecycle.StatusSending), ownerID)
	require.NoError(t, err)

	unrelatedOwnerID, unrelatedFollowerID := seedTransitionLogicalGroup(t, pool, "video-cleanup-unrelated", "room-cleanup-unrelated")
	markTransitionDeliveriesTerminalFailed(t, pool, terminalAt, unrelatedOwnerID, unrelatedFollowerID)

	// cleanup이 owner 잠금을 기다리면 이 시한에 걸려 실패한다.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	sweep, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackPlanModeSQLTest(t, sweep)

	stale, _, err := loadStaleSendingCandidates(ctx, sweep, transition.config.LockTimeout, 10)
	require.NoError(t, err)
	require.Len(t, stale, 1)
	require.Equal(t, ownerID, stale[0].ID)

	held, err := transition.CleanupTerminalOutboxes(ctx, cutoff, CleanupCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, 3, held.ExaminedOutboxes)
	require.Equal(t, 2, held.DeletedOutboxes)
	require.Equal(t, 1, held.GuardedOutboxes)
	require.Equal(t, map[CleanupGuardReason]int{CleanupGuardIncompleteLogicalGroup: 1}, held.Guards)
	require.Equal(t, 2, countCleanupTestDeliveries(t, pool, ownerID, followerID))
	require.Zero(t, countCleanupTestDeliveries(t, pool, unrelatedOwnerID, unrelatedFollowerID))

	// stale sweep은 cleanup이 남긴 FAILED 증거까지 포함한 논리 그룹을 그대로 판정한다.
	resolved, err := transition.resolveStaleSendingGroups(ctx, sweep, stale)
	require.NoError(t, err)
	require.Contains(t, resolved.rowsByID, followerID)
	rollbackPlanModeSQLTest(t, sweep)

	// 잠금이 풀리면 기존 판정대로 SENDING owner가 FAILED sibling 삭제를 막는다.
	released, err := transition.CleanupTerminalOutboxes(ctx, cutoff, CleanupCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, 1, released.ExaminedOutboxes)
	require.Zero(t, released.DeletedOutboxes)
	require.Equal(t, map[CleanupGuardReason]int{CleanupGuardActiveLogicalGroup: 1}, released.Guards)
	require.Equal(t, 2, countCleanupTestDeliveries(t, pool, ownerID, followerID))
}

// 같은 논리 키 행이 LogicalGroupLimit를 넘으면 sibling 조회가 완전하다고 증명할 수 없으므로, 모두 FAILED인 그룹도
// 지우지 않는다.
func TestTransitionCleanupGuardsLogicalGroupBeyondScanLimit(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)

	transition.config.LogicalGroupLimit = 1

	terminalAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)

	ownerID, followerID := seedTransitionLogicalGroup(t, pool, "video-cleanup-scan-limit", "room-cleanup-scan-limit")
	markTransitionDeliveriesTerminalFailed(t, pool, terminalAt, ownerID, followerID)

	result, err := transition.CleanupTerminalOutboxes(t.Context(), terminalAt.Add(24*time.Hour), CleanupCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, 2, result.ExaminedOutboxes)
	require.Zero(t, result.DeletedOutboxes)
	require.Equal(t, map[CleanupGuardReason]int{CleanupGuardIncompleteLogicalGroup: 2}, result.Guards)
	require.Equal(t, 2, countCleanupTestDeliveries(t, pool, ownerID, followerID))
}

func countCleanupTestDeliveries(t *testing.T, pool *pgxpool.Pool, ids ...int64) int {
	t.Helper()

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT COUNT(*)
		FROM youtube_notification_delivery
		WHERE id = ANY($1::bigint[])
	`, ids).Scan(&count))

	return count
}

// 오래된 과대 그룹이 전체 조회 창을 채워도, 같은 배치의 다른 완전한 논리 키는 정리되어야 한다.
func TestTransitionCleanupOversizedGroupDoesNotStarveOtherKeys(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)

	transition.config.LogicalGroupLimit = 2

	terminalAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	ownerID, followerID := seedOversizedCleanupGroup(t, pool, terminalAt)
	otherOwnerID, otherFollowerID := seedTransitionLogicalGroup(t, pool, "video-cleanup-small", "room-cleanup-small")
	markTransitionDeliveriesTerminalFailed(t, pool, terminalAt, otherOwnerID, otherFollowerID)

	result, err := transition.CleanupTerminalOutboxes(t.Context(), terminalAt.Add(24*time.Hour), CleanupCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, 3, result.ExaminedOutboxes)
	require.Equal(t, 2, result.DeletedOutboxes)
	require.Equal(t, map[CleanupGuardReason]int{CleanupGuardIncompleteLogicalGroup: 1}, result.Guards)
	require.Equal(t, 2, countCleanupTestDeliveries(t, pool, ownerID, followerID))
	require.Zero(t, countCleanupTestDeliveries(t, pool, otherOwnerID, otherFollowerID))
}

func seedOversizedCleanupGroup(t *testing.T, pool *pgxpool.Pool, terminalAt time.Time) (int64, int64) {
	t.Helper()

	const contentID, roomID = "video-cleanup-oversized", "room-cleanup-oversized"

	ownerID, followerID := seedTransitionLogicalGroup(t, pool, contentID, roomID)
	markTransitionDeliveriesTerminalFailed(t, pool, terminalAt, ownerID, followerID)

	_, err := pool.Exec(t.Context(), `
		UPDATE youtube_notification_outbox SET terminal_at=$1
		WHERE id=(SELECT outbox_id FROM youtube_notification_delivery WHERE id=$2)
	`, terminalAt.Add(36*time.Hour), followerID)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `
		UPDATE youtube_notification_delivery SET created_at=$1 WHERE id=ANY($2::BIGINT[])
	`, terminalAt.Add(-24*time.Hour), []int64{ownerID, followerID})
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `
		WITH inserted AS (
			INSERT INTO youtube_notification_outbox
				(kind, channel_id, content_id, payload, status, attempt_count, terminal_at, created_at)
			SELECT 'NEW_VIDEO', 'channel-transition', repeat(' ', padding)||$1||repeat(' ', padding),
			       '{}'::jsonb, 'FAILED', 3, $2, $3
			FROM generate_series(2, 14) AS padding
			RETURNING id
		)
		INSERT INTO youtube_notification_delivery
			(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, error)
		SELECT id, $4, 'FAILED', 3, $2, $3, 'retry exhausted' FROM inserted
	`, contentID, terminalAt.Add(36*time.Hour), terminalAt.Add(-24*time.Hour), roomID)
	require.NoError(t, err)

	return ownerID, followerID
}

func TestTransitionCleanupDeletesTerminalOutboxWithoutChildren(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)

	var outboxID int64

	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, created_at, terminal_at)
		VALUES ('NEW_VIDEO', 'channel-transition', 'cleanup-no-children', '{}'::jsonb, 'FAILED', 3,
		        clock_timestamp()-INTERVAL '3 days', clock_timestamp()-INTERVAL '2 days')
		RETURNING id
	`).Scan(&outboxID))

	result, err := transition.CleanupTerminalOutboxes(t.Context(), time.Now().Add(-24*time.Hour), CleanupCursor{}, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.ExaminedOutboxes)
	require.Equal(t, 1, result.DeletedOutboxes)
	require.Zero(t, result.GuardedOutboxes)

	var remaining bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS (
		SELECT 1 FROM youtube_notification_outbox WHERE id=$1)`, outboxID).Scan(&remaining))
	require.False(t, remaining)
}
