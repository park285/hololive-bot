package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

const (
	testExistingRoomA = "existing-a"
	testExistingRoomB = "existing-b"
	testNewRoomC      = "new-c"
)

type commitHookRepository struct {
	*sharedalarm.Repository

	afterCommit func()
}

func (r *commitHookRepository) committed(err error) error {
	if err != nil {
		return fmt.Errorf("commit subscription: %w", err)
	}

	r.afterCommit()

	return nil
}

func (r *commitHookRepository) Add(ctx context.Context, alarm *domain.Alarm) error {
	return r.committed(r.Repository.Add(ctx, alarm))
}

func (r *commitHookRepository) Remove(ctx context.Context, roomID, channelID string) error {
	return r.committed(r.Repository.Remove(ctx, roomID, channelID))
}

func (r *commitHookRepository) ClearByRoom(ctx context.Context, roomID string) (int64, error) {
	count, err := r.Repository.ClearByRoom(ctx, roomID)
	return count, r.committed(err)
}

type commitMutationCase struct {
	name          string
	mutate        func(context.Context, *AlarmService) error
	wantLive      []string
	wantCommunity []string
	wantShorts    []string
	wantNew       []string
}

func commitMutationCases() []commitMutationCase {
	return []commitMutationCase{
		{
			name: "add-room",
			mutate: func(ctx context.Context, service *AlarmService) error {
				_, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testNewRoomC, ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
				return err
			},
			wantLive: []string{testExistingRoomA, testExistingRoomB, testNewRoomC}, wantCommunity: []string{testExistingRoomA, testExistingRoomB},
		},
		{
			name: "add-type",
			mutate: func(ctx context.Context, service *AlarmService) error {
				_, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testExistingRoomA, ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts}})
				return err
			},
			wantLive: []string{testExistingRoomA, testExistingRoomB}, wantCommunity: []string{testExistingRoomA, testExistingRoomB}, wantShorts: []string{testExistingRoomA},
		},
		{
			name: "remove-room",
			mutate: func(ctx context.Context, service *AlarmService) error {
				_, err := service.RemoveAlarm(ctx, testExistingRoomA, testChannelID, nil)
				return err
			},
			wantLive: []string{testExistingRoomB}, wantCommunity: []string{testExistingRoomB},
		},
		{
			name: "remove-type",
			mutate: func(ctx context.Context, service *AlarmService) error {
				_, err := service.RemoveAlarm(ctx, testExistingRoomA, testChannelID, domain.AlarmTypes{domain.AlarmTypeLive})
				return err
			},
			wantLive: []string{testExistingRoomB}, wantCommunity: []string{testExistingRoomA, testExistingRoomB},
		},
		{
			name: "clear-room",
			mutate: func(ctx context.Context, service *AlarmService) error {
				_, err := service.ClearRoomAlarms(ctx, testExistingRoomA)
				return err
			},
			wantLive: []string{testExistingRoomB}, wantCommunity: []string{testExistingRoomB},
		},
		{
			name: "add-new-channel",
			mutate: func(ctx context.Context, service *AlarmService) error {
				_, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testNewRoomC, ChannelID: "new-channel", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
				return err
			},
			wantLive: []string{testExistingRoomA, testExistingRoomB}, wantCommunity: []string{testExistingRoomA, testExistingRoomB}, wantNew: []string{testNewRoomC},
		},
	}
}

func newCommitBoundaryService(t *testing.T) (*AlarmService, *pgxpool.Pool, *cachemocks.Client) {
	t.Helper()

	pool := dbtest.NewPool(t)
	logger := newDiscardAlarmLogger()
	repository := sharedalarm.NewRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)
	cacheClient, _ := newLenientAlarmCacheMock(t.Context(), t, nil)
	service, err := NewAlarmService(cacheClient, nil, repository, logger, []int{5})
	require.NoError(t, err)

	for _, room := range []string{testExistingRoomA, testExistingRoomB} {
		_, err = service.AddAlarm(t.Context(), &domain.AddAlarmRequest{
			RoomID: room, ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeCommunity},
		})
		require.NoError(t, err)
	}

	require.NoError(t, service.WarmCacheFromDB(t.Context()))

	return service, pool, cacheClient
}

