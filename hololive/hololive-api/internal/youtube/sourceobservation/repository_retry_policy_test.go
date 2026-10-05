package sourceobservation

import (
	"testing"
	"time"

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
