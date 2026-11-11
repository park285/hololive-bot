package dispatchoutbox

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	testRoomID             = "room-1"
	testOtherRoomID        = "room-2"
	testChannelID          = "channel-1"
	testStreamID           = "stream-1"
	testUserName           = "alice"
	testYouTubeChannelID   = "UC_test"
	testClaimEventCategory = "claim:event"
	testEventKey           = "key1"
)

func newTestRepository(tb testing.TB, pool *pgxpool.Pool) *PgxRepository {
	tb.Helper()

	repository, err := NewPgxRepository(dbtest.WrapPool(pool), nil)
	require.NoError(tb, err)

	return repository
}

// ledgerDedupeKey는 envelope을 적재할 때 쓰는 방별 delivery key다.
func ledgerDedupeKey(tb testing.TB, envelope *AlarmQueueEnvelope) string {
	tb.Helper()

	_, delivery, err := buildLedgerRows(envelope)
	require.NoError(tb, err)

	return delivery.DedupeKey
}
