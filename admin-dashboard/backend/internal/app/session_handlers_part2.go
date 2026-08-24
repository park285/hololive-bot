package app

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/admin-dashboard/internal/session"
)

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
		rotated, ok, err := r.sessions.Rotate(c.Request.Context(), sessionID)
		if err != nil {
			r.logger.Error("session rotate failed", slog.Any("error", err))
			httpx.Abort(c, httpx.StoreUnavailable())

			return
		}

		if ok {
			r.writeHeartbeatSession(c, &rotated, true)

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
