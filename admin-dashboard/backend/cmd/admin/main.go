// Package main: Admin Backend 서버의 엔트리포인트입니다.
// 공통 관리 기능(인증, Docker, Logs)과 도메인별 봇 프록시를 제공합니다.
//
// @title           Admin Backend API
// @version         1.0.0
// @description     Unified Admin Console Backend for the Antigravity Bot Ecosystem.
// @description     Provides infrastructure management APIs (Auth, Docker, Logs) and domain bot proxies.
//
// @contact.name   API Support
// @contact.email  admin@capu.blog
//
// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT
//
// @host      admin.capu.blog
// @BasePath  /admin/api
// @schemes   https
//
// @securityDefinitions.apikey  SessionCookie
// @in                          cookie
// @name                        admin_session
// @description                 Session-based authentication via HMAC-signed HTTP-only cookie
//
// @tag.name        auth
// @tag.description Authentication endpoints (login, logout, heartbeat)
//
// @tag.name        docker
// @tag.description Docker container lifecycle management
//
// @tag.name        logs
// @tag.description System and container log access
package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/telemetry"
	"github.com/valkey-io/valkey-go"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/auth"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/bootstrap"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/config"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/docker"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/proxy"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/server"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/status"
)

// Version: 빌드 시 ldflags로 주입됨
var Version = "dev"

func main() {
	// .env 파일 로드 (개발 환경용)
	_ = godotenv.Load()

	cfg := config.Load()
	ctx := context.Background()

	// OTel 활성화 여부 확인
	otelEnabled := cfg.OTELEnabled

	// 로거 생성 (OTel 상관관계 포함)
	logger, err := bootstrap.NewLoggerWithConfig(cfg.Log, otelEnabled)
	if err != nil {
		// 파일 로깅 실패 시 stdout 로거 사용
		logger = bootstrap.NewLogger()
		logger.Warn("file_logging_failed", slog.Any("error", err))
	}

	// 필수 설정 검증 (운영 파손 방지: 누락 시 즉시 종료)
	if strings.TrimSpace(cfg.AdminPassHash) == "" {
		logger.Error("config_missing_admin_pass_hash", slog.String("expected_env", "ADMIN_PASS_HASH or ADMIN_PASS_BCRYPT"))
		os.Exit(1)
	}
	if strings.TrimSpace(cfg.AdminSecretKey) == "" {
		logger.Error("config_missing_session_secret", slog.String("expected_env", "SESSION_SECRET or ADMIN_SECRET_KEY"))
		os.Exit(1)
	}
	// OpenTelemetry 초기화 (선택적)
	var otelProvider *telemetry.Provider
	if otelEnabled && cfg.OTELEndpoint != "" {
		sampleRate := 1.0
		if s := os.Getenv("OTEL_SAMPLE_RATE"); s != "" {
			if parsed, parseErr := strconv.ParseFloat(s, 64); parseErr == nil {
				sampleRate = parsed
			}
		}

		otelCfg := telemetry.Config{
			Enabled:        true,
			ServiceName:    cfg.OTELServiceName,
			ServiceVersion: Version,
			Environment:    cfg.Environment,
			OTLPEndpoint:   cfg.OTELEndpoint,
			OTLPInsecure:   cfg.OTLPInsecure,
			SampleRate:     sampleRate,
		}

		otelProvider, err = telemetry.NewProvider(ctx, otelCfg)
		if err != nil {
			logger.Warn("otel_init_failed", slog.Any("error", err))
		} else if otelProvider.IsEnabled() {
			logger.Info("otel_initialized",
				slog.String("endpoint", cfg.OTELEndpoint),
				slog.String("service", cfg.OTELServiceName),
				slog.Float64("sample_rate", sampleRate),
			)
		}
	}

	logger.Info("admin_backend_starting",
		slog.String("version", Version),
		slog.String("port", cfg.Port),
		slog.String("env", cfg.Environment),
		slog.Bool("otel_enabled", otelEnabled),
	)

	// 애플리케이션 초기화 및 실행
	serverApp, cleanup, err := initializeApp(ctx, cfg, logger)
	if err != nil {
		logger.Error("app_init_failed", slog.Any("error", err))
		os.Exit(1)
	}

	// Deferred cleanup (LIFO 순서)
	defer func() {
		if cleanup != nil {
			cleanup()
		}
		// OTel Provider 정리 (마지막에 실행)
		if otelProvider != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := otelProvider.Shutdown(shutdownCtx); err != nil {
				logger.Warn("otel_shutdown_failed", slog.Any("error", err))
			} else {
				logger.Info("otel_shutdown_complete")
			}
		}
	}()

	if err := serverApp.Run(ctx); err != nil {
		logger.Error("app_run_failed", slog.Any("error", err))
		os.Exit(1)
	}
}

