package adminruntime

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

const testLoopbackAddr = "127.0.0.1:0"

type adminCloseFunc func() error

func (f adminCloseFunc) Close() error { return f() }

type adminPhotoSyncFunc func(context.Context)

func (f adminPhotoSyncFunc) Start(ctx context.Context) { f(ctx) }

func TestAdminHTTPBuildFailureClosesWholeOwner(t *testing.T) {
	t.Parallel()

	for _, stage := range []string{"router", "server"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()

			var calls []string

			closeErr := errors.New("trigger transport close failed")
			resources := newAdminResourceFixture(t, &calls, closeErr)
			config := &apiconfig.AdminPlaneConfig{}

			if stage == "server" {
				config.Server = settings.ServerConfig{APIKey: testAPIKey, HTTPTransports: []string{"h3"}, H3Addr: testLoopbackAddr}
				config.CORS.AllowedOrigins = []string{testAllowedOrigin}
			}

			runtime, err := buildAdminAPIHTTPRuntime(t.Context(), config, resources.infra, nil, &server.Handler{}, resources.Close, slog.New(slog.DiscardHandler))
			require.Nil(t, runtime)
			require.ErrorIs(t, err, closeErr)

			if stage == "router" {
				require.ErrorContains(t, err, "provide api router")
			} else {
				require.ErrorContains(t, err, "http server")
			}

			require.ErrorIs(t, resources.Close(), closeErr)
			assert.Equal(t, []string{"members", "holodex", "rooms", "collector", "trigger", "alarm", "infra"}, calls)
		})
	}
}

func newAdminResourceFixture(t *testing.T, calls *[]string, closeErr error) *adminRuntimeResources {
	t.Helper()

	closer := func(name string, err error) adminCloseFunc {
		return func() error { *calls = append(*calls, name); return err }
	}

	return &adminRuntimeResources{
		infra: &sharedmodules.InfraModule{
			Cache: sharedtestutil.NewTestCacheService(t.Context(), t), Postgres: &databasemocks.Client{},
			StopMemberCache: func() { *calls = append(*calls, "members") },
			Cleanup:         func() { *calls = append(*calls, "infra") },
		},
		holodex: stopFunc(func() { *calls = append(*calls, "holodex") }),
		alarm:   closer("alarm", nil), trigger: closer("trigger", closeErr),
		collector: closer("collector", nil), rooms: closer("rooms", nil),
	}
}

func TestAdminCloseBeforeStartTransfersOwnerExactlyOnce(t *testing.T) {
	t.Parallel()

	var cleanups, starts atomic.Int32

	runtime := &AdminAPIRuntime{
		Managed:   lifecycle.NewManaged(func() { cleanups.Add(1) }),
		PhotoSync: adminPhotoSyncFunc(func(context.Context) { starts.Add(1) }),
	}

	var callers sync.WaitGroup

	for range 8 {
		callers.Go(func() { assert.NoError(t, runtime.CloseContext(t.Context())) })
	}

	callers.Wait()
	runtime.Start(t.Context(), nil)
	assert.Equal(t, int32(1), cleanups.Load())
	assert.Zero(t, starts.Load())
}

func TestAdminPhotoSyncJoinTimeoutPreservesInfrastructure(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})

		var cleanups atomic.Int32

		runtime := &AdminAPIRuntime{
			Managed:   lifecycle.NewManaged(func() { cleanups.Add(1) }),
			Logger:    slog.New(slog.DiscardHandler),
			PhotoSync: adminPhotoSyncFunc(func(ctx context.Context) { <-ctx.Done(); <-release }),
		}
		runtime.Start(t.Context(), nil)
		synctest.Wait()

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)

		defer cancel()

		require.ErrorIs(t, runtime.Shutdown(ctx), context.DeadlineExceeded)
		require.ErrorIs(t, runtime.CloseContext(ctx), context.DeadlineExceeded)
		assert.Zero(t, cleanups.Load())
		close(release)
		synctest.Wait()
		require.ErrorIs(t, runtime.CloseContext(t.Context()), context.DeadlineExceeded)
		assert.Equal(t, int32(1), cleanups.Load())
	})
}

func TestAdminHTTPDrainTimeoutThenJoinClosesResourcesAndPreservesError(t *testing.T) {
	t.Parallel()

	runtime, release, requestDone, cleanups := newAdminBlockingHTTPRuntime(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)

	defer cancel()

	require.ErrorIs(t, runtime.Shutdown(ctx), context.DeadlineExceeded)
	require.ErrorIs(t, runtime.CloseContext(ctx), context.DeadlineExceeded)
	assert.Zero(t, cleanups.Load())

	release()
	require.NoError(t, <-requestDone)
	require.ErrorIs(t, runtime.CloseContext(t.Context()), context.DeadlineExceeded)
	assert.Equal(t, int32(1), cleanups.Load())
}

func newAdminBlockingHTTPRuntime(t *testing.T) (*AdminAPIRuntime, func(), <-chan error, *atomic.Int32) {
	t.Helper()

	release := make(chan struct{})
	entered := make(chan struct{})
	httpServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))
	t.Cleanup(httpServer.Close)

	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)

	cleanups := new(atomic.Int32)
	runtime := &AdminAPIRuntime{
		started:     true,
		Managed:     lifecycle.NewManaged(func() { cleanups.Add(1) }),
		Logger:      slog.New(slog.DiscardHandler),
		HTTPServers: &sharedserver.RuntimeHTTPServers{Metrics: httpServer.Config},
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, httpServer.URL, http.NoBody)
	require.NoError(t, err)

	requestDone := make(chan error, 1)

	go func() {
		response, requestErr := httpServer.Client().Do(request)
		if requestErr == nil {
			requestErr = response.Body.Close()
		}

		requestDone <- requestErr
	}()

	<-entered

	return runtime, releaseOnce, requestDone, cleanups
}

func TestAdminCleanupDeadlineDoesNotStartAnotherCleanup(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})

		var cleanups atomic.Int32

		runtime := &AdminAPIRuntime{cleanup: func() error {
			cleanups.Add(1)
			<-release

			return nil
		}}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)

		defer cancel()

		require.ErrorIs(t, runtime.CloseContext(ctx), context.DeadlineExceeded)
		assert.Equal(t, int32(1), cleanups.Load())
		close(release)
		synctest.Wait()
		require.NoError(t, runtime.CloseContext(t.Context()))
		assert.Equal(t, int32(1), cleanups.Load())
	})
}
