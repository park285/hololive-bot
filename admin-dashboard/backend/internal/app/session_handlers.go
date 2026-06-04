package app

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/pkg/ginjson"
	"github.com/park285/shared-go/pkg/json"
	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/admin-dashboard/internal/session"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (r *Runtime) handleLogin(c *gin.Context) {
	var body loginRequest
	if err := httpx.DecodeJSON(c.Request, &body, 16<<10); err != nil {
		httpx.Abort(c, httpx.BadRequest("invalid login payload"))
		return
	}
	ip := r.clientIP(c.Request)
	allowed, retryAfter := r.rateLimiter.IsAllowed(ip)
	if !allowed {
		retry := uint64(retryAfter.Seconds())
		httpx.Abort(c, httpx.AppError{Status: http.StatusTooManyRequests, Body: httpx.ErrorResponse{Error: "Too many login attempts", RetryAfter: &retry}})
		return
	}
	usernameOK := auth.ConstantTimeStringEqual(body.Username, r.cfg.AdminUser)
	passwordOK := bcrypt.CompareHashAndPassword([]byte(r.cfg.AdminPassHash), []byte(body.Password)) == nil
	if !(usernameOK && passwordOK) {
		count := r.rateLimiter.RecordFailure(ip)
		delay := time.Duration(min(count*500, 3000)) * time.Millisecond
		time.Sleep(delay)
		httpx.Abort(c, httpx.Unauthorized())
		return
	}
	r.rateLimiter.RecordSuccess(ip)
	sess, err := r.sessions.Create(c.Request.Context())
	if err != nil {
		r.logger.Error("session create failed", slog.Any("error", err))
		httpx.Abort(c, httpx.StoreUnavailable())
		return
	}
	csrf, err := auth.NewCSRFToken(sess.ID, r.cfg.SessionSecret)
	if err != nil {
		httpx.Abort(c, httpx.Internal(err))
		return
	}
	secure := r.secureCookie()
	auth.SetSessionCookie(c.Writer, auth.SignSessionID(sess.ID, r.cfg.SessionSecret), r.cfg.Session.ExpiryDuration, secure)
	auth.SetCSRFCookie(c.Writer, csrf, secure)
	ginjson.Respond(c, http.StatusOK, loginResponse{Status: "ok", Message: "Login successful", CSRFToken: csrf})
}

func (r *Runtime) handleSessionStatus(c *gin.Context) {
	sessionID, ok := sessionIDFrom(c)
	if !ok {
		httpx.Abort(c, httpx.Unauthorized())
		return
	}
	sess, err := r.sessions.Get(c.Request.Context(), sessionID)
	if err != nil {
		r.logger.Error("session lookup failed", slog.Any("error", err))
		httpx.Abort(c, httpx.StoreUnavailable())
		return
	}
	if sess == nil {
		httpx.Abort(c, httpx.Unauthorized())
		return
	}
	ginjson.Respond(c, http.StatusOK, sessionStatusResponse{
		Status:            "ok",
		Authenticated:     true,
		Username:          r.cfg.AdminUser,
		AbsoluteExpiresAt: sess.AbsoluteExpiresAt.Unix(),
		SessionPolicy: sessionPolicy{
			HeartbeatIntervalMS:     uint64(r.cfg.Session.HeartbeatInterval.Milliseconds()),
			IdleTimeoutMS:           uint64(r.cfg.Session.IdleTimeout.Milliseconds()),
			IdleWarningTimeoutMS:    uint64(r.cfg.Session.IdleWarningTimeout.Milliseconds()),
			IdleSessionTTLMS:        uint64(r.cfg.Session.IdleSessionTTL.Milliseconds()),
			AbsoluteWarningWindowMS: uint64(r.cfg.Session.AbsoluteWarningWindow.Milliseconds()),
		},
	})
}

func (r *Runtime) handleLogout(c *gin.Context) {
	if sessionID, ok := sessionIDFrom(c); ok {
		_ = r.sessions.Delete(c.Request.Context(), sessionID)
	}
	auth.ClearAuthCookies(c.Writer, r.secureCookie())
	ginjson.Respond(c, http.StatusOK, statusResponse{Status: "ok"})
}

