package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/iris"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/kapu/hololive-kakao-bot-go/internal/server"
)

// ProvideAPIAddr: 관리자 서버가 리슨할 주소를 반환합니다.
func ProvideAPIAddr(cfg *config.Config) string {
	return fmt.Sprintf(":%d", cfg.Server.Port)
}

// ProvideAPIServer: 관리자용 HTTP 서버 인스턴스를 생성합니다.
// H2C(HTTP/2 Cleartext)를 기본으로 사용하여 멀티플렉싱과 헤더 압축 이점을 제공한다.
func ProvideAPIServer(addr string, router *gin.Engine) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           sharedserver.WrapH2C(router),
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}
}

// ProvideAPIRouter: hololive-bot 도메인 API를 서빙하는 Gin 라우터를 설정합니다.
// Admin Dashboard와 Tauri 앱에서 사용됩니다.
func ProvideAPIRouter(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	domainHandlers *server.DomainAPIHandlers,
	authHandler *server.AuthHandler,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
) (*gin.Engine, error) {
	router, err := newAPIRouter(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	if domainHandlers == nil {
		return nil, fmt.Errorf("domain handlers must not be nil")
	}

	if authHandler == nil {
		return nil, fmt.Errorf("auth handler must not be nil")
	}

	if webhookHandler != nil {
		router.POST("/webhook/iris", webhookHandler.Handle)
	}

	// 내부 트리거 라우트 등록 (운영 API에서 스케줄러 수동 실행용)
	if triggerHandler != nil {
		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), cfg.Server.APIKey)
	}

	registerAPIRoutes(router, cfg.Server.APIKey, domainHandlers, authHandler)

	if cfg.Server.APIKey != "" {
		logger.Info("api_key_auth_enabled")
	} else {
		return nil, fmt.Errorf("API_SECRET_KEY required")
	}

	return router, nil
}

func newAPIRouter(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
	}
	router.TrustedPlatform = gin.PlatformCloudflare

	// OTel 미들웨어: 활성화된 경우 모든 HTTP 요청을 추적함 (가장 앞에 배치)
	if cfg.Telemetry.Enabled {
		serviceName := cfg.Telemetry.ServiceName
		if serviceName == "" {
			serviceName = "hololive-bot"
		}
		router.Use(otelgin.Middleware(serviceName))
		logger.Info("otel_http_middleware_enabled", slog.String("service", serviceName))
	}

	router.Use(gin.Recovery())
	router.Use(sharedserver.LoggerMiddleware(ctx, logger,
		"/health",
		"/metrics", // Prometheus 메트릭 폴링 (15초 간격)
	))
	isProduction := strings.EqualFold(strings.TrimSpace(cfg.Telemetry.Environment), "production")
	if isProduction && cfg.CORS.MissingInProduction {
		logger.Warn(
			"cors_allowed_origins_missing_in_production_monitor_mode",
			slog.Bool("cors_enforce", cfg.CORS.Enforce),
			slog.String("next_step", "set CORS_ALLOWED_ORIGINS and enable CORS_ENFORCE"),
		)
	}
	router.Use(corsOriginGuard(cfg.CORS.AllowedOrigins))
	router.Use(cors.New(newAPICORSConfig(cfg)))
	router.Use(sharedserver.SecurityHeadersMiddleware())
	router.Use(sharedserver.ClientHintsMiddleware()) // Client Hints 요청 (실제 기기 정보 수집)

	registerAPIHealthRoutes(router)

	// NoRoute 핸들러: 미등록 경로 접근 시 API Key 검증 후 401/404 반환
	// 크롤러/스캐너가 루트 경로 등에 접근할 때 404 대신 401 Unauthorized 반환
	router.NoRoute(sharedserver.NoRouteAuthHandler(cfg.Server.APIKey))

	return router, nil
}

func newAPICORSConfig(cfg *config.Config) cors.Config {
	corsConfig := cors.DefaultConfig()
	if len(cfg.CORS.AllowedOrigins) == 0 {
		corsConfig.AllowOriginFunc = func(string) bool { return false }
	} else {
		corsConfig.AllowOrigins = cfg.CORS.AllowedOrigins
	}
	corsConfig.AllowCredentials = true
	corsConfig.AllowMethods = constants.CORSConfig.AllowMethods
	corsConfig.AllowHeaders = constants.CORSConfig.AllowHeaders
	return corsConfig
}

