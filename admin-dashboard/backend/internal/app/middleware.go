package app

import (
	"hash/fnv"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/pkg/httputil"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/admin-dashboard/internal/session"
)

func (r *Runtime) auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, sess, clearCookies, err := r.resolveSession(c.Request)
		if err != nil {
			if clearCookies {
				auth.ClearAuthCookies(c.Writer, r.cfg.Security.ForceHTTPS)
			}
			httpx.Abort(c, err)
			return
		}
		c.Set(sessionIDKey, sessionID)
		c.Set(sessionObjKey, sess)
		c.Next()
	}
}

func (r *Runtime) resolveSession(req *http.Request) (sessionID string, sess *session.Session, clearCookies bool, err error) {
	cookie, err := req.Cookie(auth.SessionCookieName)
	if err != nil {
		return "", nil, false, httpx.Unauthorized()
	}
	sessionID, ok := auth.ValidateSessionSignature(cookie.Value, r.cfg.SessionSecret)
	if !ok {
		return "", nil, true, httpx.Unauthorized()
	}
	sess, err = r.sessions.Get(req.Context(), sessionID)
	if err != nil {
		r.logger.Error("session lookup failed", slog.Any("error", err))
		return "", nil, false, httpx.StoreUnavailable()
	}
	if sess == nil || (sess.RotatedTo != nil && req.URL.Path != "/admin/api/auth/heartbeat") {
		return "", nil, true, httpx.Unauthorized()
	}
	return sessionID, sess, false, nil
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

const etagMaxBufferBytes = 64 << 10

type etagWriter struct {
	gin.ResponseWriter
	body     strings.Builder
	status   int
	limit    int
	overflow bool
}

func (w *etagWriter) WriteHeader(status int) {
	if w.overflow {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
}

func (w *etagWriter) WriteHeaderNow() {}

func (w *etagWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if !w.overflow && w.body.Len()+len(data) > w.limit {
		w.startOverflow()
	}
	if w.overflow {
		return w.ResponseWriter.Write(data)
	}
	return w.body.Write(data)
}

func (w *etagWriter) WriteString(s string) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if !w.overflow && w.body.Len()+len(s) > w.limit {
		w.startOverflow()
	}
	if w.overflow {
		return w.ResponseWriter.WriteString(s)
	}
	return w.body.WriteString(s)
}

func (w *etagWriter) startOverflow() {
	w.overflow = true
	w.ResponseWriter.WriteHeader(w.status)
	writeBufferedString(w.ResponseWriter, w.body.String())
	w.body.Reset()
}

func (w *etagWriter) Status() int {
	if w.status != 0 {
		return w.status
	}
	return w.ResponseWriter.Status()
}

func (w *etagWriter) Size() int {
	if w.overflow {
		return w.ResponseWriter.Size()
	}
	return w.body.Len()
}

func (w *etagWriter) Written() bool {
	return w.status != 0
}

func (w *etagWriter) flush(req *http.Request) {
	if w.overflow {
		return
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.status != http.StatusOK {
		w.ResponseWriter.WriteHeader(w.status)
		writeBufferedString(w.ResponseWriter, w.body.String())
		return
	}
	h := fnv.New64a()
	writeHash(h, w.body.String())
	etag := `"` + strconv.FormatUint(h.Sum64(), 16) + `"`
	w.ResponseWriter.Header().Set("ETag", etag)
	if etagMatches(req.Header.Get("If-None-Match"), etag) {
		w.ResponseWriter.WriteHeader(http.StatusNotModified)
		return
	}
	w.ResponseWriter.WriteHeader(http.StatusOK)
	writeBufferedString(w.ResponseWriter, w.body.String())
}

func writeBufferedString(w gin.ResponseWriter, body string) {
	if _, err := w.WriteString(body); err != nil {
		return
	}
}

func writeHash(h interface{ Write([]byte) (int, error) }, body string) {
	if _, err := h.Write([]byte(body)); err != nil {
		return
	}
}

func (r *Runtime) etag() gin.HandlerFunc {
	return func(c *gin.Context) {
		if etagSkipped(c.Request) {
			c.Next()
			return
		}
		writer := &etagWriter{ResponseWriter: c.Writer, limit: etagMaxBufferBytes}
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
	return httputil.ClientIP(req, httputil.ClientIPOptions{
		TrustForwarded: r.cfg.TrustedForwarders,
		TrustedProxies: r.cfg.TrustedProxyCIDRs,
		ForwardedMode:  httputil.ForwardedHeaderRightmostNonTrusted,
	})
}
