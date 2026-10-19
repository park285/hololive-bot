package dispatchoutbox

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
)

// recordClaims는 ClaimDue가 돌려준 행의 claim(ID, claim 당시 attempt)이다.
func recordClaims(records ...*Record) []DeliveryClaim {
	claims := make([]DeliveryClaim, 0, len(records))
	for _, record := range records {
		claims = append(claims, DeliveryClaim{ID: record.ID, AttemptCount: record.AttemptCount})
	}

	return claims
}

// attemptClaims는 같은 attempt의 claim 목록이다.
func attemptClaims(attempt int, ids ...int64) []DeliveryClaim {
	claims := make([]DeliveryClaim, 0, len(ids))
	for _, id := range ids {
		claims = append(claims, DeliveryClaim{ID: id, AttemptCount: attempt})
	}

	return claims
}

// beginSendingFixture는 claim한 행을 send unit별로 실제 고정·발송 진입 경로를 거쳐 sending에 넣는다. 각 unit의 멤버
// 전원이 records에 있어야 한다.
func beginSendingFixture(tb testing.TB, repository *PgxRepository, owner string, records ...*Record) {
	tb.Helper()

	var units []int64

	members := make(map[int64][]*Record, len(records))
	for _, record := range records {
		if _, ok := members[record.SendUnitID]; !ok {
			units = append(units, record.SendUnitID)
		}

		members[record.SendUnitID] = append(members[record.SendUnitID], record)
	}

	for _, unit := range units {
		unitRecords := members[unit]
		require.NotEmpty(tb, unitRecords)

		first := unitRecords[0]
		claims := recordClaims(unitRecords...)
		request, err := repository.PinSendRequest(tb.Context(), unit, claims, owner, first.ClientRequestID, SendRequest{Body: "fixture", Route: SendRouteText})
		require.NoError(tb, err)
		require.NoError(tb, repository.BeginSendRequest(tb.Context(), unit, claims, owner, request.ClientRequestID, time.Minute))
	}
}

// requireFenceMiss는 err가 reason을 포함한 send fence 미스인지 확인하고 그 미스를 돌려준다.
func requireFenceMiss(tb testing.TB, err error, reason sendfence.Reason) *sendfence.MissError {
	tb.Helper()

	miss, ok := errors.AsType[*sendfence.MissError](err)
	require.True(tb, ok, "want *sendfence.MissError, got %T %v", err, err)
	require.True(tb, miss.Has(reason), "want reason %s, got %v", reason, miss)

	return miss
}
