package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/pkg/ginjson"
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
		httpx.Abort(c, &httpx.AppError{Status: http.StatusTooManyRequests, Body: httpx.ErrorResponse{Error: "Too many login attempts", RetryAfter: &retry}})
		return
	}
	usernameOK := auth.ConstantTimeStringEqual(body.Username, r.cfg.AdminUser)
	passwordOK := bcrypt.CompareHashAndPassword([]byte(r.cfg.AdminPassHash), []byte(body.Password)) == nil
	if !(usernameOK && passwordOK) {
		count := r.rateLimiter.RecordFailure(ip)
		delay := time.Duration(min(count*500, 3000)) * time.Millisecond
		if !waitForLoginBackoff(c.Request.Context(), delay) {
			return
		}
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
	auth.SetSessionCookie(c.Writer, auth.SignSessionID(sess.ID, r.cfg.SessionSecret), r.cfg.Session.ExpiryDuration, r.cfg.Security.ForceHTTPS)
	auth.SetCSRFCookie(c.Writer, csrf, r.cfg.Security.ForceHTTPS)
	ginjson.Respond(c, http.StatusOK, loginResponse{Status: "ok", Message: "Login successful", CSRFToken: csrf})
}

func (r *Runtime) handleSessionStatus(c *gin.Context) {
	sessionID, ok := sessionIDFrom(c)
	if !ok {
		httpx.Abort(c, httpx.Unauthorized())
		return
	}
	sess, ok := sessionFrom(c)
	if !ok {
		httpx.Abort(c, httpx.Unauthorized())
		return
	}
	csrf, err := r.sessionStatusCSRFToken(c.Request, sessionID)
	if err != nil {
		httpx.Abort(c, httpx.Internal(err))
		return
	}
	auth.SetCSRFCookie(c.Writer, csrf, r.cfg.Security.ForceHTTPS)
	ginjson.Respond(c, http.StatusOK, sessionStatusResponse{
		Status:            "ok",
		Authenticated:     true,
		Username:          r.cfg.AdminUser,
		AbsoluteExpiresAt: sess.AbsoluteExpiresAt.Unix(),
		CSRFToken:         csrf,
		SessionPolicy: sessionPolicy{
			HeartbeatIntervalMS:     durationMillis(r.cfg.Session.HeartbeatInterval),
			IdleTimeoutMS:           durationMillis(r.cfg.Session.IdleTimeout),
			IdleWarningTimeoutMS:    durationMillis(r.cfg.Session.IdleWarningTimeout),
			IdleSessionTTLMS:        durationMillis(r.cfg.Session.IdleSessionTTL),
			AbsoluteWarningWindowMS: durationMillis(r.cfg.Session.AbsoluteWarningWindow),
		},
	})
}

func (r *Runtime) sessionStatusCSRFToken(req *http.Request, sessionID string) (string, error) {
	if cookie, err := req.Cookie(auth.CSRFCookieName); err == nil && auth.ValidateCSRFToken(sessionID, cookie.Value, r.cfg.SessionSecret) {
		return cookie.Value, nil
	}
	return auth.NewCSRFToken(sessionID, r.cfg.SessionSecret)
}

func (r *Runtime) handleLogout(c *gin.Context) {
	if sessionID, ok := sessionIDFrom(c); ok {
		if err := r.sessions.Delete(c.Request.Context(), sessionID); err != nil {
			r.logger.Warn("session delete failed during logout", slog.Any("error", err))
		}
	}
	auth.ClearAuthCookies(c.Writer, r.cfg.Security.ForceHTTPS)
	ginjson.Respond(c, http.StatusOK, statusResponse{Status: "ok"})
}

const maxHeartbeatBodyBytes int64 = 1024

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
	body, err := io.ReadAll(io.LimitReader(req.Body, maxHeartbeatBodyBytes+1))
	if closeErr := req.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return heartbeatRequest{}, err
	}
	if int64(len(body)) > maxHeartbeatBodyBytes {
		return heartbeatRequest{}, fmt.Errorf("heartbeat body exceeds %d bytes", maxHeartbeatBodyBytes)
	}

	hb := heartbeatRequest{}
	if len(bytes.TrimSpace(body)) == 0 {
		return hb, nil
	}
	if err := httpx.DecodeJSONBytes(body, &hb); err != nil {
		return hb, err
	}
	return hb, nil
}

