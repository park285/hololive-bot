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
	testDigestPeriodKey    = "2026-08"
	testEventKey           = "key1"
)

// newTestRepository는 production 생성자에 테스트 pool을 dbtest.WrapPool로 감싸 넘긴다.
func newTestRepository(tb testing.TB, pool *pgxpool.Pool) *PgxRepository {
	tb.Helper()

	repository, err := NewPgxRepository(dbtest.WrapPool(pool), nil)
	require.NoError(tb, err)

	return repository
}
