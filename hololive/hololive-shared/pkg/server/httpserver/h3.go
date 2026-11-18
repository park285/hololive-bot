package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	sharedh3 "github.com/park285/shared-go/transport/h3"
	runtimehttpserver "github.com/park285/shared-go/v2/pkg/runtime/httpserver"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/quic-go/quic-go/http3"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type RuntimeHTTPServers struct {
	H3      *http3.Server
	Metrics *http.Server
	Pprof   *http.Server

	h3Requests *h3RequestDrain
}

func NewRuntimeHTTPServers(ctx context.Context, serverConfig *settings.ServerConfig, handler http.Handler,
	workerRegistry *workercontract.Registry,
) (*RuntimeHTTPServers, error) {
	if serverConfig == nil {
		return nil, errors.New("server config is nil")
	}

	servers := &RuntimeHTTPServers{}

	if serverConfig.TransportEnabled("h3") {
		drain := &h3RequestDrain{}

		h3Server, err := newH3Server(runtimeH3Addr(serverConfig), drain.wrap(handler), serverConfig.H3CertFile, serverConfig.H3KeyFile)
		if err != nil {
			return nil, fmt.Errorf("H3 server: %w", err)
		}

		servers.H3 = h3Server
		servers.h3Requests = drain
	}

	if metricsAddr := strings.TrimSpace(serverConfig.MetricsAddr); metricsAddr != "" {
		servers.Metrics = NewMetricsServer(ctx, metricsAddr, serverConfig.APIKey, workerRegistry)
	}

	if pprofAddr := strings.TrimSpace(serverConfig.PprofAddr); pprofAddr != "" {
		servers.Pprof = NewPprofServer(ctx, pprofAddr, serverConfig.APIKey)
	}

	return servers, nil
}

func WithRuntimeBodyReadTimeout(handler http.Handler) http.Handler {
	return runtimehttpserver.WithBodyReadTimeout(handler, serverReadTimeout)
}

func newH3Server(addr string, handler http.Handler, certFile, keyFile string) (*http3.Server, error) {
	if handler == nil {
		handler = http.NotFoundHandler()
	}

	handler = WithRuntimeBodyReadTimeout(handler)

	out, err := sharedh3.NewServer(addr, handler, certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("server: %w", err)
	}

	return out, nil
}

func (s *RuntimeHTTPServers) Addr() string {
	if s == nil || s.H3 == nil {
		return ""
	}

	return s.H3.Addr
}

func (s *RuntimeHTTPServers) Start(logger *slog.Logger, errCh chan<- error) {
	if s == nil {
		return
	}

	var firstError sync.Once

	report := func(err error) {
		if errCh != nil {
			firstError.Do(func() { errCh <- err })
		}
	}

	if s.H3 != nil {
		startRuntimeServer(s.H3, "HTTP/3 server error", logger, report)
	}

	if s.Metrics != nil {
		startRuntimeServer(s.Metrics, "metrics server error", logger, report)
	}

	if s.Pprof != nil {
		startRuntimeServer(s.Pprof, "pprof server error", logger, report)
	}
}

func (s *RuntimeHTTPServers) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}

	var metricsErr, pprofErr error

	var wg sync.WaitGroup

	if s.Metrics != nil {
		wg.Go(func() { metricsErr = runtimehttpserver.Shutdown(ctx, s.Metrics, "metrics server shutdown failed") })
	}

	if s.Pprof != nil {
		wg.Go(func() { pprofErr = runtimehttpserver.Shutdown(ctx, s.Pprof, "pprof server shutdown failed") })
	}

	h3Err := shutdownH3Server(ctx, s.H3, s.h3Requests)

	wg.Wait()

	return errors.Join(h3Err, metricsErr, pprofErr)
}

type runtimeErrorServer struct {
	runtimehttpserver.Server

	errorText string
	report    func(error)
}

func (s runtimeErrorServer) ListenAndServe() error {
	err := s.Server.ListenAndServe()
	if err == nil {
		return nil
	}

	if !errors.Is(err, http.ErrServerClosed) {
		s.report(fmt.Errorf("httpserver: listen: %s: %w", s.errorText, err))
	}

	return fmt.Errorf("serve runtime listener: %w", err)
}

func startRuntimeServer(server runtimehttpserver.Server, errorText string, logger *slog.Logger, report func(error)) {
	runtimehttpserver.StartServerWithPrefix(runtimeErrorServer{Server: server, errorText: errorText, report: report}, errorText, logger, nil)
}

func runtimeH3Addr(serverConfig *settings.ServerConfig) string {
	if serverConfig == nil {
		return ""
	}

	if strings.TrimSpace(serverConfig.H3Addr) != "" {
		return serverConfig.H3Addr
	}

	return fmt.Sprintf(":%d", serverConfig.Port)
}
