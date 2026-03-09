// Package server: HTTP 서버 및 라우팅
package server

import (
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/auth"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/config"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/docker"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/middleware"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/proxy"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/ssr"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/static"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/status"
)

// Server: HTTP 서버
type Server struct {
	engine          *gin.Engine
	cfg             *config.Config
	securityCfg     *config.SecurityConfig
	logger          *slog.Logger
	sessions        auth.SessionProvider
	rateLimiter     *auth.LoginRateLimiter
	dockerSvc       docker.DockerProvider
	streamLimiter   *StreamLimiter
	botProxies      *proxy.BotProxies
	statusCollector *status.Collector
	ssrInjector     *ssr.Injector
	ssrConfig       ssr.Config

	// Origin 검증 캐시 (SecurityConfig에서 로드)
	allowedOriginsMap   map[string]struct{}
	allowedOriginsSlice []string
}

// New: 서버 생성
func New(
	cfg *config.Config,
	logger *slog.Logger,
	sessions auth.SessionProvider,
	dockerSvc *docker.Service,
	botProxies *proxy.BotProxies,
	statusCollector *status.Collector,
) *Server {
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	securityCfg := config.LoadSecurityConfig(cfg.Environment, logger)
	engine := gin.New()

	setupOTelMiddleware(engine, cfg, logger)
	engine.Use(gin.Recovery())
	engine.Use(auth.SecurityHeadersMiddleware(cfg.ForceHTTPS))

	allowedOriginsSlice := setupCORS(engine, securityCfg)
	setupPerformanceMiddlewares(engine)

	ssrConfig, ssrInjector := initializeSSR(dockerSvc, cfg.HoloBotURL, logger)
	streamLimiter := NewStreamLimiter(
		securityCfg.GlobalStreamLimit,
		securityCfg.PerSessionStreamLimit,
		securityCfg.StreamLimitMode,
		logger,
	)

	s := &Server{
		engine:              engine,
		cfg:                 cfg,
		securityCfg:         securityCfg,
		logger:              logger,
		sessions:            sessions,
		rateLimiter:         auth.NewLoginRateLimiter(),
		dockerSvc:           dockerSvc,
		streamLimiter:       streamLimiter,
		botProxies:          botProxies,
		statusCollector:     statusCollector,
		ssrInjector:         ssrInjector,
		ssrConfig:           ssrConfig,
		allowedOriginsMap:   securityCfg.AllowedOriginsMap(),
		allowedOriginsSlice: allowedOriginsSlice,
	}

	s.setupRoutes()
	return s
}

// setupOTelMiddleware: OpenTelemetry 미들웨어 설정
func setupOTelMiddleware(engine *gin.Engine, cfg *config.Config, logger *slog.Logger) {
	if !cfg.OTELEnabled {
		return
	}

	serviceName := strings.TrimSpace(cfg.OTELServiceName)
	if serviceName == "" {
		serviceName = "admin-dashboard"
	}
	engine.Use(otelgin.Middleware(serviceName))
	if logger != nil {
		logger.Info("otel_http_middleware_enabled", slog.String("service", serviceName))
	}
}

// setupCORS: CORS 미들웨어 설정 및 Origin 목록 반환
func setupCORS(engine *gin.Engine, securityCfg *config.SecurityConfig) []string {
	allowedOriginsSlice := make([]string, len(securityCfg.AllowedOrigins))
	copy(allowedOriginsSlice, securityCfg.AllowedOrigins)
	sort.Strings(allowedOriginsSlice)

	engine.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOriginsSlice,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-CSRF-Token"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	return allowedOriginsSlice
}

// setupPerformanceMiddlewares: 성능 최적화 미들웨어 설정
func setupPerformanceMiddlewares(engine *gin.Engine) {
	// 압축: Cloudflare Tunnel Edge에서 Brotli/Gzip 처리 (서버 CPU 자원 보호)

	// ETag: API GET 응답에 조건부 요청 지원 (304 Not Modified)
	engine.Use(middleware.ETag())

	// Early Hints: 비활성화 - Cloudflare Tunnel과 호환 문제로 임시 비활성화
	// TODO: Cloudflare Tunnel 환경에서 103 응답이 제대로 전달되는지 확인 필요
	// engine.Use(middleware.EarlyHints(nil))

	// Static 캐시 미들웨어
	engine.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		}
		c.Next()
	})
}