func waitForLoginBackoff(ctx context.Context, delay time.Duration) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

func (r *Runtime) writeHeartbeatResult(c *gin.Context, sessionID string, result session.RefreshResult) {
	if result.Kind == session.RefreshRefreshed {
		r.heartbeatRefreshed(c, sessionID, result)
		return
	}
	r.writeTerminalHeartbeatResult(c, result)
}

func (r *Runtime) writeTerminalHeartbeatResult(c *gin.Context, result session.RefreshResult) {
	if r.writeRotatedHeartbeatResult(c, result) {
		return
	}
	if r.writeIdleHeartbeatResult(c, result) {
		return
	}
	r.heartbeatDenied(c, result.Kind)
}

func (r *Runtime) writeRotatedHeartbeatResult(c *gin.Context, result session.RefreshResult) bool {
	if result.Kind != session.RefreshRotated {
		return false
	}
	r.writeHeartbeatSession(c, result.Session, true)
	return true
}

func (r *Runtime) writeIdleHeartbeatResult(c *gin.Context, result session.RefreshResult) bool {
	if result.Kind != session.RefreshIdleShortened {
		return false
	}
	ginjson.Respond(c, http.StatusOK, heartbeatIdleResponse{Status: "idle", IdleRejected: true})
	return true
}

func (r *Runtime) heartbeatRefreshed(c *gin.Context, sessionID string, result session.RefreshResult) {
	if r.cfg.Session.TokenRotationEnabled {
		rotated, err := r.sessions.Rotate(c.Request.Context(), sessionID)
		if err != nil {
			r.logger.Error("session rotate failed", slog.Any("error", err))
			httpx.Abort(c, httpx.StoreUnavailable())
			return
		}
		if rotated != nil {
			r.writeHeartbeatSession(c, rotated, true)
			return
		}
	}
	maxAge := max(time.Until(result.Session.ExpiresAt), time.Second)
	auth.SetSessionCookie(c.Writer, auth.SignSessionID(sessionID, r.cfg.SessionSecret), maxAge, r.cfg.Security.ForceHTTPS)
	ginjson.Respond(c, http.StatusOK, heartbeatOKResponse{Status: "ok", AbsoluteExpiresAt: result.Session.AbsoluteExpiresAt.Unix()})
}

func (r *Runtime) heartbeatDenied(c *gin.Context, kind session.RefreshKind) {
	auth.ClearAuthCookies(c.Writer, r.cfg.Security.ForceHTTPS)
	if kind == session.RefreshAbsoluteExpired {
		absolute := true
		httpx.Abort(c, &httpx.AppError{Status: http.StatusUnauthorized, Body: httpx.ErrorResponse{Error: "Session expired", AbsoluteExpired: &absolute}})
		return
	}
	httpx.Abort(c, httpx.Unauthorized())
}

func (r *Runtime) writeHeartbeatSession(c *gin.Context, sess *session.Session, rotated bool) {
	csrf, err := auth.NewCSRFToken(sess.ID, r.cfg.SessionSecret)
	if err != nil {
		httpx.Abort(c, httpx.Internal(err))
		return
	}
	maxAge := max(time.Until(sess.ExpiresAt), time.Second)
	auth.SetSessionCookie(c.Writer, auth.SignSessionID(sess.ID, r.cfg.SessionSecret), maxAge, r.cfg.Security.ForceHTTPS)
	auth.SetCSRFCookie(c.Writer, csrf, r.cfg.Security.ForceHTTPS)
	ginjson.Respond(c, http.StatusOK, heartbeatRotatedResponse{Status: "ok", Rotated: rotated, AbsoluteExpiresAt: sess.AbsoluteExpiresAt.Unix(), CSRFToken: csrf})
}

func durationMillis(duration time.Duration) uint64 {
	if duration <= 0 {
		return 0
	}
	return uint64(duration / time.Millisecond)
}
