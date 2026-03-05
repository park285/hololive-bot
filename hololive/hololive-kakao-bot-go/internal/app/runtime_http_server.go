package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// StartHTTPServer: Bot HTTP 서버를 비동기적으로 시작합니다.
func (r *BotRuntime) StartHTTPServer(errCh chan<- error) {
	if r == nil || r.HttpServer == nil {
		return
	}

	go func() {
		if err := r.HttpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if errCh != nil {
				errCh <- fmt.Errorf("HTTP server error: %w", err)
				return
			}
			if r.Logger != nil {
				r.Logger.Error("HTTP server error", slog.Any("error", err))
			}
		}
	}()
}

// ShutdownHTTPServer: Bot HTTP 서버를 안전하게 종료합니다.
func (r *BotRuntime) ShutdownHTTPServer(ctx context.Context) error {
	if r == nil || r.HttpServer == nil {
		return nil
	}
	if err := r.HttpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP server shutdown failed: %w", err)
	}
	return nil
}
