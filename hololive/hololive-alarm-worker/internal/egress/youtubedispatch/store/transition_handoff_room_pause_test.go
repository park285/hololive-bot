package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestHandoffRoomNotJoinedFailsUnitAndPausesRoom(t *testing.T) {
	pool := dbtest.NewPool(t)
	transition := newTestTransitionStore(t, pool)
	unit := sendHandoffTestUnit(t, pool, transition, "video-handoff-room-pause", testHandoffHorizon)
	claim := claimDueHandoff(t, pool, transition, unit.admission.RequestBaseID)

	paused, err := transition.PausedRooms(t.Context(), []string{unit.roomID, "room-never-paused"})
	require.NoError(t, err)
	require.Empty(t, paused)

	result, err := transition.PauseRoomAfterHandoff(t.Context(), &claim, contractsalarm.HandoffCodeRoomNotJoined, "iris reply status failed: bot account is not in the room")
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, result.Outcome)

	owner := loadHandoffTestDelivery(t, pool, unit.ownerID)
	require.Equal(t, domain.OutboxStatusFailed, owner.status)
	require.Equal(t, 1, owner.attempt)
	require.Equal(t, domain.OutboxStatusFailed, loadHandoffTestDelivery(t, pool, unit.followerID).status)

	status, _ := loadHandoffTestLedger(t, pool, unit)
	require.Empty(t, status)

	state := requireHandoffRequest(t, pool, unit.admission.RequestBaseID)
	require.Zero(t, state.Generation, "a room the bot is not in must not be reissued")
	require.Nil(t, state.NextCheckAt)
	require.NotNil(t, state.CheckedAt)

	paused, err = transition.PausedRooms(t.Context(), []string{unit.roomID, "room-never-paused"})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{unit.roomID: true}, paused)
}
