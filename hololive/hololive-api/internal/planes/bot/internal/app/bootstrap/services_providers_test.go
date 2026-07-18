// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package bootstrap

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bootstrapTestContextKey struct{}

func TestProvideACLServiceWrapsInitializationErrorForNilPostgres(t *testing.T) {
	t.Parallel()

	service, err := ProvideACLService(
		context.Background(),
		true,
		acl.ACLModeWhitelist,
		[]string{"room-a"},
		nil,
		cachemocks.NewLenientClient(),
		slog.New(slog.DiscardHandler),
	)

	require.Nil(t, service)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to create ACL service")
	assert.ErrorContains(t, err, "postgres service is nil")
}

func TestProvideACLServicePropagatesContextToInitialCacheSync(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), bootstrapTestContextKey{}, "acl-context")
	cacheClient, observedCalls := newACLCacheSyncMock(t, "acl-context")

	service, err := ProvideACLService(
		ctx,
		true,
		acl.ACLModeWhitelist,
		[]string{"room-a"},
		newACLPostgresMock(t),
		cacheClient,
		slog.New(slog.DiscardHandler),
	)

	require.NoError(t, err)
	require.NotNil(t, service)
	assert.Greater(t, observedCalls.Load(), int64(0))
}

func TestProvideActivityLoggerReturnsLogger(t *testing.T) {
	t.Parallel()

	logger := ProvideActivityLogger(slog.New(slog.DiscardHandler))

	require.NotNil(t, logger)
	logs, err := logger.GetRecentLogs(1)
	require.NoError(t, err)
	assert.Empty(t, logs)
}

func TestProvideBotDependenciesMapsOptionalYouTubeStack(t *testing.T) {
	t.Parallel()

	t.Run("nil stack leaves YouTube dependencies nil", func(t *testing.T) {
		t.Parallel()

		deps := ProvideBotDependencies(&BotDependencyModules{
			Stream: BotStreamModule{YTStack: nil},
		})

		require.NotNil(t, deps)
		assert.Nil(t, deps.Service)
	})

	t.Run("populated stack wires YouTube dependencies", func(t *testing.T) {
		t.Parallel()

		youTubeService := &stubYouTubeService{}

		deps := ProvideBotDependencies(&BotDependencyModules{
			Stream: BotStreamModule{
				YTStack: &providers.YouTubeStack{
					Service: youTubeService,
				},
			},
		})

		require.NotNil(t, deps)
		assert.Same(t, youTubeService, deps.Service)
	})
}

func newACLPostgresMock(t *testing.T) *databasemocks.Client {
	t.Helper()

	pool := dbtest.NewPool(t)

	return &databasemocks.Client{
		GetPoolFunc: func() *pgxpool.Pool { return pool },
	}
}

func newACLCacheSyncMock(t *testing.T, wantContextValue string) (*cachemocks.Client, *atomic.Int64) {
	t.Helper()

	var observedCalls atomic.Int64
	recordContext := func(ctx context.Context) {
		assert.Equal(t, wantContextValue, ctx.Value(bootstrapTestContextKey{}))
		observedCalls.Add(1)
	}

	cacheClient := &cachemocks.Client{
		SetFunc: func(ctx context.Context, _ string, _ any, _ time.Duration) error {
			recordContext(ctx)
			return nil
		},
		DelFunc: func(ctx context.Context, _ string) error {
			recordContext(ctx)
			return nil
		},
		SAddFunc: func(ctx context.Context, _ string, members []string) (int64, error) {
			recordContext(ctx)
			return int64(len(members)), nil
		},
	}

	return cacheClient, &observedCalls
}
