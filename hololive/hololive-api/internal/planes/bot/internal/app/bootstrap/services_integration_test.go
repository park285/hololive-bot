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
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCoreIntegrationServicesReturnsACLInitializationError(t *testing.T) {
	t.Parallel()

	services, err := InitCoreIntegrationServices(
		context.Background(),
		&config.Config{
			Kakao: config.KakaoConfig{
				ACLEnabled: true,
				ACLMode:    "whitelist",
				Rooms:      []string{"room-a"},
			},
		},
		&sharedmodules.InfraModule{
			Cache:    cachemocks.NewLenientClient(),
			Postgres: nil,
		},
		slog.New(slog.DiscardHandler),
	)

	require.Nil(t, services)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to create ACL service")
	assert.ErrorContains(t, err, "postgres service is nil")
}

func TestInitCoreIntegrationServicesCreatesWorkerPool(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), bootstrapTestContextKey{}, "integration-context")
	cacheClient, observedCalls := newACLCacheSyncMock(t, "integration-context")

	services, err := InitCoreIntegrationServices(
		ctx,
		&config.Config{
			Kakao: config.KakaoConfig{
				ACLEnabled: true,
				ACLMode:    "whitelist",
				Rooms:      []string{"room-a"},
			},
			Server:     config.ServerConfig{APIKey: "test-api-key"},
			WorkerPool: config.WorkerPoolConfig{Workers: 10, QueueSize: 100},
		},
		&sharedmodules.InfraModule{
			Cache:    cacheClient,
			Postgres: newACLPostgresMock(t),
		},
		slog.New(slog.DiscardHandler),
	)

	require.NoError(t, err)
	require.NotNil(t, services)
	require.NotNil(t, services.WorkerPool)
	t.Cleanup(services.WorkerPool.StopAndWait)
	assert.Equal(t, 10, services.WorkerPool.Workers())
	assert.NotNil(t, services.ACLService)
	assert.Empty(t, services.CommandBuilders)
	assert.Greater(t, observedCalls.Load(), int64(0))
}