func corsOriginGuard(allowedOrigins []string) gin.HandlerFunc {
	allowAll := false
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			allowAll = true
			continue
		}
		allowed[trimmed] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" || allowAll {
			c.Next()
			return
		}
		if _, ok := allowed[origin]; !ok {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

func registerAPIHealthRoutes(router *gin.Engine) {
	// Health check 엔드포인트 (버전/uptime 포함)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, health.Get())
	})

	// Prometheus 메트릭 (장기 히스토리 분석용)
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))
}

func registerAPIRoutes(
	router *gin.Engine,
	apiKey string,
	domainHandlers *server.DomainAPIHandlers,
	authHandler *server.AuthHandler,
) {
	domains := domainHandlers

	// OAuth 콜백 프록시 (인증 불필요 - Google에서 직접 호출)
	// 모바일 앱에서 localhost 리디렉션이 불가능하므로 서버가 프록시 역할
	router.GET("/oauth/callback", domains.OAuth.OAuthCallbackHandler)

	// Session 기반 인증 API
	authAPI := router.Group("/api/auth")
	authAPI.POST("/register", authHandler.Register)
	authAPI.POST("/login", authHandler.Login)
	authAPI.POST("/logout", authHandler.Logout)
	authAPI.POST("/refresh", authHandler.Refresh)
	authAPI.GET("/me", authHandler.Me)
	authAPI.POST("/password/reset-request", authHandler.ResetRequest)
	authAPI.POST("/password/reset", authHandler.ResetPassword)

	// hololive-bot 도메인 API (Admin Dashboard, Tauri 앱에서 사용)
	holoAPI := router.Group("/api/holo")

	// API Key 인증 미들웨어 적용 (apiKey가 빈 문자열이면 인증 건너뜀)
	holoAPI.Use(sharedserver.APIKeyAuthMiddleware(apiKey))

	registerMemberRoutes(holoAPI, domains.Member)
	registerAlarmRoutes(holoAPI, domains.Alarm)
	registerRoomRoutes(holoAPI, domains.Room)
	registerStatsRoutes(holoAPI, domains.Stats, domains.Stream)
	registerSettingsRoutes(holoAPI, domains.Settings)
	registerTemplateRoutes(holoAPI, domains.Template)
	registerMilestoneRoutes(holoAPI, domains.Milestone)
	registerProfileRoutes(holoAPI, domains.Profile)
	registerMajorEventRoutes(holoAPI, domains.MajorEvent)
}

// ProvideHealthOnlyRouter: health + metrics 엔드포인트만 제공하는 최소 라우터.
func ProvideHealthOnlyRouter(ctx context.Context, logger *slog.Logger) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
	}
	router.TrustedPlatform = gin.PlatformCloudflare

	router.Use(gin.Recovery())
	router.Use(sharedserver.LoggerMiddleware(ctx, logger,
		"/health",
		"/metrics",
	))

	registerAPIHealthRoutes(router)

	return router, nil
}

// ProvideTriggerRouter: health + metrics + 내부 트리거 엔드포인트를 제공하는 라우터.
func ProvideTriggerRouter(ctx context.Context, logger *slog.Logger, triggerHandler *sharedserver.TriggerHandler, apiKey string) (*gin.Engine, error) {
	router, err := ProvideHealthOnlyRouter(ctx, logger)
	if err != nil {
		return nil, err
	}

	if triggerHandler != nil {
		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)
	}

	return router, nil
}

// ProvideBotRouter: Bot 전용 라우터를 구성합니다. (webhook + internal trigger + health만)
// Admin API 라우트(members, alarms, rooms, stats, settings 등)는 이 라우터에서 제외합니다.
func ProvideBotRouter(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	webhookHandler *iris.WebhookHandler,
	triggerHandler *sharedserver.TriggerHandler,
) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	if err := router.SetTrustedProxies(constants.ServerConfig.TrustedProxies); err != nil {
		return nil, fmt.Errorf("failed to set trusted proxies: %w", err)
	}
	router.TrustedPlatform = gin.PlatformCloudflare

	router.Use(gin.Recovery())
	router.Use(sharedserver.LoggerMiddleware(ctx, logger,
		"/health",
		"/metrics",
	))

	registerAPIHealthRoutes(router)

	// Iris webhook 수신 (h2c POST)
	if webhookHandler != nil {
		router.POST("/webhook/iris", webhookHandler.Handle)
	}

	// 내부 트리거 라우트 (운영 API에서 내부 호출)
	if triggerHandler != nil {
		triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), cfg.Server.APIKey)
	}

	return router, nil
}
