package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/admin-dashboard/internal/session"
)

const heartbeatPath = "/admin/api/auth/heartbeat"

func (r *API) auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, sess, err := r.resolveSession(c.Request)
		if err != nil {
			// 늦은 인증 거부가 새 로그인 쿠키를 지우지 않도록 일반 API는 쿠키를 쓰지 않습니다.
			httpx.Abort(c, err)
			r.auditSecurityRejection(c, "admin.authentication.denied")

			return
		}

		c.Set(sessionIDKey, sessionID)
		c.Set(sessionObjKey, sess)
		c.Next()
	}
}

func (r *API) resolveSession(req *http.Request) (sessionID string, sess *session.Session, err error) {
	cookie, err := req.Cookie(auth.SessionCookieName)
	if err != nil {
		return "", nil, contract.Unauthorized()
	}

	sessionID, ok := auth.ValidateSessionSignature(cookie.Value, r.cfg.SessionSecret)
	if !ok {
		return "", nil, contract.Unauthorized()
	}

	current, found, err := r.sessions.Get(req.Context(), sessionID)
	if err != nil {
		r.logger.Error("session lookup failed", slog.Any("error", err))

		return "", nil, contract.StoreUnavailable()
	}

	if !found {
		return "", nil, contract.Unauthorized()
	}

	sess = &current

	if sess.RotatedTo != nil && req.URL.Path != heartbeatPath {
		rotatedID, rotated, err := r.resolveRotatedSession(req, sessionID, *sess.RotatedTo)
		if err != nil {
			return "", nil, fmt.Errorf("resolve rotated session: %w", err)
		}

		return rotatedID, rotated, nil
	}

	return sessionID, sess, nil
}

// 회전 유예 중 옛 쿠키 요청은 교체 세션을 따라간다. 쿠키를 지우면 동시 heartbeat가 방금 심은 새 쿠키까지 삭제되므로,
// 실패 경로에서도 ClearAuthCookies를 하지 않는다. CSRF 바인딩은 요청이 실제로 들고 온 marker ID를 유지해야 성립한다.
func (r *API) resolveRotatedSession(req *http.Request, markerID, rotatedTo string) (string, *session.Session, error) {
	replacement, found, err := r.sessions.Get(req.Context(), rotatedTo)
	if err != nil {
		r.logger.Error("rotated session lookup failed", slog.Any("error", err))

		return "", nil, contract.StoreUnavailable()
	}

	if !found || replacement.RotatedTo != nil {
		return "", nil, contract.Unauthorized()
	}

	return markerID, &replacement, nil
}

func (r *API) csrf() gin.HandlerFunc {
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
			r.logger.Warn("csrf violation monitor", slog.String("session_id", stringutil.TruncateString(sessionID, 8)))
			c.Next()

			return
		}

		httpx.Abort(c, contract.Forbidden())
		r.auditSecurityRejection(c, "admin.csrf.denied")
	}
}

func csrfExempt(method string, mode config.SecurityMode) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || mode == config.SecurityOff
}

func (r *API) csrfTokenValid(req *http.Request, sessionID string) bool {
	headerToken := req.Header.Get("X-CSRF-Token")
	if headerToken == "" {
		return false
	}

	cookie, err := req.Cookie(auth.CSRFCookieName)
	if err != nil || cookie.Value == "" || !httputil.ConstantTimeStringEqual(cookie.Value, headerToken) {
		return false
	}

	return auth.ValidateCSRFToken(sessionID, headerToken, r.cfg.SessionSecret)
}

func (r *API) clientIP(req *http.Request) string {
	return httputil.ClientIP(req, httputil.ClientIPOptions{
		TrustForwarded: r.cfg.TrustedForwarders,
		TrustedProxies: r.cfg.TrustedProxyCIDRs,
		ForwardedMode:  httputil.ForwardedHeaderRightmostNonTrusted,
	})
}
