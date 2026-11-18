package subscriptions

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	dbtest "github.com/kapu/hololive-dbtest"
	targetpolicy "github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

const (
	testRoomID         = "room-1"
	testAltRoomID      = "room1"
	testUserID         = "user-1"
	testChannelID      = "ch-1"
	testOtherChannelID = "ch-2"
	testMemberName     = "Miko"
	testUCChannelID    = "UC_TEST"
	testAlphaChannelID = "UC_alpha"

	testMetricLabelOperation = "operation"
	testMetricLabelResult    = "result"
	testWarmOperation        = "warm"
	testFallbackChannelID    = "default"
)

type mockMemberDataProvider struct {
	members []*domain.Member
}

func (m *mockMemberDataProvider) FindMemberByChannelID(_ context.Context, channelID string) (*domain.Member, error) {
	for _, member := range m.members {
		if member.ChannelID == channelID {
			return member, nil
		}
	}

	return nil, domain.ErrMemberNotFound
}

func (m *mockMemberDataProvider) FindMemberByName(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (m *mockMemberDataProvider) FindMemberByAlias(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (m *mockMemberDataProvider) GetChannelIDs(context.Context) ([]string, error) {
	return []string{}, nil
}

func (m *mockMemberDataProvider) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	return m.members, nil
}

func newTestAlarmService(t *testing.T) *AlarmService {
	t.Helper()

	ctx := t.Context()
	cacheClient := sharedtestutil.NewTestCacheService(ctx, t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	pool := dbtest.NewPool(t)
	repository := alarmstore.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)

	return &AlarmService{
		cache:           cacheClient,
		alarmRepository: repository,
		alarmWriter:     repository,
		logger:          logger,
		targetPolicy:    targetpolicy.NewTargetMinutePolicyFromConfigured([]int{30, 15, 5, 1}),
	}
}

func roomChannelIDs(ctx context.Context, as *AlarmService, roomID string) ([]string, error) {
	alarms, err := as.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("room channel ids: %w", err)
	}

	return uniqueAlarmChannelIDs(alarms), nil
}

func channelSubscriberRooms(ctx context.Context, as *AlarmService, channelID string, alarmType domain.AlarmType) ([]string, error) {
	alarms, err := as.alarmRepository.FindByChannelAndType(ctx, channelID, alarmType)
	if err != nil {
		return nil, fmt.Errorf("channel subscriber rooms: %w", err)
	}

	rooms := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		rooms = append(rooms, alarm.RoomID)
	}

	slices.Sort(rooms)

	return slices.Compact(rooms), nil
}
