package runtime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiserver "github.com/kapu/hololive-api/internal/httpapi"
	"github.com/kapu/hololive-shared/pkg/applifecycle"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/contracts/common"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type llmHTTPTriggerScheduler struct {
	entered chan context.Context
	release chan struct{}
}

func (s *llmHTTPTriggerScheduler) SendWeeklyNotification(ctx context.Context) error {
	s.entered <- ctx

	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("trigger execution: %w", ctx.Err())
	}
}

type llmHTTPTriggerFixture struct {
	runtime     *LLMSchedulerRuntime
	scheduler   *llmHTTPTriggerScheduler
	unblock     func()
	requestDone <-chan error
	cleanups    *atomic.Int32
}

// 실제 인증된 trigger와 H3를 연결하고 scheduler 실행 시간만 제어한다.
func newLLMHTTPTriggerFixture(t *testing.T) *llmHTTPTriggerFixture {
	t.Helper()

	certFile, keyFile, roots := llmHTTPTestCertificate(t)
	scheduler := &llmHTTPTriggerScheduler{entered: make(chan context.Context, 1), release: make(chan struct{})}
	unblock := sync.OnceFunc(func() { close(scheduler.release) })
	logger := testRuntimeLogger()
	handler := apiserver.NewTriggerHandler(scheduler, nil, nil, logger)
	apiKey := t.Name()
	router, err := buildTriggerRouter(t.Context(), logger, handler, apiKey)
	require.NoError(t, err)

	servers, err := sharedserver.NewRuntimeHTTPServers(t.Context(), &settings.ServerConfig{
		HTTPTransports: []string{"h3"}, H3Addr: "127.0.0.1:0", H3CertFile: certFile, H3KeyFile: keyFile,
	}, router, "test.llm.trigger.shutdown", nil)
	require.NoError(t, err)

	var listenConfig net.ListenConfig

	packetConn, err := listenConfig.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := packetConn.LocalAddr()
	require.NotNil(t, addr)

	served := make(chan error, 1)

	go func() { served <- servers.H3.Serve(packetConn) }()

	transport := &http3.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"https://"+addr.String()+triggercontracts.MajorEventWeeklyPath, http.NoBody)
	require.NoError(t, err)
	request.Header.Set(common.APIKeyHeader, apiKey)

	requestDone := make(chan error, 1)

	go func() { requestDone <- runLLMHTTPTriggerRequest(client, request) }()

	cleanups := new(atomic.Int32)
	runtime := &LLMSchedulerRuntime{
		Logger: logger, httpServers: servers,
		Managed: lifecycle.NewManaged(func() { cleanups.Add(1) }),
	}

	t.Cleanup(func() {
		unblock()
		require.NoError(t, servers.H3.Close())
		require.NoError(t, transport.Close())
		require.NoError(t, packetConn.Close())

		select {
		case serveErr := <-served:
			require.ErrorIs(t, serveErr, http.ErrServerClosed)
		case <-time.After(2 * time.Second):
			t.Error("H3 serve task did not finish")
		}
	})

	return &llmHTTPTriggerFixture{
		runtime: runtime, scheduler: scheduler, unblock: unblock, requestDone: requestDone, cleanups: cleanups,
	}
}

func llmHTTPTestCertificate(t *testing.T) (certFile, keyFile string, roots *x509.CertPool) {
	t.Helper()

	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(fixture.Close)

	pair := fixture.TLS.Certificates[0]
	privateKey, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	require.NoError(t, err)

	certFile = filepath.Join(t.TempDir(), "cert.pem")
	keyFile = filepath.Join(t.TempDir(), "key.pem")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]}), 0o600))
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}), 0o600))

	roots = x509.NewCertPool()
	roots.AddCert(fixture.Certificate())

	return certFile, keyFile, roots
}

