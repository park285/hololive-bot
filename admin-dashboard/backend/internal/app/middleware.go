package app

import (
	"hash/fnv"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/httpx"
)

func (r *Runtime) auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, clearCookies, err := r.resolveSession(c.Request)
		if err != nil {
			if clearCookies {
				auth.ClearAuthCookies(c.Writer, r.secureCookie())
			}
			httpx.Abort(c, err)
			return
		}
		c.Set(sessionIDKey, sessionID)
		c.Next()
	}
}

func (r *Runtime) resolveSession(req *http.Request) (string, bool, error) {
	cookie, err := req.Cookie(auth.SessionCookieName)
	if err != nil {
		return "", false, httpx.Unauthorized()
	}
	sessionID, ok := auth.ValidateSessionSignature(cookie.Value, r.cfg.SessionSecret)
	if !ok {
		return "", true, httpx.Unauthorized()
	}
	sess, err := r.sessions.Get(req.Context(), sessionID)
	if err != nil {
		r.logger.Error("session lookup failed", slog.Any("error", err))
		return "", false, httpx.StoreUnavailable()
	}
	if sess == nil || (sess.RotatedTo != nil && req.URL.Path != "/admin/api/auth/heartbeat") {
		return "", true, httpx.Unauthorized()
	}
	return sessionID, false, nil
}

func (r *Runtime) csrf() gin.HandlerFunc {
	return func(c *gin.Context) {
		if csrfExempt(c.Request.Method, r.cfg.Security.CSRFMode) {
			c.Next()
			return
		}
		sessionID, _ := sessionIDFrom(c)
		if r.csrfTokenValid(c.Request, sessionID) {
			c.Next()
			return
		}
		if r.cfg.Security.CSRFMode == config.SecurityMonitor {
			r.logger.Warn("csrf violation monitor", slog.String("session_id", auth.TruncateSessionID(sessionID)))
			c.Next()
			return
		}
		httpx.Abort(c, httpx.Forbidden())
	}
}

func csrfExempt(method string, mode config.SecurityMode) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || mode == config.SecurityOff
}

func (r *Runtime) csrfTokenValid(req *http.Request, sessionID string) bool {
	headerToken := req.Header.Get("X-CSRF-Token")
	if headerToken == "" {
		return false
	}
	cookie, err := req.Cookie(auth.CSRFCookieName)
	if err != nil || cookie.Value != headerToken {
		return false
	}
	return auth.ValidateCSRFToken(sessionID, headerToken, r.cfg.SessionSecret)
}

func (r *Runtime) securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("X-XSS-Protection", "1; mode=block")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://*.ytimg.com https://*.ggpht.com; connect-src 'self' ws: wss:; frame-ancestors 'none'")
		if r.cfg.Security.ForceHTTPS {
			header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

type etagWriter struct {
	gin.ResponseWriter
	body   strings.Builder
	status int
}

func (w *etagWriter) WriteHeader(status int) {
	w.status = status
}

func (w *etagWriter) WriteHeaderNow() {}

func (w *etagWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *etagWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *etagWriter) Status() int {
	if w.status != 0 {
		return w.status
	}
	return w.ResponseWriter.Status()
}

func (w *etagWriter) Size() int {
	return w.body.Len()
}

func (w *etagWriter) Written() bool {
	return w.status != 0
}

func (w *etagWriter) flush(req *http.Request) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.status != http.StatusOK {
		w.ResponseWriter.WriteHeader(w.status)
		_, _ = w.ResponseWriter.Write([]byte(w.body.String()))
		return
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(w.body.String()))
	etag := `"` + strconv.FormatUint(h.Sum64(), 16) + `"`
	w.ResponseWriter.Header().Set("ETag", etag)
	if etagMatches(req.Header.Get("If-None-Match"), etag) {
		w.ResponseWriter.WriteHeader(http.StatusNotModified)
		return
	}
	w.ResponseWriter.WriteHeader(http.StatusOK)
	_, _ = w.ResponseWriter.Write([]byte(w.body.String()))
}

func (r *Runtime) etag() gin.HandlerFunc {
	return func(c *gin.Context) {
		if etagSkipped(c.Request) {
			c.Next()
			return
		}
		writer := &etagWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		c.Next()
		writer.flush(c.Request)
	}
}

func etagSkipped(req *http.Request) bool {
	return req.Method != http.MethodGet || !strings.HasPrefix(req.URL.Path, "/admin/api/") || req.Header.Get("Upgrade") != ""
}

func etagMatches(header, etag string) bool {
	return header == etag || header == strings.Trim(etag, `"`)
}

func (r *Runtime) verifyWSOrigin(origin string) error {
	if r.cfg.Security.WSOriginMode == config.SecurityOff {
		return nil
	}
	allowed := slices.Contains(r.cfg.Security.AllowedOrigins, origin)
	if origin == "" || !allowed {
		if r.cfg.Security.WSOriginMode == config.SecurityMonitor {
			r.logger.Warn("ws origin rejected in monitor mode", slog.String("origin", origin))
			return nil
		}
		return httpx.Forbidden()
	}
	return nil
}

func (r *Runtime) clientIP(req *http.Request) string {
	if r.cfg.TrustedForwarders {
		if ip := forwardedClientIP(req); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

func forwardedClientIP(req *http.Request) string {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		value := req.Header.Get(header)
		if value == "" {
			continue
		}
		candidate := strings.TrimSpace(strings.Split(value, ",")[0])
		if net.ParseIP(candidate) != nil {
			return candidate
		}
	}
	return ""
}

func (r *Runtime) secureCookie() bool { return r.cfg.Security.ForceHTTPS }
