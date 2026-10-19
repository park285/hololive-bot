package youtubedispatch

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 부모 잠금을 보유한 세션에서도 실제 Project를 실행한다. 내부 transaction은
// savepoint이고 바깥 READ COMMITTED transaction의 commit까지 부모 잠금은 유지된다.
type aggregateProjectionTransaction struct {
	pgx.Tx
}

func (db aggregateProjectionTransaction) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return db.Begin(ctx)
}

func TestOutboxAggregateProjector_OlderProjectionCannotOverwriteCompletedSend(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "Project"

		if pending {
			name = "ProjectPending"
		}

		t.Run(name, func(t *testing.T) {
			runAggregateProjectionCompletionRace(t, pending)
		})
	}
}

func runAggregateProjectionCompletionRace(t *testing.T, pending bool) {
	t.Helper()

	pool := newDeliveryPool(t)
	item, delivery := seedAggregateProjectionRevive(t, pool)
	logger := slog.New(slog.DiscardHandler)
	transition := newAggregateTransitionStore(t, pool, logger)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	parentTx := holdAggregateProjectionParent(ctx, t, pool, item)

	projectedAt := time.Now().UTC().Truncate(time.Microsecond)
	newer := newOutboxAggregateProjector(store.NewDeliveryRepository(
		aggregateProjectionTransaction{Tx: parentTx}, logger, func() time.Time { return projectedAt }))
	older := newOutboxAggregateProjector(store.NewDeliveryRepository(
		conn, logger, func() time.Time { return projectedAt.Add(-time.Minute) }))

	var (
		projectionErr error
		projectedIDs  []int64
	)

	finished := make(chan struct{})

	go func() {
		defer close(finished)

		if pending {
			projectedIDs, projectionErr = older.ProjectPending(ctx, 10)
		} else {
			projectionErr = older.Project(ctx, []int64{item.ID})
		}
	}()

	defer func() {
		cancel()
		<-finished
	}()

	// FAILED를 관측한 이전 projector가 실제 부모 row lock에서 기다린 뒤에만
	// revive/send를 진행한다. sleep이나 SQL 문자열 검사로 경합을 추정하지 않는다.
	waitAggregateProjectionLock(ctx, t, pool, parentTx, conn)

	frozen := reviveCompleteAndProject(ctx, t, transition, newer, item)
	require.NoError(t, parentTx.Commit(ctx))

	select {
	case <-finished:
		require.NoError(t, projectionErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	if pending {
		require.Equal(t, []int64{item.ID}, projectedIDs)
	}

	assertAggregateProjectionCompleted(ctx, t, pool, item, delivery, projectedAt)

	requests, err := transition.LoadFrozenRequests(ctx, []int64{delivery.ID})
	require.NoError(t, err)
	require.Equal(t, []store.FrozenRequest{frozen}, requests)
}

func reviveCompleteAndProject(ctx context.Context, t *testing.T, transition *store.TransitionStore, newer *OutboxAggregateProjector, item domain.YouTubeNotificationOutbox) store.FrozenRequest {
	t.Helper()

	revived, err := transition.ReviveFailedLogicalGroups(ctx, time.Hour, 10)
	require.NoError(t, err)
	require.Equal(t, store.ApplyApplied, revived.Outcome)
	require.Equal(t, 1, revived.RevivedDeliveries)
	require.NoError(t, newer.Project(ctx, revived.TouchedOutboxIDs))

	frozen := completeAggregateProjectionDelivery(ctx, t, transition, item)
	require.NoError(t, newer.Project(ctx, []int64{item.ID}))

	return frozen
}

func newAggregateTransitionStore(t *testing.T, pool *pgxpool.Pool, logger *slog.Logger) *store.TransitionStore {
	t.Helper()

	transition, err := store.NewTransitionStore(pool, logger, store.TransitionConfig{
		Owner: testTransitionOwner, MaxRetries: 3, RetryBackoff: time.Second, LockTimeout: time.Minute,
		ClaimFreshnessWindow: time.Hour, LogicalGroupLimit: 10,
	})
	require.NoError(t, err)

	return transition
}

func waitAggregateProjectionLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, parentTx pgx.Tx, conn *pgxpool.Conn) {
	t.Helper()

	require.Eventually(t, func() bool {
		var blocked bool

		waitErr := pool.QueryRow(ctx, `SELECT $1::integer = ANY(pg_blocking_pids($2::integer))`,
			parentTx.Conn().PgConn().PID(), conn.Conn().PgConn().PID()).Scan(&blocked)

		return waitErr == nil && blocked
	}, 3*time.Second, 10*time.Millisecond)
}

func holdAggregateProjectionParent(ctx context.Context, t *testing.T, pool *pgxpool.Pool, item domain.YouTubeNotificationOutbox) pgx.Tx {
	t.Helper()

	parentTx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	require.NoError(t, err)

	t.Cleanup(func() {
		if rollbackErr := pgxdb.Rollback(ctx, parentTx); rollbackErr != nil {
			require.NoError(t, rollbackErr)
		}
	})

	_, err = parentTx.Exec(ctx, `SELECT id FROM youtube_notification_outbox WHERE id = $1 FOR NO KEY UPDATE`, item.ID)
	require.NoError(t, err)

	return parentTx
}

func assertAggregateProjectionCompleted(
	ctx context.Context,
	t *testing.T,
	pool *pgxpool.Pool,
	item domain.YouTubeNotificationOutbox,
	delivery domain.YouTubeNotificationDelivery,
	projectedAt time.Time,
) {
	t.Helper()

	var (
		parentStatus, childStatus domain.OutboxStatus
		sentAt, terminalAt        time.Time
		lockedAt                  *time.Time
		errorText                 string
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT o.status, d.status, o.sent_at, o.terminal_at, o.locked_at, COALESCE(o.error, '')
		FROM youtube_notification_outbox o
		JOIN youtube_notification_delivery d ON d.outbox_id = o.id
		WHERE d.id = $1`, delivery.ID).Scan(&parentStatus, &childStatus, &sentAt, &terminalAt, &lockedAt, &errorText))
	require.Equal(t, domain.OutboxStatusSent, parentStatus)
	require.Equal(t, domain.OutboxStatusSent, childStatus)
	require.True(t, sentAt.Equal(projectedAt))
	require.True(t, terminalAt.Equal(projectedAt))
	require.Nil(t, lockedAt)
	require.Empty(t, errorText)

	var (
		ledgerStatus     store.LedgerStatus
		sourceDeliveryID int64
	)

	require.NoError(t, pool.QueryRow(ctx, `
		SELECT status, source_delivery_id FROM youtube_notification_delivery_ledger
		WHERE kind = $1 AND logical_id = $2 AND room_id = $3`,
		item.Kind, item.ContentID, delivery.RoomID).Scan(&ledgerStatus, &sourceDeliveryID))
	require.Equal(t, store.LedgerStatusSent, ledgerStatus)
	require.Equal(t, delivery.ID, sourceDeliveryID)
}

func seedAggregateProjectionRevive(t *testing.T, pool *pgxpool.Pool) (domain.YouTubeNotificationOutbox, domain.YouTubeNotificationDelivery) {
	t.Helper()

	createdAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	item := domain.YouTubeNotificationOutbox{
		Kind: domain.OutboxKindNewVideo, ChannelID: "channel-aggregate-race", ContentID: "video-aggregate-race",
		Payload: `{}`, Status: domain.OutboxStatusPending, CreatedAt: createdAt, NextAttemptAt: createdAt,
	}
	require.NoError(t, insertDeliveryTestRows(pool, &item).Error)

	delivery := domain.YouTubeNotificationDelivery{
		OutboxID: item.ID, RoomID: "room-aggregate-race", Status: domain.OutboxStatusFailed,
		AttemptCount: 3, CreatedAt: createdAt, NextAttemptAt: createdAt, Error: "retry exhausted",
	}
	require.NoError(t, insertDeliveryTestRows(pool, &delivery).Error)

	return item, delivery
}

func completeAggregateProjectionDelivery(
	ctx context.Context,
	t *testing.T,
	transition *store.TransitionStore,
	item domain.YouTubeNotificationOutbox,
) store.FrozenRequest {
	t.Helper()

	claimed, err := transition.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	outboxes := map[int64]domain.YouTubeNotificationOutbox{item.ID: item}
	prepared, err := transition.PrepareClaimed(ctx, claimed, outboxes)
	require.NoError(t, err)
	require.Empty(t, prepared.Blocked)
	require.Len(t, prepared.ActiveRows, 1)

	frozen, err := transition.FreezeRequest(ctx, prepared.ActiveRows, store.FrozenRequest{
		BaseID: "youtube:aggregate-race", RoomID: claimed[0].RoomID,
		Message: "immutable aggregate race message", Route: "text", DedupeKeys: []string{"aggregate-race"},
	})
	require.NoError(t, err)

	operation, begin, err := transition.BeginSending(ctx, prepared.ActiveRows, outboxes)
	require.NoError(t, err)
	require.Equal(t, store.ApplyApplied, begin.Outcome)
	require.True(t, operation.Valid())

	completed, err := transition.CompleteSent(ctx, operation, nil, store.DeliveryModePerRoom)
	require.NoError(t, err)
	require.Equal(t, store.ApplyApplied, completed.Outcome)
	require.Equal(t, []int64{item.ID}, completed.TouchedOutboxIDs)

	return frozen
}
