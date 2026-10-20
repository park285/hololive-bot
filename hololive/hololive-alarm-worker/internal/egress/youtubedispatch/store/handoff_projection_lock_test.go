package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 보존 정리는 부모를 먼저 잠그므로 handoff도 부모를 기다리는 동안 자식 잠금을 쥐면 안 된다.
func TestHandoffSettlementWaitsForParentBeforeLockingChildren(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "handoff-parent-order", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	cleanup, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackPlanModeSQLTest(t, cleanup)

	_, err = cleanup.Exec(ctx, `SELECT id FROM youtube_notification_outbox
		WHERE id=(SELECT outbox_id FROM youtube_notification_delivery WHERE id=$1) FOR UPDATE`, unit.ownerID)
	require.NoError(t, err)

	var workers sync.WaitGroup

	done := make(chan error, 1)

	workers.Go(func() {
		_, settleErr := transition.ExhaustAfterHandoff(ctx, &claim, handoffcheck.CodeFailed, "failed without budget")
		done <- settleErr
	})

	defer func() { cancel(); workers.Wait() }()

	waitHandoffBlockedBy(ctx, t, pool, cleanup)

	_, err = cleanup.Exec(ctx, `SELECT id FROM youtube_notification_delivery WHERE id=$1 FOR UPDATE NOWAIT`, unit.ownerID)
	require.NoError(t, err, "부모 대기 중 자식을 잠그면 보존 정리의 부모→자식 순서와 순환 대기가 생긴다")
	require.NoError(t, cleanup.Commit(ctx))
	require.NoError(t, <-done)
}

// 정산이 부모 집합을 미리 읽어 잠그는 사이 같은 논리 키의 새 follower·outbox가 들어오면, 자식 잠금을 쥔 채 새 부모를
// 잠그지 않고 아무것도 바꾸지 않은 membership 미스로 끝나며 확인을 닫지 않는다. 다음 확인은 늘어난 집합으로 정산한다.
func TestHandoffSettlementDefersWhenLogicalGroupGainsParent(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "handoff-parent-growth", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackPlanModeSQLTest(t, blocker)

	_, err = blocker.Exec(ctx, `SELECT id FROM youtube_notification_outbox
		WHERE id=(SELECT outbox_id FROM youtube_notification_delivery WHERE id=$1) FOR UPDATE`, unit.ownerID)
	require.NoError(t, err)

	type settled struct {
		result ApplyResult
		err    error
	}

	var workers sync.WaitGroup

	done := make(chan settled, 1)

	workers.Go(func() {
		result, settleErr := transition.ExhaustAfterHandoff(ctx, &claim, handoffcheck.CodeFailed, "failed without budget")
		done <- settled{result: result, err: settleErr}
	})

	defer func() { cancel(); workers.Wait() }()

	// 정산은 부모 id를 사전 조회한 뒤 부모 잠금에서 기다린다. 그 사이 같은 논리 키의 SENT follower가 새 outbox로 들어온다.
	waitHandoffBlockedBy(ctx, t, pool, blocker)

	addedOutboxID, addedID := insertSentHandoffFollower(ctx, t, pool, unit)
	require.NoError(t, blocker.Commit(ctx))

	deferred := <-done
	require.NotErrorIs(t, deferred.err, ErrHandoffCheckClosed)
	require.Equal(t, ApplyConflict, deferred.result.Outcome)

	requireHandoffGroupGrowthDeferred(t, pool, unit, claim, addedID, deferred.err)

	next := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)
	result, err := transition.ExhaustAfterHandoff(t.Context(), &next, handoffcheck.CodeFailed, "failed without budget")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)
	require.Contains(t, result.TouchedOutboxIDs, addedOutboxID)
	require.Len(t, result.TouchedOutboxIDs, 3)

	for _, id := range []int64{unit.ownerID, unit.followerID, addedID} {
		require.Equal(t, domain.OutboxStatusFailed, loadHandoffTestDelivery(t, pool, id).status)
	}

	requireHandoffOutboxStatuses(t, pool, result.TouchedOutboxIDs, domain.OutboxStatusFailed)

	status, _ := loadHandoffTestLedger(t, pool, unit)
	require.Empty(t, status)

	closed := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.Nil(t, closed.NextCheckAt)
	require.NotNil(t, closed.CheckedAt)
}

