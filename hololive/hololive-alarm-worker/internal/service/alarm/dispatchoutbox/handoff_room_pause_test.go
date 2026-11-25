package dispatchoutbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestHandoffRoomNotJoinedCancelsMembersAndPausesRoom(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)

	const room = "handoff-room-not-joined"

	unit := seedSentHandoffUnits(t, pool, repo, room)[0]

	releaser := &fakeClaimKeyReleaser{}
	consumer, err := NewHandoffConsumer(repo, releaser)
	require.NoError(t, err)

	claim := claimOneHandoff(t, repo)
	require.NoError(t, consumer.PauseRoomAfterHandoff(t.Context(), &claim, contractsalarm.HandoffCodeRoomNotJoined, "iris reply status failed: bot account is not in the room"))

	var canceled int

	//nolint:misspell // cancelled_at은 변경할 수 없는 정본 DB 열 이름입니다.
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(id) FROM alarm_dispatch_deliveries
		WHERE send_unit_id = $1 AND status = 'cancelled' AND cancelled_at IS NOT NULL AND sent_at IS NOT NULL
		AND attempt_count = 1 AND last_error_code = $2`,
		unit.id, contractsalarm.HandoffCodeRoomNotJoined).Scan(&canceled))
	require.Len(t, unit.members, canceled)
	require.Empty(t, releaser.lastKeys, "a paused room must keep its claim keys so the same alarm is not published again")
	require.True(t, loadHandoffUnit(t, pool, unit.id).checked)

	var reason string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT reason FROM kakao_room_delivery_pauses WHERE room_id = $1`, room).Scan(&reason))
	require.Equal(t, "room_not_joined", reason)

	envelope := receiptEnvelope(room, "after-pause")

	envelope.Notification.Stream.ID = room + "-after-pause"

	result, err := repo.InsertBatch(t.Context(), PublishBatchInput{Envelopes: []AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)
	require.Equal(t, 1, result.PausedRoomDeliveries)
	require.Len(t, result.Receipts, 1)
	require.Equal(t, PublishInserted, result.Receipts[0].Outcome)
	require.True(t, result.Receipts[0].Accepted())

	var status, code string

	//nolint:misspell // cancelled_at은 변경할 수 없는 정본 DB 열 이름입니다.
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT status, last_error_code FROM alarm_dispatch_deliveries
		WHERE dedupe_key = $1 AND cancelled_at IS NOT NULL`, result.Receipts[0].DedupeKey).Scan(&status, &code))
	require.Equal(t, string(StatusCancelled), status)
	require.Equal(t, "room_paused", code)

	records, err := repo.ClaimDue(t.Context(), handoffTestOwner, 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, records, "a delivery for a paused room must never be claimed for sending")

	_, err = pool.Exec(t.Context(), `DELETE FROM kakao_room_delivery_pauses WHERE room_id = $1`, room)
	require.NoError(t, err)

	resumed := receiptEnvelope(room, "after-resume")

	resumed.Notification.Stream.ID = room + "-after-resume"
	result, err = repo.InsertBatch(t.Context(), PublishBatchInput{Envelopes: []AlarmQueueEnvelope{resumed}})
	require.NoError(t, err)
	require.Zero(t, result.PausedRoomDeliveries)
	require.Equal(t, PublishInserted, result.Receipts[0].Outcome)
	require.Equal(t, StatusPending, result.Receipts[0].Status)
}