type heartbeatRequest struct {
	Idle bool `json:"idle"`
}

func (r *Runtime) handleHeartbeat(c *gin.Context) {
	sessionID, ok := sessionIDFrom(c)
	if !ok {
		httpx.Abort(c, httpx.Unauthorized())
		return
	}
	hb, err := parseHeartbeat(c.Request)
	if err != nil {
		httpx.Abort(c, httpx.BadRequest("Invalid heartbeat payload"))
		return
	}
	result, err := r.sessions.Refresh(c.Request.Context(), sessionID, hb.Idle)
	if err != nil {
		r.logger.Error("session refresh failed", slog.Any("error", err))
		httpx.Abort(c, httpx.StoreUnavailable())
		return
	}
	r.writeHeartbeatResult(c, sessionID, result)
}

func parseHeartbeat(req *http.Request) (heartbeatRequest, error) {
	body, _ := io.ReadAll(io.LimitReader(req.Body, 1024))
	_ = req.Body.Close()
	hb := heartbeatRequest{}
	if len(strings.TrimSpace(string(body))) == 0 {
		return hb, nil
	}
	if err := json.Unmarshal(body, &hb); err != nil {
		return hb, err
	}
	return hb, nil
}

func (r *Runtime) writeHeartbeatResult(c *gin.Context, sessionID string, result session.RefreshResult) {
	secure := r.secureCookie()
	switch result.Kind {
	case session.RefreshRefreshed:
		r.heartbeatRefreshed(c, sessionID, result, secure)
	case session.RefreshRotated:
		r.writeHeartbeatSession(c, result.Session, true, secure)
	case session.RefreshIdleShortened:
		ginjson.Respond(c, http.StatusOK, heartbeatIdleResponse{Status: "idle", IdleRejected: true})
	default:
		r.heartbeatDenied(c, result.Kind, secure)
	}
}

func (r *Runtime) heartbeatRefreshed(c *gin.Context, sessionID string, result session.RefreshResult, secure bool) {
	if r.cfg.Session.TokenRotationEnabled {
		rotated, err := r.sessions.Rotate(c.Request.Context(), sessionID)
		if err != nil {
			r.logger.Error("session rotate failed", slog.Any("error", err))
			httpx.Abort(c, httpx.StoreUnavailable())
			return
		}
		if rotated != nil {
			r.writeHeartbeatSession(c, rotated, true, secure)
			return
		}
	}
	ginjson.Respond(c, http.StatusOK, heartbeatOKResponse{Status: "ok", AbsoluteExpiresAt: result.Session.AbsoluteExpiresAt.Unix()})
}

func (r *Runtime) heartbeatDenied(c *gin.Context, kind session.RefreshKind, secure bool) {
	auth.ClearAuthCookies(c.Writer, secure)
	if kind == session.RefreshAbsoluteExpired {
		absolute := true
		httpx.Abort(c, httpx.AppError{Status: http.StatusUnauthorized, Body: httpx.ErrorResponse{Error: "Session expired", AbsoluteExpired: &absolute}})
		return
	}
	httpx.Abort(c, httpx.Unauthorized())
}

func (r *Runtime) writeHeartbeatSession(c *gin.Context, sess *session.Session, rotated bool, secure bool) {
	csrf, err := auth.NewCSRFToken(sess.ID, r.cfg.SessionSecret)
	if err != nil {
		httpx.Abort(c, httpx.Internal(err))
		return
	}
	maxAge := max(time.Until(sess.ExpiresAt), time.Second)
	auth.SetSessionCookie(c.Writer, auth.SignSessionID(sess.ID, r.cfg.SessionSecret), maxAge, secure)
	auth.SetCSRFCookie(c.Writer, csrf, secure)
	ginjson.Respond(c, http.StatusOK, heartbeatRotatedResponse{Status: "ok", Rotated: rotated, AbsoluteExpiresAt: sess.AbsoluteExpiresAt.Unix(), CSRFToken: csrf})
}