func failCommittedCacheWrites(client *cachemocks.Client) {
	cacheErr := errors.New("cache unavailable after commit")

	client.HDelFunc = func(context.Context, string, ...string) error { return cacheErr }
	client.HSetFunc = func(context.Context, string, string, string) error { return cacheErr }
	client.DelFunc = func(context.Context, string) error { return cacheErr }
	client.ScanKeyPagesFunc = func(context.Context, string, int64, func([]string) error) error { return cacheErr }
	client.DoMultiFunc = func(context.Context, ...valkey.Completed) []valkey.ValkeyResult { return nil }
}

func TestSubscriptionCommitBoundaryPreservesRecipients(t *testing.T) {
	for _, failure := range []string{"request-canceled", "cache-unavailable"} {
		t.Run(failure, func(t *testing.T) {
			for _, mutation := range commitMutationCases() {
				t.Run(mutation.name, func(t *testing.T) {
					service, pool, client := newCommitBoundaryService(t)
					ctx, cancel := context.WithCancel(t.Context())

					defer cancel()

					committed := false

					service.alarmWriter = &commitHookRepository{Repository: service.alarmRepository, afterCommit: func() {
						committed = true

						if failure == "request-canceled" {
							cancel()
						} else {
							failCommittedCacheWrites(client)
						}
					}}

					err := mutation.mutate(ctx, service)

					require.True(t, committed)

					if failure == "request-canceled" {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}

					assertCommittedRecipients(t, service, pool, mutation)
					// commit 후 오류를 받은 호출자가 재시도해도 이미 반영된 구독이 되돌아가면 안 된다.
					require.NoError(t, mutation.mutate(t.Context(), service))
					assertCommittedRecipients(t, service, pool, mutation)
				})
			}
		})
	}
}

func assertCommittedRecipients(t *testing.T, service *AlarmService, pool *pgxpool.Pool, mutation commitMutationCase) {
	t.Helper()

	resolver := sharedalarm.NewSubscriberResolver(service.cache, pool)

	for kind, expected := range map[domain.AlarmType][]string{
		domain.AlarmTypeLive: mutation.wantLive, domain.AlarmTypeCommunity: mutation.wantCommunity, domain.AlarmTypeShorts: mutation.wantShorts,
	} {
		rooms, err := resolver.ResolveEventSubscribers(t.Context(), testChannelID, "", kind)
		require.NoError(t, err)
		require.ElementsMatch(t, expected, rooms, "alarm type: %s", kind)
	}

	rooms, err := resolver.ResolveEventSubscribers(t.Context(), "new-channel", "", domain.AlarmTypeLive)
	require.NoError(t, err)
	require.ElementsMatch(t, mutation.wantNew, rooms)

	if len(mutation.wantNew) > 0 {
		channels, err := service.cache.SMembers(t.Context(), sharedalarmkeys.AlarmChannelRegistryKey)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{testChannelID, "new-channel"}, channels)
	}
}

func TestFailedNegativeInvalidationLeavesDatabaseAndRecipientsUnchanged(t *testing.T) {
	service, pool, client := newCommitBoundaryService(t)
	ctx := t.Context()
	emptyKey := sharedalarmkeys.BuildChannelSubscriberEmptyKey(testChannelID, domain.AlarmTypeLive)
	require.NoError(t, client.Set(ctx, emptyKey, "1", time.Minute))

	originalDel := client.DelFunc
	cacheErr := errors.New("negative marker invalidation failed")

	client.DelFunc = func(ctx context.Context, key string) error {
		if key == emptyKey {
			return cacheErr
		}

		return originalDel(ctx, key)
	}

	added, err := service.AddAlarm(ctx, &domain.AddAlarmRequest{RoomID: testNewRoomC, ChannelID: testChannelID, AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}})
	require.ErrorIs(t, err, cacheErr)
	require.False(t, added)

	alarms, err := service.alarmRepository.FindByRoom(ctx, testNewRoomC)
	require.NoError(t, err)
	require.Empty(t, alarms)

	rooms, err := sharedalarm.NewSubscriberResolver(client, pool).ResolveEventSubscribers(ctx, testChannelID, "", domain.AlarmTypeLive)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{testExistingRoomA, testExistingRoomB}, rooms)
}
