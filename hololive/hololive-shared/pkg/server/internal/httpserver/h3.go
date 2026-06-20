package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	sharedh3 "github.com/park285/shared-go/pkg/h3"
	runtimehttpserver "github.com/park285/shared-go/pkg/runtime/httpserver"
	"github.com/quic-go/quic-go/http3"

	"github.com/kapu/hololive-shared/pkg/config"
)

type RuntimeHTTPServers struct {
	H3      *http3.Server
	Metrics *http.Server
	Pprof   *http.Server
}

func NewRuntimeHTTPServers(serverConfig *config.ServerConfig, handler http.Handler, operation string) (*RuntimeHTTPServers, error) {
	if serverConfig == nil {
		return nil, fmt.Errorf("server config is nil")
	}
	servers := &RuntimeHTTPServers{}
	if serverConfig.TransportEnabled("h3") {
		h3Server, err := NewH3Server(runtimeH3Addr(serverConfig), handler, serverConfig.H3CertFile, serverConfig.H3KeyFile, operation)
		if err != nil {
			return nil, err
		}
		servers.H3 = h3Server
	}
	if metricsAddr := strings.TrimSpace(serverConfig.MetricsAddr); metricsAddr != "" {
		servers.Metrics = NewMetricsServer(metricsAddr, serverConfig.APIKey)
	}
	if pprofAddr := strings.TrimSpace(serverConfig.PprofAddr); pprofAddr != "" {
		servers.Pprof = NewPprofServer(pprofAddr, serverConfig.APIKey)
	}
	return servers, nil
}

func NewH3Server(addr string, handler http.Handler, certFile, keyFile, operation string) (*http3.Server, error) {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	handler = newOtelHandler(handler, operation)

	return sharedh3.NewServer(addr, handler, certFile, keyFile)
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
	StartH3Server(s.H3, logger, errCh)
	if s.Metrics != nil {
		runtimehttpserver.StartServerWithPrefix(s.Metrics, "metrics server error", logger, errCh)
	}
	if s.Pprof != nil {
		runtimehttpserver.StartServerWithPrefix(s.Pprof, "pprof server error", logger, errCh)
	}
}

func (s *RuntimeHTTPServers) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	err := ShutdownH3Server(ctx, s.H3)
	if s.Metrics != nil {
		err = errors.Join(err, runtimehttpserver.Shutdown(ctx, s.Metrics, "metrics server shutdown failed"))
	}
	if s.Pprof != nil {
		err = errors.Join(err, runtimehttpserver.Shutdown(ctx, s.Pprof, "pprof server shutdown failed"))
	}
	return err
}

func StartH3Server(server *http3.Server, logger *slog.Logger, errCh chan<- error) {
	if server == nil {
		return
	}
	runtimehttpserver.StartServerWithPrefix(server, "HTTP/3 server error", logger, errCh)
}

func ShutdownH3Server(ctx context.Context, server *http3.Server) error {
	if server == nil {
		return nil
	}
	return runtimehttpserver.Shutdown(ctx, server, "HTTP/3 server shutdown failed")
}

func runtimeH3Addr(serverConfig *config.ServerConfig) string {
	if serverConfig == nil {
		return ""
	}
	if strings.TrimSpace(serverConfig.H3Addr) != "" {
		return serverConfig.H3Addr
	}
	return fmt.Sprintf(":%d", serverConfig.Port)
}