// initializeApp: 애플리케이션 구성 요소를 초기화합니다.
func initializeApp(_ context.Context, cfg *config.Config, logger *slog.Logger) (*bootstrap.ServerApp, func(), error) {
	cleanupFns := make([]func(), 0, 4)
	cleanup := func() {
		for i := len(cleanupFns) - 1; i >= 0; i-- {
			cleanupFns[i]()
		}
	}

	// Valkey 클라이언트 초기화
	valkeyClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{cfg.ValkeyURL},
	})
	if err != nil {
		logger.Error("valkey_connect_failed", slog.Any("error", err))
		return nil, cleanup, err
	}
	cleanupFns = append(cleanupFns, func() {
		valkeyClient.Close()
		logger.Info("valkey_closed")
	})
	logger.Info("valkey_connected", slog.String("addr", cfg.ValkeyURL))

	// 세션 저장소 초기화
	sessions := auth.NewValkeySessionStore(valkeyClient, logger)

	// Docker 서비스 초기화 (선택적)
	var dockerSvc *docker.Service
	dockerSvc, err = docker.NewService(logger, "llm-bot")
	if err != nil {
		// Docker 서비스를 사용할 수 없어도 서버는 계속 동작함
		logger.Warn("docker_init_failed", slog.Any("error", err))
	} else {
		logger.Info("docker_initialized")
	}

	// 봇 프록시 초기화 (선택적)
	var botProxies *proxy.BotProxies
	if cfg.HoloBotURL != "" {
		botProxies, err = proxy.NewBotProxies(cfg.HoloBotURL, cfg.HoloBotAPIKey, logger)
		if err != nil {
			logger.Warn("bot_proxy_init_failed", slog.Any("error", err))
		} else {
			holoAPIKeyStatus := "not_set"
			if cfg.HoloBotAPIKey != "" {
				holoAPIKeyStatus = "configured"
			}
			logger.Info("bot_proxy_initialized",
				slog.String("holo", cfg.HoloBotURL),
				slog.String("holo_api_key", holoAPIKeyStatus),
			)
		}
	}

	// 통합 시스템 상태 수집기 초기화
	statusEndpoints := []status.ServiceEndpoint{
		{Name: "hololive-bot", HealthURL: cfg.HoloBotURL + "/health", StatsURL: cfg.HoloBotURL + "/api/holo/stats"},
	}
	statusCollector := status.NewCollector(statusEndpoints, Version, logger)
	logger.Info("status_collector_initialized", slog.Int("endpoints", len(statusEndpoints)))

	// HTTP 서버 생성
	httpServer := server.New(cfg, logger, sessions, dockerSvc, botProxies, statusCollector)

	// ServerApp 생성
	serverApp := bootstrap.NewServerApp(
		"admin-dashboard",
		logger,
		httpServer.HTTPServer(),
		30*time.Second,
	).WithTLS(cfg.TLSEnabled, cfg.TLSCertPath, cfg.TLSKeyPath)

	return serverApp, cleanup, nil
}