func runLLMHTTPTriggerRequest(client *http.Client, request *http.Request) error {
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

func awaitLLMHTTPTrigger(t *testing.T, fixture *llmHTTPTriggerFixture) context.Context {
	t.Helper()

	select {
	case ctx := <-fixture.scheduler.entered:
		return ctx
	case <-time.After(2 * time.Second):
		t.Fatal("authenticated H3 trigger did not enter scheduler")

		return nil
	}
}

func awaitLLMHTTPClose(t *testing.T, result <-chan error) error {
	t.Helper()

	select {
	case err := <-result:
		return err
	case <-time.After(500 * time.Millisecond):
		t.Fatal("LLM close exceeded its caller deadline")

		return nil
	}
}

func TestLLMH3TriggerDeadlineAllowsLaterPlaneDrainAndConcurrentClose(t *testing.T) {
	fixture := newLLMHTTPTriggerFixture(t)
	executionCtx := awaitLLMHTTPTrigger(t, fixture)

	var youtubeDrains atomic.Int32

	group := applifecycle.NewGroupRuntime(testRuntimeLogger(),
		applifecycle.GroupComponent{Name: "youtube", Shutdown: func(context.Context) error {
			youtubeDrains.Add(1)

			return nil
		}},
		applifecycle.GroupComponent{Name: "llm", Shutdown: fixture.runtime.Shutdown},
		applifecycle.GroupComponent{Name: "admin", Shutdown: func(context.Context) error { return nil }},
		applifecycle.GroupComponent{Name: "bot", Shutdown: func(context.Context) error { return nil }},
	)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)

	defer cancel()

	result := make(chan error, 1)

	go func() { result <- group.Shutdown(ctx) }()

	require.ErrorIs(t, awaitLLMHTTPClose(t, result), context.DeadlineExceeded)
	assert.Equal(t, int32(1), youtubeDrains.Load())
	assert.Zero(t, fixture.cleanups.Load())
	require.NoError(t, executionCtx.Err())

	var callers sync.WaitGroup

	for range 8 {
		callers.Go(func() {
			closeCtx, closeCancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
			defer closeCancel()

			assert.ErrorIs(t, fixture.runtime.CloseContext(closeCtx), context.DeadlineExceeded)
		})
	}

	callers.Wait()
	assert.Zero(t, fixture.cleanups.Load())
	fixture.unblock()
	require.Error(t, <-fixture.requestDone)

	for range 8 {
		callers.Go(func() {
			closeCtx, closeCancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer closeCancel()

			assert.ErrorIs(t, fixture.runtime.CloseContext(closeCtx), context.DeadlineExceeded)
		})
	}

	callers.Wait()
	assert.Equal(t, int32(1), fixture.cleanups.Load())
	fixture.runtime.Start(t.Context(), nil)
	require.ErrorIs(t, fixture.runtime.CloseContext(t.Context()), context.DeadlineExceeded)
	assert.Equal(t, int32(1), fixture.cleanups.Load())
}

func TestLLMH3TriggerInFlightCompletesBeforeResourceCleanup(t *testing.T) {
	fixture := newLLMHTTPTriggerFixture(t)
	awaitLLMHTTPTrigger(t, fixture)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	result := make(chan error, 1)

	go func() { result <- fixture.runtime.CloseContext(ctx) }()

	select {
	case err := <-result:
		t.Fatalf("close finished while trigger still running: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	assert.Zero(t, fixture.cleanups.Load())
	fixture.unblock()
	require.NoError(t, <-fixture.requestDone)
	require.NoError(t, awaitLLMHTTPClose(t, result))
	assert.Equal(t, int32(1), fixture.cleanups.Load())
}

func TestLLMH3NormalShutdownClosesResourcesOnce(t *testing.T) {
	fixture := newLLMHTTPTriggerFixture(t)
	awaitLLMHTTPTrigger(t, fixture)
	fixture.unblock()
	require.NoError(t, <-fixture.requestDone)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	require.NoError(t, fixture.runtime.CloseContext(ctx))
	require.NoError(t, fixture.runtime.CloseContext(ctx))
	assert.Equal(t, int32(1), fixture.cleanups.Load())
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

			go func() { requestDone <- runLLMHTTPTriggerRequest(server.Client(), request) }()

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
