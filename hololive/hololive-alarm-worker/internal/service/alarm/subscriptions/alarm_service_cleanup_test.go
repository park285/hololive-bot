package subscriptions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestCleanupChannelRegistryIfEmpty_ReturnsErrorWhenRemovingRegistryEntryFails(t *testing.T) {
	t.Parallel()

	cache := sharedtestutil.NewTestCacheService(t.Context(), t)
	as := newTestAlarmService(t)
	removeErr := errors.New("remove failed")

	as.cache = &cachemocks.Client{
		BuilderFunc: cache.Builder,
		BFunc:       cache.B,
		GetClientFunc: func() valkey.Client {
			return cache.GetClient()
		},
		DoMultiFunc: cache.DoMulti,
		SRemFunc: func(context.Context, string, []string) (int64, error) {
			return 0, removeErr
		},
	}

	err := as.cleanupChannelRegistryIfEmpty(t.Context(), "channel-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, removeErr)
}
