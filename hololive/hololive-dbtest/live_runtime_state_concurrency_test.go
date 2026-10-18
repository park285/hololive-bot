package dbtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"
)

// 서로 다른 표를 쓰는 두 트랜잭션도 같은 영상의 합성 상태를 잃지 않고 대기 전 snapshot을 재사용하지 않는다.
func TestLiveRuntimeStateConcurrentDistinctSourceWriters(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "absent_upserts"

		if existing {
			name = "existing_rows"
		}

		t.Run(name, func(t *testing.T) {
			pool := NewPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

			defer cancel()

			if existing {
				_, err := pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('concurrent','old-channel','LIVE');
					INSERT INTO youtube_live_reconciliation_heads(video_id,status) VALUES ('concurrent','UPCOMING')`)
				require.NoError(t, err)
			}

			first, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, first)

			second, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, second)

			_, err = first.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status)
				VALUES ('concurrent','first-writer','LIVE') ON CONFLICT(video_id) DO UPDATE SET channel_id=excluded.channel_id`)
			require.NoError(t, err)
			assertLiveRuntimeState(t, first)

			result := make(chan error, 1)
			done := make(chan struct{})

			go func() {
				defer close(done)

				_, writeErr := second.Exec(ctx, `INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at)
					VALUES ('concurrent','LIVE','2026-10-12','2026-10-13') ON CONFLICT(video_id) DO UPDATE SET
					status=excluded.status,last_live_positive_at=excluded.last_live_positive_at,last_live_positive_seen_at=excluded.last_live_positive_seen_at`)
				result <- writeErr
			}()

			defer func() {
				cancel()
				rollbackLiveReviewReceiptFactsTx(t, first)
				<-done
			}()

			waitLiveRuntimeGuard(ctx, t, pool, first, second)
			require.NoError(t, first.Commit(ctx))

			select {
			case err = <-result:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("second source writer did not finish after guard release")
			}

			assertLiveRuntimeState(t, second)

			assertCombinedLiveRuntimeState(ctx, t, second)
			require.NoError(t, second.Commit(ctx))
			assertLiveRuntimeState(t, pool)
		})
	}
}

// 오래된 고정 snapshot은 새 원본과 섞인 모델을 커밋하지 않고 SQLSTATE 40001로 끝나야 한다.
func TestLiveRuntimeStateStaleFixedSnapshotFails(t *testing.T) {
	for _, isolation := range []pgx.TxIsoLevel{pgx.RepeatableRead, pgx.Serializable} {
		t.Run(string(isolation), func(t *testing.T) {
			pool := NewPool(t)
			ctx := t.Context()
			stale, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: isolation})
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, stale)

			var before int

			require.NoError(t, stale.QueryRow(ctx, `SELECT count(*) FROM youtube_live_runtime_state`).Scan(&before))

			_, err = pool.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status) VALUES ('fixed-snapshot','new-source','LIVE')`)
			require.NoError(t, err)

			_, err = stale.Exec(ctx, `INSERT INTO youtube_live_reconciliation_heads(video_id,status) VALUES ('fixed-snapshot','UPCOMING')`)

			pgErr, ok := errors.AsType[*pgconn.PgError](err)
			require.True(t, ok, "stale snapshot must fail with PostgreSQL serialization error: %v", err)
			require.Equal(t, "40001", pgErr.Code)
			require.NoError(t, pgxdb.Rollback(ctx, stale))
			assertLiveRuntimeState(t, pool)

			var hasHead bool

			require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM youtube_live_reconciliation_heads WHERE video_id='fixed-snapshot')`).Scan(&hasHead))
			require.False(t, hasHead)
		})
	}
}

func waitLiveRuntimeGuard(ctx context.Context, t *testing.T, pool *pgxpool.Pool, first, second pgx.Tx) {
	t.Helper()

	firstPID := first.Conn().PgConn().PID()
	secondPID := second.Conn().PgConn().PID()
	// 시간 지연으로 겹침을 추정하지 않고 실제 guard 잠금 대기를 확인한 뒤 첫 쓰기를 커밋한다.
	require.Eventually(t, func() bool {
		var blocked bool

		err := pool.QueryRow(ctx, `SELECT $1::int=ANY(pg_blocking_pids($2::int))`, firstPID, secondPID).Scan(&blocked)

		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
}

func assertCombinedLiveRuntimeState(ctx context.Context, t *testing.T, tx pgx.Tx) {
	t.Helper()

	var combined bool

	require.NoError(t, tx.QueryRow(ctx, `SELECT channel_id='first-writer' AND session_status='LIVE' AND head_status='LIVE'
		AND last_live_positive_at='2026-10-12'::timestamptz AND last_live_positive_seen_at='2026-10-13'::timestamptz
		FROM youtube_live_runtime_state WHERE video_id='concurrent'`).Scan(&combined))
	require.True(t, combined)
}
