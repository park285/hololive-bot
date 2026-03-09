// Package bootstrap: 애플리케이션 초기화 및 실행을 위한 공통 유틸리티입니다.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

// ServerApp: HTTP 서버를 포함하는 애플리케이션 실행 단위입니다.
type ServerApp struct {
	Name            string
	Logger          *slog.Logger
	Server          *http.Server
	ShutdownTimeout time.Duration
	// TLS 설정 (HTTP/2 지원)
	TLSEnabled  bool
	TLSCertPath string
	TLSKeyPath  string
}

// NewServerApp: 새로운 ServerApp 인스턴스를 생성합니다.
func NewServerApp(
	name string,
	logger *slog.Logger,
	server *http.Server,
	shutdownTimeout time.Duration,
) *ServerApp {
	return &ServerApp{
		Name:            name,
		Logger:          logger,
		Server:          server,
		ShutdownTimeout: shutdownTimeout,
	}
}

// WithTLS: TLS 설정을 추가합니다.
func (a *ServerApp) WithTLS(enabled bool, certPath, keyPath string) *ServerApp {
	a.TLSEnabled = enabled
	a.TLSCertPath = certPath
	a.TLSKeyPath = keyPath
	return a
}

// Run: 애플리케이션을 실행합니다.
// OS 시그널(SIGINT, SIGTERM)을 감지하여 우아하게 종료합니다.
func (a *ServerApp) Run(ctx context.Context) error {
	if a == nil {
		return nil
	}

	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	g, gctx := errgroup.WithContext(signalCtx)

	protocol := "http"
	if a.TLSEnabled {
		protocol = "https (HTTP/2)"
	}
	a.Logger.Info("server_start",
		slog.String("name", a.Name),
		slog.String("addr", a.Server.Addr),
		slog.String("protocol", protocol),
	)

	g.Go(func() error {
		var err error
		if a.TLSEnabled {
			// TLS 활성화: HTTP/2 자동 지원
			err = a.Server.ListenAndServeTLS(a.TLSCertPath, a.TLSKeyPath)
		} else {
			err = a.Server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http server failed: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-gctx.Done()
		a.Logger.Info("shutdown_signal_received", slog.String("name", a.Name))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.ShutdownTimeout)
		defer cancel()

		if err := a.Server.Shutdown(shutdownCtx); err != nil {
			a.Logger.Error("server_shutdown_failed", slog.Any("error", err))
			return fmt.Errorf("server shutdown failed: %w", err)
		}

		a.Logger.Info("server_stopped", slog.String("name", a.Name))
		return nil
	})

	if err := g.Wait(); err != nil {
		return fmt.Errorf("wait for goroutines: %w", err)
	}
	return nil
}
