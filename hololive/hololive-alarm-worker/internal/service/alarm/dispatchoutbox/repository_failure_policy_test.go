//go:build integration

package dispatchoutbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFailurePolicyRollsBackBatchAndPreservesUnchangedFields(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	retryID := insertAndClaimRoutingRow(t, repo, "groups-worker", "retry-room", "retry-stream")
	dlqID := insertAndClaimRoutingRow(t, repo, "groups-worker", "dlq-room", "dlq-stream")
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := pool.Exec(t.Context(), "UPDATE alarm_dispatch_deliveries SET next_attempt_at=$1 WHERE id=$2", now, dlqID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), "ALTER TABLE alarm_dispatch_deliveries ADD CONSTRAINT test_reject_dlq CHECK(status <> 'dlq')")
	require.NoError(t, err)
	updates := []FailureUpdate{
		{ID: retryID, AttemptCount: 1, RetryAfter: time.Minute, TargetStatus: StatusRetry},
		{ID: dlqID, AttemptCount: 1, RetryAfter: time.Hour, TargetStatus: StatusDLQ},
	}
	err = repo.RouteFailures(t.Context(), updates, "groups-worker")
	require.ErrorContains(t, err, "test_reject_dlq")
	var untouched int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_dispatch_deliveries WHERE id=ANY($1) AND status='leased' AND attempt_count=0 AND locked_by='groups-worker'", []int64{retryID, dlqID}).Scan(&untouched))
	require.Equal(t, 2, untouched)
	_, err = pool.Exec(t.Context(), "ALTER TABLE alarm_dispatch_deliveries DROP CONSTRAINT test_reject_dlq")
	require.NoError(t, err)
	require.NoError(t, repo.RouteFailures(t.Context(), updates, "groups-worker"))
	var retryDelay time.Duration
	var dlqNext time.Time
	var retryTerminal, dlqTerminal *time.Time
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT next_attempt_at-updated_at,dlq_at FROM alarm_dispatch_deliveries WHERE id=$1", retryID).Scan(&retryDelay, &retryTerminal))
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT next_attempt_at,dlq_at FROM alarm_dispatch_deliveries WHERE id=$1", dlqID).Scan(&dlqNext, &dlqTerminal))
	require.Equal(t, time.Minute, retryDelay, "retry는 전이 clock에 지연을 더한다")
	require.Nil(t, retryTerminal)
	require.True(t, dlqNext.Equal(now), "DLQ는 기존 재시도 시각을 보존한다")
	require.NotNil(t, dlqTerminal)
}

func TestFailurePolicyRejectsDuplicateDeliveryBeforeWrite(t *testing.T) {
	repo, pool := setupDispatchOutboxIntegration(t)
	id := insertAndClaimRoutingRow(t, repo, "groups-worker", "duplicate-room", "duplicate-stream")
	err := repo.RouteFailures(t.Context(), []FailureUpdate{
		{ID: id, AttemptCount: 1, TargetStatus: StatusRetry},
		{ID: id, AttemptCount: 1, TargetStatus: StatusDLQ},
	}, "groups-worker")
	require.ErrorContains(t, err, "duplicate delivery")
	var status string
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM alarm_dispatch_deliveries WHERE id=$1", id).Scan(&status))
	require.Equal(t, string(StatusLeased), status)
}
