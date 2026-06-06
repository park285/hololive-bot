package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedh3 "github.com/park285/shared-go/pkg/h3"
	runtimehttpserver "github.com/park285/shared-go/pkg/runtime/httpserver"
	"github.com/quic-go/quic-go/http3"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type RuntimeHTTPServers struct {
	H2C *http.Server
	H3  *http3.Server
}

func NewRuntimeHTTPServers(serverConfig config.ServerConfig, handler http.Handler, operation string) (*RuntimeHTTPServers, error) {
	servers := &RuntimeHTTPServers{}
	if serverConfig.TransportEnabled("h2c") {
		servers.H2C = NewH2CServer(runtimeH2CAddr(serverConfig), handler, operation)
	}
	if serverConfig.TransportEnabled("h3") {
		h3Server, err := NewH3Server(runtimeH3Addr(serverConfig), handler, serverConfig.H3CertFile, serverConfig.H3KeyFile, operation)
		if err != nil {
			return nil, err
		}
		servers.H3 = h3Server
	}
	return servers, nil
}

func NewH3Server(addr string, handler http.Handler, certFile, keyFile, operation string) (*http3.Server, error) {
	if handler == nil {
		handler = http.NotFoundHandler()
	}
	if strings.TrimSpace(operation) != "" {
		handler = otelhttp.NewHandler(handler, operation)
	}

	return sharedh3.NewServer(addr, handler, certFile, keyFile)
}

func (s *RuntimeHTTPServers) Addr() string {
	if s == nil {
		return ""
	}
	if s.H3 != nil {
		return s.H3.Addr
	}
	if s.H2C != nil {
		return s.H2C.Addr
	}
	return ""
}

func (s *RuntimeHTTPServers) Start(logger *slog.Logger, errCh chan<- error) {
	if s == nil {
		return
	}
	runtimehttpserver.StartHTTPServer(s.H2C, logger, errCh)
	StartH3Server(s.H3, logger, errCh)
}

func (s *RuntimeHTTPServers) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return errors.Join(
		runtimehttpserver.ShutdownHTTPServer(ctx, s.H2C),
		ShutdownH3Server(ctx, s.H3),
	)
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

func runtimeH2CAddr(serverConfig config.ServerConfig) string {
	if strings.TrimSpace(serverConfig.H2CAddr) != "" {
		return serverConfig.H2CAddr
	}
	return fmt.Sprintf(":%d", serverConfig.Port)
}

func runtimeH3Addr(serverConfig config.ServerConfig) string {
	if strings.TrimSpace(serverConfig.H3Addr) != "" {
		return serverConfig.H3Addr
	}
	return fmt.Sprintf(":%d", serverConfig.Port)
}