// initializeSSR: SSR 설정 및 Injector 초기화
func initializeSSR(dockerSvc *docker.Service, holoBotURL string, logger *slog.Logger) (ssr.Config, *ssr.Injector) {
	ssrConfig := ssr.DefaultConfig()
	ssrInjector := ssr.NewInjector(dockerSvc, holoBotURL, logger)

	// HTML 캐시 로드: 임베디드 우선, 파일시스템 폴백
	if static.HasEmbedded() {
		if htmlData, err := static.IndexHTML(); err == nil {
			ssrInjector.LoadHTMLFromBytes(htmlData)
			logger.Info("ssr_using_embedded_static")
		}
	} else {
		if err := ssrInjector.LoadHTMLCache(ssrConfig.IndexPath); err != nil {
			logger.Warn("ssr_html_cache_failed", slog.Any("error", err))
		} else {
			logger.Info("ssr_html_cache_loaded", slog.String("path", ssrConfig.IndexPath))
		}
	}

	return ssrConfig, ssrInjector
}

// checkAuthFromCookie: 세션 쿠키에서 인증 상태 확인 (SSR 전용)
func (s *Server) checkAuthFromCookie(c *gin.Context) bool {
	signedSessionID, err := c.Cookie("admin_session")
	if err != nil || signedSessionID == "" {
		return false
	}

	sessionID, valid := auth.ValidateSessionSignature(signedSessionID, s.cfg.AdminSecretKey)
	if !valid {
		return false
	}

	return s.sessions.ValidateSession(c.Request.Context(), sessionID)
}

// HTTPServer: net/http.Server 인스턴스를 반환합니다.
func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Addr:              ":" + s.cfg.Port,
		Handler:           s.engine,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// setupHealthRoute: 헬스체크 라우트 (인증 없음)
func (s *Server) setupHealthRoute() {
	s.engine.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}

// setupStaticRoutes: React SPA 정적 파일 서빙
func (s *Server) setupStaticRoutes() {
	// 임베디드 FS 사용 여부
	useEmbedded := static.HasEmbedded()

	// Static assets (/assets/*)
	if useEmbedded {
		assetsFS, err := static.Assets()
		if err == nil {
			s.engine.StaticFS("/assets", http.FS(assetsFS))
		}
	} else {
		s.engine.Static("/assets", s.ssrConfig.AssetsDir)
	}

	// Favicon
	s.engine.GET("/favicon.svg", func(c *gin.Context) {
		c.Header("Cache-Control", s.ssrConfig.CacheControlFavicon)
		if useEmbedded {
			if data, err := static.Favicon(); err == nil {
				c.Data(200, "image/svg+xml", data)
				return
			}
		}
		c.File(s.ssrConfig.FaviconPath)
	})

	// SSR 데이터 주입 핸들러
	serveWithSSR := func(c *gin.Context) {
		c.Header("Cache-Control", s.ssrConfig.CacheControlHTML)

		// 인증 상태 확인
		isAuthenticated := s.checkAuthFromCookie(c)
		sessionCookie, _ := c.Cookie("admin_session")

		// SSR 데이터 주입 시도
		html, err := s.ssrInjector.InjectForPath(c.Request.Context(), c.Request.URL.Path, isAuthenticated, sessionCookie)
		if err != nil || len(html) == 0 {
			// 폴백: 캐시된 HTML 또는 파일 시스템
			if cachedHTML := s.ssrInjector.GetHTMLCache(); len(cachedHTML) > 0 {
				c.Data(200, "text/html; charset=utf-8", cachedHTML)
				return
			}
			c.File(s.ssrConfig.IndexPath)
			return
		}

		c.Data(200, "text/html; charset=utf-8", html)
	}

	// 루트 경로
	s.engine.GET("/", serveWithSSR)

	// SPA Fallback (NoRoute)
	s.engine.NoRoute(serveWithSSR)
}
