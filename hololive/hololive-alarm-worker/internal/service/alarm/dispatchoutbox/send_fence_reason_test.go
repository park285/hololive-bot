package dispatchoutbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	dbtest "github.com/kapu/hololive-dbtest"
)

// 고정 멤버가 종단 상태가 되거나 보존 정리로 사라지면 Load·Begin은 단위를 다시 모을 수 없다는 member_terminal을 낸다.
func TestSendRequestReportsMemberTerminalForTerminalOrDeletedPinnedMember(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE alarm_dispatch_deliveries SET status='dlq', dlq_at=clock_timestamp(), locked_by=NULL, locked_at=NULL, lock_expires_at=NULL WHERE id=$1`,
		`DELETE FROM alarm_dispatch_deliveries WHERE id=$1`,
	} {
		pool := dbtest.NewPool(t)
		repo := NewPgxRepositoryFromPool(pool, nil)
		unit, ids, requestID := seedRequestClockUnit(t, repo)
		request, err := repo.PinSendRequest(t.Context(), unit, attemptClaims(0, ids...), requestClockOwner, requestID, SendRequest{Body: "fence reason", Route: SendRouteText})
		require.NoError(t, err)

		_, err = pool.Exec(t.Context(), mutation, ids[1])
		require.NoError(t, err)

		remaining := attemptClaims(0, ids[0])

		_, err = repo.LoadSendRequest(t.Context(), unit, remaining, requestClockOwner, requestID)
		require.Zero(t, requireFenceMiss(t, err, sendfence.ReasonMemberTerminal).Applied)

		err = repo.BeginSendRequest(t.Context(), unit, remaining, requestClockOwner, request.ClientRequestID, time.Minute)
		require.Zero(t, requireFenceMiss(t, err, sendfence.ReasonMemberTerminal).Applied)
		assertRequestClockLeased(t.Context(), t, pool, ids[:1])
	}
}

// claim에서 본 attempt와 행의 attempt가 다르면 MarkSent·DLQ·격리·반환은 version_changed로 행을 그대로 둔다.
func TestOwnerTransitionsRejectAttemptMismatch(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := NewPgxRepositoryFromPool(pool, nil)
	unit, ids, requestID := seedRequestClockUnit(t, repo)
	claims := attemptClaims(0, ids...)
	request, err := repo.PinSendRequest(t.Context(), unit, claims, requestClockOwner, requestID, SendRequest{Body: "fence reason", Route: SendRouteText})
	require.NoError(t, err)

	stale := []TerminalUpdate{{ID: ids[0], AttemptCount: 1}, {ID: ids[1], AttemptCount: 1}}

	requireVersionMiss(t, repo.ReleaseLeased(t.Context(), attemptClaims(1, ids...), requestClockOwner))
	requireVersionMiss(t, repo.MoveToDLQ(t.Context(), stale, requestClockOwner))
	requireVersionMiss(t, repo.Quarantine(t.Context(), stale, requestClockOwner))
	assertRequestClockLeased(t.Context(), t, pool, ids)

	require.NoError(t, repo.BeginSendRequest(t.Context(), unit, claims, requestClockOwner, request.ClientRequestID, time.Minute))
	requireVersionMiss(t, repo.MarkSent(t.Context(), unit, attemptClaims(1, ids...), requestClockOwner, "iris-request", time.Second))

	var sending int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(id) FROM alarm_dispatch_deliveries
		WHERE id=ANY($1) AND status='sending' AND locked_by=$2 AND attempt_count=0 AND sent_at IS NULL`, ids, requestClockOwner).Scan(&sending))
	require.Equal(t, len(ids), sending)
}

func requireVersionMiss(t *testing.T, err error) {
	t.Helper()

	miss := requireFenceMiss(t, err, sendfence.ReasonVersionChanged)
	require.Zero(t, miss.Applied)
}
