package sourceobservation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestRetryPolicyUsesClaimedAttemptAndPreservesAvailableAtOnExhaustion(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	batch := publishAndClaimCommunityPost(t.Context(), t, pool, claimOptions())
	first := batch.Claims[0]
	require.Equal(t, 1, first.AttemptCount)

	input := RetryInput{ObservationID: first.ObservationID, LeaseToken: first.LeaseToken, AttemptCount: first.AttemptCount, Delay: time.Millisecond, ErrorCode: "provider_error", ErrorDetail: "retry evidence"}
	status, err := repo.Retry(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, contract.StatusPending, status)

	_, err = pool.Exec(t.Context(), "UPDATE source_observation_queue SET attempt_count=$2,available_at=clock_timestamp()-interval '1 second' WHERE observation_id=$1", first.ObservationID, MaxAttempts-1)
	require.NoError(t, err)

	batch, err = repo.ClaimBatch(t.Context(), claimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)

	last := batch.Claims[0]
	require.Equal(t, MaxAttempts, last.AttemptCount)

	var availableBefore, availableAfter time.Time

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT available_at FROM source_observation_queue WHERE observation_id=$1", last.ObservationID).Scan(&availableBefore))

	input.LeaseToken = last.LeaseToken
	input.AttemptCount = last.AttemptCount
	status, err = repo.Retry(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, contract.StatusDeadLetter, status)

	var (
		code, detail string
		deadAt       *time.Time
	)

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT available_at,last_error_code,last_error_detail,dead_lettered_at FROM source_observation_queue WHERE observation_id=$1", last.ObservationID).Scan(&availableAfter, &code, &detail, &deadAt))
	require.True(t, availableAfter.Equal(availableBefore))
	require.Equal(t, "attempts_exhausted", code)
	require.Equal(t, input.ErrorDetail, detail)
	require.NotNil(t, deadAt)
}

func TestRetryRejectsMismatchedAttemptWithoutChangingClaim(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	batch := publishAndClaimCommunityPost(t.Context(), t, pool, claimOptions())
	work := batch.Claims[0]
	input := RetryInput{ObservationID: work.ObservationID, LeaseToken: work.LeaseToken, AttemptCount: MaxAttempts, ErrorCode: "provider_error"}
	_, err := repo.Retry(t.Context(), input)
	require.ErrorIs(t, err, ErrClaimLost)
	requireQueueStatus(t.Context(), t, pool, work.ObservationID, contract.StatusProcessing)

	input.AttemptCount = work.AttemptCount

	status, err := repo.Retry(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, contract.StatusPending, status)

	_, err = repo.Retry(t.Context(), input)
	require.ErrorIs(t, err, ErrClaimLost)
}

// claim은 시도를 소진한 due 행을 queue 순서대로 LIMIT만큼 DEAD_LETTER로 분류하고, 다른 실행이 잠근 행과
// 아직 due가 아닌 행은 남긴다. 분류는 같은 claim의 정상 후보 선점과 독립이다.
func TestClaimDeadLettersDueExhaustedRowsInQueueOrderSkippingLocked(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedClaimBacklogRows(t, pool, 20, 0)

	rows, err := pool.Query(ctx, "SELECT observation_id FROM source_observation_queue ORDER BY observation_id LIMIT 7")
	require.NoError(t, err)

	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	require.NoError(t, err)
	require.Len(t, ids, 7)

	locked, processingDue, first, second, third, notDue, leased := ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6]

	exhaustQueueRow(t, pool, locked, "PENDING", "-60 minutes", "0")
	exhaustQueueRow(t, pool, processingDue, "PROCESSING", "-55 minutes", "-1 minute")
	exhaustQueueRow(t, pool, first, "PENDING", "-50 minutes", "0")
	exhaustQueueRow(t, pool, second, "PENDING", "-40 minutes", "0")
	exhaustQueueRow(t, pool, third, "PENDING", "-30 minutes", "0")
	exhaustQueueRow(t, pool, notDue, "PENDING", "10 minutes", "0")
	exhaustQueueRow(t, pool, leased, "PROCESSING", "-70 minutes", "10 minutes")

	lockTx, err := pool.Begin(ctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		if rollbackErr := lockTx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback lock transaction: %v", rollbackErr)
		}
	})

	_, err = lockTx.Exec(ctx, "SELECT 1 FROM source_observation_queue WHERE observation_id = $1 FOR UPDATE", locked)
	require.NoError(t, err)

	repo := NewRepository(pool)
	options := contentClaimOptions()

	options.Limit = 2

	_, err = repo.ClaimBatch(ctx, options)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{processingDue, first}, attemptsExhaustedRows(t, pool), "locked exhausted row must be skipped")

	require.NoError(t, lockTx.Rollback(ctx))

	_, err = repo.ClaimBatch(ctx, options)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{locked, processingDue, first, second}, attemptsExhaustedRows(t, pool))

	_, err = repo.ClaimBatch(ctx, options)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{locked, processingDue, first, second, third}, attemptsExhaustedRows(t, pool),
		"exhausted rows that are not due or still leased must stay active")
}

// exhaustQueueRow는 queue 행을 시도 소진 상태로 만든다. PROCESSING이면 다른 API의 lease를 붙인다.
func exhaustQueueRow(t *testing.T, pool *pgxpool.Pool, id int64, status, availableAt, leaseExpiresAt string) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		UPDATE source_observation_queue
		SET attempt_count = $2, status = $3,
		    available_at = NOW() + $4::interval,
		    lease_owner = CASE WHEN $3 = 'PROCESSING' THEN 'other-api' END,
		    lease_token = CASE WHEN $3 = 'PROCESSING' THEN repeat('b', 64) END,
		    lease_expires_at = CASE WHEN $3 = 'PROCESSING' THEN NOW() + $5::interval END
		WHERE observation_id = $1`, id, MaxAttempts, status, availableAt, leaseExpiresAt)
	require.NoError(t, err)
}

// attemptsExhaustedRows는 claim이 시도 소진으로 DEAD_LETTER 처리하고 lease를 비운 행을 돌려준다.
func attemptsExhaustedRows(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()

	rows, err := pool.Query(t.Context(), `
		SELECT observation_id FROM source_observation_queue
		WHERE status = 'DEAD_LETTER' AND last_error_code = 'attempts_exhausted'
		  AND lease_owner IS NULL AND lease_expires_at IS NULL AND dead_lettered_at IS NOT NULL
		ORDER BY observation_id`)
	require.NoError(t, err)

	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	require.NoError(t, err)

	return ids
}
