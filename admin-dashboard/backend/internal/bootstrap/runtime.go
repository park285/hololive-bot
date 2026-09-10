// Package bootstrap는 관리자 설정으로 외부 자원을 조립하고 시작·종료 순서를 소유합니다.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/runtime/httpserver"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/admin-dashboard/internal/adapters/docker"
	"github.com/kapu/admin-dashboard/internal/adapters/holo"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpapi"
	"github.com/kapu/admin-dashboard/internal/observations"
	"github.com/kapu/admin-dashboard/internal/session"
)

// Runtime은 한 관리자 프로세스의 HTTP 경계·store·관측 수명을 소유합니다.
type Runtime struct {
	cfg       config.Config
	logger    *slog.Logger
	api       *httpapi.API
	store     *session.Store
	limiter   *session.LoginLimiter
	holo      *holo.Client
	sampler   *observations.Sampler
	hub       *observations.Hub
	docker    *docker.Client
	closeOnce sync.Once
}

// New는 외부 자원을 연결하고 관측을 시작합니다. 실패하면 이미 연 자원을 닫습니다.
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (_ *Runtime, err error) {
	r := &Runtime{cfg: *cfg, logger: logger}

	defer func() {
		if err != nil {
			r.Close()
		}
	}()

	r.store, err = session.NewStore(ctx, cfg.ValkeyURL, &cfg.Session)
	if err != nil {
		return nil, fmt.Errorf("create session store: %w", err)
	}

	r.limiter, err = session.NewLoginLimiter(ctx, cfg.ValkeyURL)
	if err != nil {
		return nil, fmt.Errorf("create login limiter: %w", err)
	}

	dockerClient, dockerErr := docker.NewClient(cfg.DockerHost)
	if dockerErr != nil {
		logger.Warn("docker service disabled", slog.Any("error", dockerErr))
	}

	r.docker = dockerClient

	r.holo, err = holo.NewClient(cfg.HoloAdminAPIURL, cfg.HoloBotAPIKey)
	if err != nil {
		return nil, fmt.Errorf("create holo client: %w", err)
	}

	openapi, err := contract.MarshalSpec(cfg.RuntimeVersion)
	if err != nil {
		return nil, fmt.Errorf("marshal contract: %w", err)
	}

	r.sampler = observations.NewSampler([]observations.ServiceEndpoint{{Name: "hololive-admin-api", URL: cfg.HoloAdminAPIURL, HealthPath: "/health"}})
	r.hub = observations.NewHubWithSampler(r.sampler)

	r.api, err = httpapi.New(cfg, logger, httpapi.Dependencies{
		Sessions: r.store, LoginLimiter: r.limiter, Docker: dockerClient, Holo: r.holo,
		Status: observations.NewCollectorWithSampler(r.sampler, cfg.RuntimeVersion), Stats: r.hub, OpenAPI: openapi,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble HTTP API: %w", err)
	}

	startStats(ctx, r.hub)

	return r, nil
}

func startStats(ctx context.Context, hub *observations.Hub) {
	// 기동용 ctx는 반환 후 취소되므로 관측 종료는 Runtime.Close가 소유합니다.
	hub.StartContext(context.WithoutCancel(ctx))
}

// Run은 HTTP 서버를 실행하고 25초 종료 예산 안에서 진입 차단·연결 정리를 수행합니다.
func (r *Runtime) Run() error {
	server := &http.Server{
		Addr: r.cfg.ListenAddr(), Handler: r.api.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second,
		IdleTimeout: 120 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	if err := lifecycle.Run(context.Background(), lifecycle.Options{
		ShutdownTimeout: 25 * time.Second,
		Start: func(_ context.Context, errCh chan<- error) {
			r.logger.Info("admin-dashboard listening", slog.String("addr", server.Addr), slog.String("env", r.cfg.Env))
			httpserver.Start(server, r.logger, errCh)
		},
		Shutdown: func(ctx context.Context) error {
			return r.shutdown(ctx, server, 20*time.Second)
		},
	}); err != nil {
		return fmt.Errorf("run admin-dashboard lifecycle: %w", err)
	}

	return nil
}

func (r *Runtime) shutdown(ctx context.Context, server *http.Server, grace time.Duration) error {
	graceCtx, cancel := context.WithTimeout(ctx, grace)
	defer cancel()

	r.logger.Info("admin admission closed", slog.Any("in_flight", r.api.BeginDrain()))
	r.api.CloseStreams()
	r.hub.Stop()

	// Shutdown만으로는 제한 시간을 넘긴 요청이나 hijack된 WS가 종료되지 않습니다.
	shutdownErr := server.Shutdown(graceCtx)
	if shutdownErr != nil {
		r.logger.Warn("admin HTTP drain expired", slog.Any("in_flight", r.api.BeginDrain()))

		shutdownErr = errors.Join(shutdownErr, server.Close())
	}

	drainErr := r.api.WaitDrained(ctx)
	// 외부 client/store 정리도 lifecycle 종료 예산 안에서 수행하고 초과를 성공으로 보고하지 않습니다.
	r.Close()

	if err := errors.Join(shutdownErr, drainErr, ctx.Err()); err != nil {
		return fmt.Errorf("shutdown administrator HTTP: %w", err)
	}

	return nil
}

// Close는 관측을 먼저 멈춘 뒤 공유 sampler·upstream client·인증 store를 닫습니다.
func (r *Runtime) Close() {
	r.closeOnce.Do(r.closeResources)
}

func (r *Runtime) closeResources() {
	if r.api != nil {
		r.api.BeginDrain()
		r.api.Close()
	}

	if r.hub != nil {
		r.hub.Stop()
	}

	if r.sampler != nil {
		if err := r.sampler.Close(); err != nil {
			r.logger.Warn("close endpoint sampler", slog.Any("error", err))
		}
	}

	if r.docker != nil {
		r.docker.Close()
	}

	if r.holo != nil {
		if err := r.holo.Close(); err != nil {
			r.logger.Warn("close holo client", slog.Any("error", err))
		}
	}

	if r.limiter != nil {
		r.limiter.Close()
	}

	if r.store != nil {
		r.store.Close()
	}
}
