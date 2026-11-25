package checking

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

const (
	recoveryChannelID = "UC_RECOVERY_CHANNEL"
	recoveryStreamID  = "stream-recovery-1"
)

var errSubscriberDBUnavailable = errors.New("subscriber db unavailable")

type failingSubscriberDB struct{}

func (failingSubscriberDB) Begin(context.Context) (pgx.Tx, error) {
	return nil, errSubscriberDBUnavailable
}

func (failingSubscriberDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errSubscriberDBUnavailable
}

func (failingSubscriberDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errSubscriberDBUnavailable
}

func (failingSubscriberDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return failingSubscriberRow{}
}

type failingSubscriberRow struct{}

func (failingSubscriberRow) Scan(...any) error {
	return errSubscriberDBUnavailable
}

func newSubscriberRecoveryChecker(t *testing.T, subscriptionDB pgxdb.Querier) (*YouTubeChecker, cache.Client) {
	t.Helper()

	startScheduled := time.Now().UTC().Truncate(time.Second).Add(5*time.Minute + 10*time.Second)
	server := newYouTubeCheckerScenarioServer(t, "ok", recoveryChannelID, recoveryStreamID, startScheduled)
	t.Cleanup(server.Close)

	cacheClient := newCheckerTestCacheClient(t)
	logger := newCheckerTestLogger()
	holodexService, err := holodexprovider.NewHolodexService(new(settings.DefaultHolodexOperationalConfig()), server.URL, "test-key", cacheClient, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeCheckerWithPersistedLiveSource(
		cacheClient, holodexService, tier.NewTieredScheduler(logger), dedup.NewService(cacheClient, logger),
		0, nil, subscriptionDB, logger,
	)
	require.NoError(t, err)

	_, err = cacheClient.SAdd(t.Context(), alarmkeys.AlarmChannelRegistryKey, []string{recoveryChannelID})
	require.NoError(t, err)

	return checker, cacheClient
}

func TestYouTubeCheckerCheck_RecoversEvictedSubscriberSetFromDB(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	repo := alarmstore.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, newCheckerTestLogger())

	for _, alarm := range []*domain.Alarm{
		{RoomID: testRoomID1, ChannelID: recoveryChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
		{RoomID: testRoomID2, ChannelID: recoveryChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity}},
	} {
		require.NoError(t, repo.Add(t.Context(), alarm))
	}

	checker, cacheClient := newSubscriberRecoveryChecker(t, pool)

	notifications, err := checker.Check(t.Context(), testTargetPolicy())
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	require.Equal(t, testRoomID1, notifications[0].RoomID)

	warmed, err := cacheClient.SMembers(t.Context(), alarmkeys.BuildChannelSubscriberKey(recoveryChannelID, domain.AlarmTypeLive))
	require.NoError(t, err)
	require.Empty(t, warmed)
}

func TestYouTubeCheckerCheck_CachedSubscribersDoNotHidePendingDBFailure(t *testing.T) {
	t.Parallel()

	checker, cacheClient := newSubscriberRecoveryChecker(t, failingSubscriberDB{})
	_, err := cacheClient.SAdd(t.Context(), alarmkeys.BuildChannelSubscriberKey(recoveryChannelID, domain.AlarmTypeLive), []string{testRoomID1})
	require.NoError(t, err)

	notifications, err := checker.Check(t.Context(), testTargetPolicy())
	require.ErrorIs(t, err, errSubscriberDBUnavailable)
	require.ErrorContains(t, err, "load pending")
	require.Empty(t, notifications)
}

func TestYouTubeCheckerCheck_SubscriberDBErrorFailsCheck(t *testing.T) {
	t.Parallel()

	checker, _ := newSubscriberRecoveryChecker(t, failingSubscriberDB{})

	notifications, err := checker.Check(t.Context(), testTargetPolicy())
	require.ErrorIs(t, err, errSubscriberDBUnavailable)
	require.ErrorContains(t, err, "load subscriber rooms")
	require.Nil(t, notifications)
}

func TestYouTubeCheckerIncludesObservationReceivedDuringFailedProviderRequest(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC()
	startedAt := now.Add(-time.Minute)
	insertLiveSessions(t, pool, []domain.YouTubeLiveSession{{
		VideoID: recoveryStreamID, ChannelID: recoveryChannelID, Status: domain.LiveStatusLive,
		Title: "fresh during provider request", StartedAt: &startedAt, LiveFirstSeenAt: &startedAt,
		LastSeenAt: now, StatusObservedAt: new(now.Add(-persistedLiveSessionRecentWindow - time.Second)),
	}})

	logger := newCheckerTestLogger()
	repo := alarmstore.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)
	require.NoError(t, repo.Add(ctx, &domain.Alarm{RoomID: testRoomID1, ChannelID: recoveryChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}}))

	updateErrors := make(chan error, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, err := pool.Exec(request.Context(), "UPDATE youtube_live_sessions SET status_observed_at = $1 WHERE video_id = $2", time.Now().UTC(), recoveryStreamID)
		updateErrors <- err

		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	cacheClient := newCheckerTestCacheClient(t)
	_, err := cacheClient.SAdd(ctx, alarmkeys.AlarmChannelRegistryKey, []string{recoveryChannelID})
	require.NoError(t, err)

	holodexService, err := holodexprovider.NewHolodexService(new(settings.DefaultHolodexOperationalConfig()), server.URL, "test-key", cacheClient, nil, logger)
	require.NoError(t, err)

	checker, err := NewYouTubeCheckerWithPersistedLiveSource(
		cacheClient, holodexService, tier.NewTieredScheduler(logger), dedup.NewService(cacheClient, logger),
		0, &PgYouTubeLiveSessionSource{pool: pool}, pool, logger,
	)
	require.NoError(t, err)

	notifications, err := checker.Check(ctx, testTargetPolicy(5))
	require.NoError(t, err)
	require.Len(t, notifications, 1)
	require.Equal(t, recoveryStreamID, notifications[0].Stream.ID)
	require.Equal(t, testRoomID1, notifications[0].RoomID)

	server.Close()
	close(updateErrors)

	for updateErr := range updateErrors {
		require.NoError(t, updateErr)
	}
}
