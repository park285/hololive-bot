package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/pkg/json"
	"github.com/park285/shared-go/pkg/runtime/httpserver"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/docker"
	"github.com/kapu/admin-dashboard/internal/holo"
	"github.com/kapu/admin-dashboard/internal/openapi"
	"github.com/kapu/admin-dashboard/internal/session"
	"github.com/kapu/admin-dashboard/internal/static"
	"github.com/kapu/admin-dashboard/internal/status"
)

const maxSystemStatsStreams = 16

const sessionIDKey = "admin-session-id"

type sessionStore interface {
	Create(ctx context.Context) (session.Session, error)
	Get(ctx context.Context, id string) (*session.Session, error)
	Delete(ctx context.Context, id string) error
	Refresh(ctx context.Context, id string, idle bool) (session.RefreshResult, error)
	Rotate(ctx context.Context, oldID string) (*session.Session, error)
	Close()
}

type Runtime struct {
	cfg             config.Config
	logger          *slog.Logger
	sessions        sessionStore
	rateLimiter     *auth.LoginRateLimiter
	docker          *docker.Client
	holo            *holo.Client
	statusCollector *status.Collector
	statsHub        *status.Hub
	static          static.Handler
	wsStreams       chan struct{}
	openapiJSON     []byte
}

func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Runtime, error) {
	store, err := session.NewStore(ctx, cfg.ValkeyURL, &cfg.Session)
	if err != nil {
		return nil, err
	}
	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		logger.Warn("docker service disabled", slog.Any("error", err))
		dockerClient = nil
	}
	holoClient, err := holo.NewClient(cfg.HoloAdminAPIURL, cfg.HoloBotAPIKey)
	if err != nil {
		store.Close()
		return nil, err
	}
	endpoints := []status.ServiceEndpoint{{Name: "hololive-admin-api", URL: cfg.HoloAdminAPIURL, HealthPath: "/health"}}
	openapiJSON, err := json.Marshal(openapi.Spec(cfg.RuntimeVersion))
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("marshal openapi spec: %w", err)
	}
	rateLimiter := auth.NewLoginRateLimiter()
	rateLimiter.Start()
	statsHub := status.NewHub(endpoints)
	statsHub.StartContext(ctx)
	return &Runtime{
		cfg:             *cfg,
		logger:          logger,
		sessions:        store,
		rateLimiter:     rateLimiter,
		docker:          dockerClient,
		holo:            holoClient,
		statusCollector: status.NewCollector(endpoints, cfg.RuntimeVersion),
		statsHub:        statsHub,
		static:          static.NewHandler(),
		wsStreams:       make(chan struct{}, maxSystemStatsStreams),
		openapiJSON:     openapiJSON,
	}, nil
}

func (r *Runtime) Run() {
	server := &http.Server{
		Addr:              r.cfg.ListenAddr(),
		Handler:           r.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	err := lifecycle.Run(lifecycle.Options{
		ShutdownTimeout: 20 * time.Second,
		Start: func(_ context.Context, errCh chan<- error) {
			r.logger.Info("admin-dashboard listening", slog.String("addr", server.Addr), slog.String("env", r.cfg.Env))
			httpserver.Start(server, r.logger, errCh)
		},
		Shutdown: func(ctx context.Context) error {
			return httpserver.Shutdown(ctx, server, "shutdown admin-dashboard http server")
		},
	})
	if err != nil {
		r.logger.Error("admin-dashboard terminated", slog.Any("error", err))
	}
}

func (r *Runtime) Close() {
	if r.rateLimiter != nil {
		r.rateLimiter.Stop()
	}
	if r.statsHub != nil {
		r.statsHub.Stop()
	}
	if r.sessions != nil {
		r.sessions.Close()
	}
}

func sessionIDFrom(c *gin.Context) (string, bool) {
	value := c.GetString(sessionIDKey)
	return value, value != ""
}
