package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/pgxutil"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

func TestLedgerConcurrentInsertPreservesSentPrecedence(t *testing.T) {
	for _, first := range []LedgerStatus{LedgerStatusSent, LedgerStatusQuarantined} {
		t.Run(string(first), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

			defer cancel()

			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)

			defer conn.Release()

			pid := conn.Conn().PgConn().PID()
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer func() {
				if rollbackErr := pgxutil.Rollback(ctx, tx); !errors.Is(rollbackErr, pgx.ErrTxClosed) {
					require.NoError(t, rollbackErr)
				}
			}()

			write := LedgerWrite{
				Key:        ytcontentid.LogicalKey{Kind: domain.OutboxKindNewVideo, LogicalID: "concurrent-ledger", RoomID: ledgerTestRoom},
				ObservedAt: time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC), SourceDeliveryID: 1,
			}
			require.NoError(t, RecordDeliveryLedgerWrites(ctx, tx, first, []LedgerWrite{write}))

			second := LedgerStatusSent
			if first == LedgerStatusSent {
				second = LedgerStatusQuarantined
			}

			done := make(chan error, 1)

			go func() { done <- RecordDeliveryLedgerWrites(ctx, conn, second, []LedgerWrite{write}) }()

			waitForLedgerLock(ctx, t, pool, pid)
			require.NoError(t, tx.Commit(ctx))

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}

			record := readDeliveryLedgerRecord(t, pool, write.Key)
			require.Equal(t, LedgerStatusSent, record.Status)
			require.NotNil(t, record.SentAt)
			require.Equal(t, write.ObservedAt, record.SentAt.UTC())
		})
	}
}

func waitForLedgerLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, pid uint32) {
	t.Helper()
	require.Eventually(t, func() bool {
		var blocked bool

		err := pool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1::integer))>0`, pid).Scan(&blocked)

		return err == nil && blocked
	}, time.Second, 10*time.Millisecond)
}

func TestLedgerRejectsDuplicateBatchBeforeWriting(t *testing.T) {
	pool := dbtest.NewPool(t)
	write := LedgerWrite{
		Key:        ytcontentid.LogicalKey{Kind: domain.OutboxKindNewVideo, LogicalID: "duplicate-ledger", RoomID: ledgerTestRoom},
		ObservedAt: time.Now(), SourceDeliveryID: 1,
	}
	require.Error(t, RecordDeliveryLedgerWrites(t.Context(), pool, LedgerStatusSent, []LedgerWrite{write, write}))

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_notification_delivery_ledger`).Scan(&count))
	require.Zero(t, count)
}

func TestLedgerNoopDoesNotHideInvalidInputOrCommitSibling(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	write := LedgerWrite{
		Key:        ytcontentid.LogicalKey{Kind: domain.OutboxKindNewVideo, LogicalID: "existing-sent", RoomID: ledgerTestRoom},
		ObservedAt: time.Now(), SourceDeliveryID: 1,
	}
	require.NoError(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusSent, []LedgerWrite{write}))

	sibling := write

	sibling.Key.LogicalID = "must-rollback"
	write.SourceDeliveryID = 0
	require.Error(t, RecordDeliveryLedgerWrites(ctx, pool, LedgerStatusQuarantined, []LedgerWrite{sibling, write}))

	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM youtube_notification_delivery_ledger`).Scan(&count))
	require.Equal(t, 1, count)
	require.Equal(t, LedgerStatusSent, readDeliveryLedgerRecord(t, pool, write.Key).Status)
}
