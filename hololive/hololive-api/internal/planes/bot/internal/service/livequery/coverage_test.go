package livequery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCoverageReasonPrecedence(t *testing.T) {
	// 여러 진단이 겹쳐도 projection·수집·상태·시간의 기존 우선순위를 유지합니다.
	snapshot := channelSnapshot{Facts: inconsistentState | pendingEnd | futureClock | staleLive}
	require.Equal(t, InvalidProjection, snapshot.channel().Reason)

	snapshot.Facts |= projectionValid
	require.Equal(t, Uncollected, snapshot.channel().Reason)

	snapshot.Facts |= targetsCollected
	require.Equal(t, Inconsistent, snapshot.channel().Reason)

	snapshot.Facts &^= inconsistentState
	require.Equal(t, ConfirmingEnd, snapshot.channel().Reason)

	snapshot.Facts &^= pendingEnd
	require.Equal(t, InvalidClock, snapshot.channel().Reason)

	snapshot.Facts &^= futureClock
	require.Equal(t, Stale, snapshot.channel().Reason)

	snapshot.Facts &^= staleLive
	require.Equal(t, Incomplete, snapshot.channel().Reason)

	snapshot.CoveredAt = new(time.Now())
	require.Equal(t, Covered, snapshot.channel().Reason)
}
