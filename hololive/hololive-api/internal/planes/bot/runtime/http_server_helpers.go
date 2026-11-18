package botruntime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/park285/shared-go/v2/pkg/runtime/httpserver"
	"github.com/quic-go/quic-go/http3"
)

func startHTTP3Server(server *http3.Server, logger *slog.Logger, errCh chan<- error) {
	if server == nil {
		return
	}

	httpserver.StartServerWithPrefix(server, "HTTP/3 server error", logger, errCh)
}

func shutdownHTTP3Server(ctx context.Context, server *http3.Server) error {
	if server == nil {
		return nil
	}

	if err := httpserver.Shutdown(ctx, server, "HTTP/3 server shutdown failed"); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}

func startShortLinkServer(server *http.Server, logger *slog.Logger, errCh chan<- error) {
	if server == nil {
		return
	}

	httpserver.StartServerWithPrefix(server, "short-link server error", logger, errCh)
}

func shutdownShortLinkServer(ctx context.Context, server *http.Server) error {
	if server == nil {
		return nil
	}

	if err := httpserver.Shutdown(ctx, server, "short-link server shutdown failed"); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}

func startMetricsServer(server *http.Server, logger *slog.Logger, errCh chan<- error) {
	if server == nil {
		return
	}

	httpserver.StartServerWithPrefix(server, "metrics server error", logger, errCh)
}

func shutdownMetricsServer(ctx context.Context, server *http.Server) error {
	if server == nil {
		return nil
	}

	if err := httpserver.Shutdown(ctx, server, "metrics server shutdown failed"); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}

func startPprofServer(server *http.Server, logger *slog.Logger, errCh chan<- error) {
	if server == nil {
		return
	}

	httpserver.StartServerWithPrefix(server, "pprof server error", logger, errCh)
}

func shutdownPprofServer(ctx context.Context, server *http.Server) error {
	if server == nil {
		return nil
	}

	if err := httpserver.Shutdown(ctx, server, "pprof server shutdown failed"); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