func requireHandoffGroupGrowthDeferred(t *testing.T, pool *pgxpool.Pool, unit handoffTestUnit, claim HandoffClaim, addedID int64, err error) {
	t.Helper()

	miss, ok := errors.AsType[*sendfence.MissError](err)
	require.True(t, ok, "error %v is not a send fence miss", err)
	require.Equal(t, []sendfence.RowMiss{{
		ID: unit.ownerID, Reason: sendfence.ReasonMembershipChanged, Status: string(domain.OutboxStatusSent),
	}}, miss.Rows)

	for _, id := range []int64{unit.ownerID, unit.followerID, addedID} {
		require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, id).status)
	}

	status, source := loadHandoffTestLedger(t, pool, unit)
	require.Equal(t, LedgerStatusSent, status)
	require.Equal(t, unit.ownerID, source)

	open := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.NotNil(t, open.NextCheckAt)
	require.True(t, open.NextCheckAt.Equal(claim.CheckToken), "claim이 미룬 soft claim 토큰이 그대로 남아야 한다")
	require.Nil(t, open.CheckedAt)
}

// waitHandoffBlockedBy는 blocker 트랜잭션의 잠금을 기다리는 backend가 생길 때까지 기다린다.
func waitHandoffBlockedBy(ctx context.Context, t *testing.T, pool *pgxpool.Pool, blocker pgx.Tx) {
	t.Helper()

	for {
		var blocked bool

		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE $1::integer = ANY(pg_blocking_pids(pid)))`,
			int64(blocker.Conn().PgConn().PID())).Scan(&blocked))

		if blocked {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatal("handoff 정산의 부모 잠금 대기를 관측하지 못했다")
		case <-time.After(time.Millisecond):
		}
	}
}

// insertSentHandoffFollower는 공백 정규화 뒤 단위와 같은 논리 키가 되는 새 SENT outbox와 SENT follower를 commit한다.
func insertSentHandoffFollower(ctx context.Context, t *testing.T, pool *pgxpool.Pool, unit handoffTestUnit) (int64, int64) {
	t.Helper()

	var outboxID, deliveryID int64

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox (
			kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, sent_at, terminal_at
		) VALUES ($1, 'channel-transition', $2, '{}'::jsonb, $3, 0, clock_timestamp(), clock_timestamp(), clock_timestamp(), clock_timestamp())
		RETURNING id
	`, domain.OutboxKindNewVideo, "  "+unit.contentID+"  ", domain.OutboxStatusSent).Scan(&outboxID))

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO youtube_notification_delivery (
			outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, sent_at
		) VALUES ($1, $2, $3, 0, clock_timestamp(), clock_timestamp(), clock_timestamp())
		RETURNING id
	`, outboxID, unit.roomID, domain.OutboxStatusSent).Scan(&deliveryID))

	return outboxID, deliveryID
}

// 행 한도 초과는 원장 값과 무관하게 확인을 닫으므로 원장 잠금 대기로 정산이 막히면 안 된다.
func TestHandoffInvalidLocalMembershipClosesWithoutLedgerLock(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "handoff-local-mismatch", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	transition.config.LogicalGroupLimit = 1

	blocker, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackPlanModeSQLTest(t, blocker)

	_, err = blocker.Exec(t.Context(), `SELECT source_delivery_id FROM youtube_notification_delivery_ledger
		WHERE source_delivery_id=$1 FOR UPDATE`, unit.ownerID)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	result, err := transition.ExhaustAfterHandoff(ctx, &claim, handoffcheck.CodeFailed, "failed without budget")
	require.ErrorIs(t, err, ErrHandoffCheckClosed)
	require.Equal(t, ApplyConflict, result.Outcome)
	require.Equal(t, domain.OutboxStatusSent, loadHandoffTestDelivery(t, pool, unit.ownerID).status)
	require.NotNil(t, requireHandoffRequest(t, pool, unit.admission.RequestBaseID).CheckedAt)
}
