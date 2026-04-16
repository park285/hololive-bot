package notification

import (
	"context"
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

func newAlarmCleanupCacheMock(
	t *testing.T,
	doMulti func(context.Context, ...valkey.Completed) []valkey.ValkeyResult,
) *cachemocks.Client {
	t.Helper()

	cacheSvc := newTestCacheService(t.Context(), t)
	client := cachemocks.NewLenientClient()

	client.BuilderFunc = cacheSvc.Builder
	client.BFunc = cacheSvc.B
	client.GetClientFunc = cacheSvc.GetClient
	client.DoMultiFunc = doMulti

	return client
}

func TestRemoveChannelSubscribers_ReturnsErrorOnUnexpectedSRemResultCount(t *testing.T) {
	t.Parallel()

	as := &AlarmService{
		cache: newAlarmCleanupCacheMock(t, func(context.Context, ...valkey.Completed) []valkey.ValkeyResult {
			return nil
		}),
		logger: newDiscardAlarmLogger(),
	}

	err := as.removeChannelSubscribers(
		t.Context(),
		"channel-1",
		"room-1",
		domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected SREM result count")
}

func TestClearChannelSubscribersPipeline_ReturnsErrorOnScardParseFailure(t *testing.T) {
	t.Parallel()

	call := 0
	as := &AlarmService{
		cache: newAlarmCleanupCacheMock(t, func(context.Context, ...valkey.Completed) []valkey.ValkeyResult {
			call++

			results := make([]valkey.ValkeyResult, len(domain.AllAlarmTypes))

			if call == 1 {
				return results
			}

			return results
		}),
		logger: newDiscardAlarmLogger(),
	}

	err := as.clearChannelSubscribersPipeline(t.Context(), []string{"channel-1"}, "room-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scard key")
}

func TestCleanupChannelRegistryIfEmpty_ReturnsErrorWhenRemovingRegistryEntryFails(t *testing.T) {
	t.Parallel()

	cacheSvc := newTestCacheService(t.Context(), t)
	as := &AlarmService{
		cache: &cachemocks.Client{
			BuilderFunc: cacheSvc.Builder,
			BFunc:       cacheSvc.B,
			GetClientFunc: func() valkey.Client {
				return cacheSvc.GetClient()
			},
			DoMultiFunc: cacheSvc.DoMulti,
			SRemFunc: func(context.Context, string, []string) (int64, error) {
				return 0, errors.New("remove failed")
			},
		},
		logger: newDiscardAlarmLogger(),
	}

	err := as.cleanupChannelRegistryIfEmpty(t.Context(), "channel-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove channel registry entry")
}
