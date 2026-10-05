package sourceobservation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestObservationSettlementRejectsExpiryUnderUnchangedRowLock(t *testing.T) {
	for _, mode := range []string{"retry", "exhausted", "dead_letter"} {
		t.Run(mode, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

			defer cancel()

			work := publishAndClaimCommunityPost(ctx, t, pool, claimOptions()).Claims[0]

			if mode == "exhausted" {
				work.AttemptCount = MaxAttempts
			}

			var expires time.Time

			require.NoError(t, pool.QueryRow(ctx, `UPDATE source_observation_queue
				SET attempt_count=$2, lease_expires_at=clock_timestamp()+interval '1 second'
				WHERE observation_id=$1 RETURNING lease_expires_at`, work.ObservationID, work.AttemptCount).Scan(&expires))

			blocker, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer rollbackPublishTestTx(t.Context(), t, blocker, "observation clock blocker")

			// 행 버전을 변경하지 않아 UPDATE 재평가에 기대지 않고 잠금 뒤 만료 검사를 검증한다.
			_, err = blocker.Exec(ctx, "SELECT observation_id FROM source_observation_queue WHERE observation_id=$1 FOR UPDATE", work.ObservationID)
			require.NoError(t, err)

			done := make(chan error, 1)

			var workers sync.WaitGroup

			workers.Go(func() { done <- settleObservationClock(ctx, NewRepository(pool), work, mode) })

			defer func() {
				cancel()
				workers.Wait()
			}()

			waitObservationSettlementLock(ctx, t, pool, blocker.Conn().PgConn().PID(), done)

			_, err = pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp())))::double precision+0.05)`, expires)
			require.NoError(t, err)
			require.NoError(t, blocker.Commit(ctx))
			require.ErrorIs(t, <-done, ErrClaimLost)

			var status, token string

			var attempts int

			require.NoError(t, pool.QueryRow(ctx, `SELECT status,lease_token,attempt_count
				FROM source_observation_queue WHERE observation_id=$1`, work.ObservationID).Scan(&status, &token, &attempts))
			require.Equal(t, "PROCESSING", status)
			require.Equal(t, work.LeaseToken, token)
			require.Equal(t, work.AttemptCount, attempts)
		})
	}
}

func settleObservationClock(ctx context.Context, repo *Repository, work ClaimWork, mode string) error {
	if mode == "dead_letter" {
		return repo.DeadLetter(ctx, DeadLetterInput{ObservationID: work.ObservationID, LeaseToken: work.LeaseToken, ErrorCode: "clock_probe"})
	}

	_, err := repo.Retry(ctx, RetryInput{
		ObservationID: work.ObservationID, LeaseToken: work.LeaseToken, AttemptCount: work.AttemptCount,
		Delay: time.Second, ErrorCode: "clock_probe",
	})

	return err
}

func waitObservationSettlementLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, blockerPID uint32, done <-chan error) {
	t.Helper()

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		var blocked bool

		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&blocked))

		if blocked {
			return
		}

		select {
		case callErr := <-done:
			t.Fatalf("settlement did not wait on row lock: %v", callErr)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
