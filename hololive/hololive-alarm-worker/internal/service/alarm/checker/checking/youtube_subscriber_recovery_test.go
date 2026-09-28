package checking

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

const (
	recoveryChannelID = "UC_RECOVERY_CHANNEL"
	recoveryStreamID  = "stream-recovery-1"
)

var errSubscriberDBUnavailable = errors.New("subscriber db unavailable")

// failingSubscriberDB는 조회가 일어나면 실패하므로, 조회 여부와 오류 전파를 함께 확인한다.
type failingSubscriberDB struct{}

func (failingSubscriberDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errSubscriberDBUnavailable
}

func (failingSubscriberDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errSubscriberDBUnavailable
}

func (failingSubscriberDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return failingSubscriberRow{}
}

// failingSubscriberRow는 nil 대신 반환되어 Scan에서 같은 조회 실패를 돌려준다.
type failingSubscriberRow struct{}

func (failingSubscriberRow) Scan(...any) error {
	return errSubscriberDBUnavailable
}

func newSubscriberRecoveryChecker(t *testing.T, subscriptionDB dbx.Querier) (*YouTubeChecker, cache.Client) {
	t.Helper()

	startScheduled := time.Now().UTC().Truncate(time.Second).Add(5*time.Minute + 10*time.Second)
	server := newYouTubeCheckerScenarioServer(t, "ok", recoveryChannelID, recoveryStreamID, startScheduled)
	t.Cleanup(server.Close)

	cacheClient := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	holodexService, err := holodexprovider.NewHolodexService(server.URL, "test-key", cacheClient, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeCheckerWithPersistedLiveSource(
		cacheClient, holodexService, tier.NewTieredScheduler(logger), dedup.NewService(cacheClient, []int{5, 3, 1}, logger),
		[]int{5, 3, 1}, 0, nil, subscriptionDB, logger,
	)
	require.NoError(t, err)

	_, err = cacheClient.SAdd(t.Context(), sharedalarmkeys.AlarmChannelRegistryKey, []string{recoveryChannelID})
	require.NoError(t, err)

	return checker, cacheClient
}

func TestYouTubeCheckerCheck_RecoversEvictedSubscriberSetFromDB(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	repo := sharedalarm.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, newCheckerTestLogger())

	for _, alarm := range []*domain.Alarm{
		{RoomID: testRoomID1, ChannelID: recoveryChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
		{RoomID: testRoomID2, ChannelID: recoveryChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity}},
	} {
		require.NoError(t, repo.Add(t.Context(), alarm))
	}

	// 구독 set이 evict된 상태: registry에는 채널이 있지만 alarm:channel_subscribers:{ch}와 empty marker가 모두 없다.
	checker, cacheClient := newSubscriberRecoveryChecker(t, pool)

	notifications, err := checker.Check(t.Context())
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	require.Equal(t, testRoomID1, notifications[0].RoomID)

	// read-through: PG 결과는 이번 cycle에만 쓰고, 동시 구독 해지와 경합하지 않도록 set을 다시 채우지 않는다.
	warmed, err := cacheClient.SMembers(t.Context(), sharedalarmkeys.BuildChannelSubscriberKey(recoveryChannelID, domain.AlarmTypeLive))
	require.NoError(t, err)
	require.Empty(t, warmed)
}

func TestYouTubeCheckerCheck_EmptySubscriberMarkerSkipsDB(t *testing.T) {
	t.Parallel()

	checker, cacheClient := newSubscriberRecoveryChecker(t, failingSubscriberDB{})
	require.NoError(t, cacheClient.Set(
		t.Context(),
		sharedalarmkeys.BuildChannelSubscriberEmptyKey(recoveryChannelID, domain.AlarmTypeLive),
		"1",
		time.Minute,
	))

	notifications, err := checker.Check(t.Context())
	require.NoError(t, err)
	require.Empty(t, notifications)
}

func TestYouTubeCheckerCheck_SubscriberDBErrorFailsCheck(t *testing.T) {
	t.Parallel()

	checker, cacheClient := newSubscriberRecoveryChecker(t, failingSubscriberDB{})

	notifications, err := checker.Check(t.Context())
	require.ErrorIs(t, err, errSubscriberDBUnavailable)
	require.ErrorContains(t, err, "load subscriber rooms")
	require.Nil(t, notifications)

	// DB 오류로 확정하지 못한 채널을 구독 0으로 기록하면 다음 주기까지 알림이 계속 빠진다.
	knownEmpty, err := cacheClient.Exists(t.Context(), sharedalarmkeys.BuildChannelSubscriberEmptyKey(recoveryChannelID, domain.AlarmTypeLive))
	require.NoError(t, err)
	require.False(t, knownEmpty)
}
