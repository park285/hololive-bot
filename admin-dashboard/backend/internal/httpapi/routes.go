package httpapi

import (
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
)

// Handler는 명세와 접근 선언이 일치하는 route만 등록합니다. 불일치는 시작을 중단합니다.
func (r *API) Handler() http.Handler {
	// crosscutting:allow 이름 기반 검사는 recoverPanics를 인식하지 못합니다. 아래 등록은 TestPanicResponseDoesNotDumpSecrets로 검증합니다.
	engine := gin.New()

	engine.HandleMethodNotAllowed = true
	engine.RedirectTrailingSlash = false
	engine.Use(r.requestContext(), r.securityHeaders(), r.recoverPanics(), r.contractGeneration(), r.etag())

	engine.GET("/health", r.handleHealth)
	engine.GET("/favicon.svg", gin.WrapF(r.static.ServeFavicon))
	engine.GET("/theme-init.js", gin.WrapF(r.static.ServeThemeInit))
	engine.GET("/assets/*filepath", gin.WrapF(r.static.ServeAsset))

	r.registerEndpoints(engine)
	engine.GET("/admin/api/ws/system-stats", r.admit("systemStatsStream", false), r.handleSystemStatsWS)

	engine.GET("/admin/docs", r.admit("adminDocs", false), r.auth(), r.handleDocs)

	engine.NoMethod(func(c *gin.Context) {
		httpx.Abort(c, contract.NewError(http.StatusMethodNotAllowed, "Method not allowed"))
	})
	engine.NoRoute(r.handleFallback)

	return engine
}

func (r *API) handleFallback(c *gin.Context) {
	if isAdminAPI(c.Request.URL.Path) || strings.HasPrefix(c.Request.URL.Path, "/assets") || path.Ext(c.Request.URL.Path) != "" {
		httpx.Abort(c, contract.NewError(http.StatusNotFound, "Not found"))

		return
	}

	// gin NoRoute는 핸들러 진입 전에 status를 404로 선설정하므로 SPA fallback은 200을 명시해야 한다.
	c.Status(http.StatusOK)
	r.static.ServeIndex(c.Writer, c.Request)
}
