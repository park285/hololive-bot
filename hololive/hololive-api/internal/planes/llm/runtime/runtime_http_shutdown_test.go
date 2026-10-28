package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

func runLLMHTTPRequest(client *http.Client, request *http.Request) error {
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request trigger: %w", err)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("trigger status: %d", response.StatusCode)
	}

	return nil
}

func TestLLMHTTPTimeoutWaitsForMetricsAndPprofHandlersBeforeCleanup(t *testing.T) {
	for _, listener := range []string{"metrics", "pprof"} {
		t.Run(listener, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(entered)
				<-release
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)
			t.Cleanup(unblock)

			servers := &sharedserver.RuntimeHTTPServers{}

			if listener == "metrics" {
				servers.Metrics = server.Config
			} else {
				servers.Pprof = server.Config
			}

			var cleanups atomic.Int32

			runtime := &LLMSchedulerRuntime{
				Logger: testRuntimeLogger(), httpServers: servers,
				Managed: lifecycle.NewManaged(func() { cleanups.Add(1) }),
			}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
			require.NoError(t, err)

			requestDone := make(chan error, 1)

			go func() { requestDone <- runLLMHTTPRequest(server.Client(), request) }()

			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP handler did not start")
			}

			for range 2 {
				ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
				require.ErrorIs(t, runtime.CloseContext(ctx), context.DeadlineExceeded)
				cancel()
				assert.Zero(t, cleanups.Load())
			}

			unblock()
			require.NoError(t, <-requestDone)

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			require.ErrorIs(t, runtime.CloseContext(ctx), context.DeadlineExceeded)
			assert.Equal(t, int32(1), cleanups.Load())
		})
	}
}
