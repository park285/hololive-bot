package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// 서로 다른 표를 쓰는 두 트랜잭션은 같은 영상 갱신에서 직렬화되고, 뒤 갱신이 앞 커밋을 읽어 합성 상태를 잃지 않는다.
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
				RefreshAllLiveRuntimeState(t, pool)
			}

			first, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, first)

			second, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackLiveReviewReceiptFactsTx(t, second)

			_, err = first.Exec(ctx, `INSERT INTO youtube_live_sessions(video_id,channel_id,status)
				VALUES ('concurrent','first-writer','LIVE') ON CONFLICT(video_id) DO UPDATE SET channel_id=excluded.channel_id;
				SELECT refresh_youtube_live_runtime_state(ARRAY['concurrent'])`)
			require.NoError(t, err)
			assertLiveRuntimeState(t, first)

			_, err = second.Exec(ctx, `INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at)
				VALUES ('concurrent','LIVE','2026-10-12','2026-10-13') ON CONFLICT(video_id) DO UPDATE SET
				status=excluded.status,last_live_positive_at=excluded.last_live_positive_at,last_live_positive_seen_at=excluded.last_live_positive_seen_at`)
			require.NoError(t, err, "다른 표의 원본 쓰기는 전역 guard 없이 진행됩니다")

			result := make(chan error, 1)
			done := make(chan struct{})

			go func() {
				defer close(done)

				_, refreshErr := second.Exec(ctx, `SELECT refresh_youtube_live_runtime_state(ARRAY['concurrent'])`)
				result <- refreshErr
			}()

			defer func() {
				cancel()
				rollbackLiveReviewReceiptFactsTx(t, first)
				<-done
			}()

			waitSecondTxBlockedByFirst(ctx, t, pool, first, second)
			require.NoError(t, first.Commit(ctx))

			select {
			case err = <-result:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("second refresh did not finish after video lock release")
			}

			assertLiveRuntimeState(t, second)
			assertCombinedLiveRuntimeState(ctx, t, second)
			require.NoError(t, second.Commit(ctx))
			assertLiveRuntimeState(t, pool)
		})
	}
}

func waitSecondTxBlockedByFirst(ctx context.Context, t *testing.T, pool *pgxpool.Pool, first, second pgx.Tx) {
	t.Helper()

	firstPID := first.Conn().PgConn().PID()
	secondPID := second.Conn().PgConn().PID()
	// 시간 지연으로 겹침을 추정하지 않고 second가 first의 잠금을 실제로 기다리는지 확인한다.
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
