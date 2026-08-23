package app

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	jsonv2 "encoding/json/v2"
	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"
	"github.com/park285/shared-go/v2/pkg/httputil"
	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/admin-dashboard/internal/session"
	"github.com/kapu/hololive-shared/pkg/httpbody"
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
	if !r.admitLoginAttempt(c, ip) {
		return
	}
	if !r.loginCredentialsMatch(body) {
		r.rejectLoginAttempt(c, ip)
		return
	}
	r.completeLogin(c, ip)
}

func (r *Runtime) admitLoginAttempt(c *gin.Context, ip string) bool {
	localAllowed, localRetryAfter := r.rateLimiter.IsAllowed(ip)
	distributedRetryAfter, err := r.distributedLoginLimiter.Check(c.Request.Context(), ip, r.cfg.AdminUser)
	if err != nil {
		r.logger.Error("distributed login limiter check failed", slog.Any("error", err))
		httpx.Abort(c, httpx.StoreUnavailable())
		return false
	}
	if localAllowed && distributedRetryAfter <= 0 {
		return true
	}
	retryAfter := max(localRetryAfter, distributedRetryAfter)
	retry := uint64(max(retryAfter.Seconds(), 1))
	httpx.Abort(c, &httpx.AppError{Status: http.StatusTooManyRequests, Body: httpx.ErrorResponse{Error: "Too many login attempts", RetryAfter: &retry}})

	return false
}

// 사용자명이 틀려도 bcrypt 비교를 건너뛰지 않아야 응답 시간이 사용자명 존재 여부를 흘리지 않는다.
func (r *Runtime) loginCredentialsMatch(body loginRequest) bool {
	usernameOK := httputil.ConstantTimeStringEqual(body.Username, r.cfg.AdminUser)
	passwordOK := bcrypt.CompareHashAndPassword([]byte(r.cfg.AdminPassHash), []byte(body.Password)) == nil

	return usernameOK && passwordOK
}

func (r *Runtime) rejectLoginAttempt(c *gin.Context, ip string) {
	localCount := r.rateLimiter.RecordFailure(ip)
	distributedCount, err := r.distributedLoginLimiter.RecordFailure(c.Request.Context(), ip, r.cfg.AdminUser)
	if err != nil {
		r.logger.Error("distributed login limiter failure record failed", slog.Any("error", err))
		httpx.Abort(c, httpx.StoreUnavailable())
		return
	}
	count := max(localCount, distributedCount)
	delay := time.Duration(min(count*500, 3000)) * time.Millisecond
	if !waitForLoginBackoff(c.Request.Context(), delay) {
		return
	}
	httpx.Abort(c, httpx.Unauthorized())
}

func (r *Runtime) completeLogin(c *gin.Context, ip string) {
	if err := r.distributedLoginLimiter.RecordSuccess(c.Request.Context(), ip, r.cfg.AdminUser); err != nil {
		r.logger.Error("distributed login limiter success record failed", slog.Any("error", err))
		httpx.Abort(c, httpx.StoreUnavailable())
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
	csrf, reissued, err := r.sessionStatusCSRFToken(c.Request, sessionID)
	if err != nil {
		httpx.Abort(c, httpx.Internal(err))
		return
	}
	if reissued {
		auth.SetCSRFCookie(c.Writer, csrf, r.cfg.Security.ForceHTTPS)
	}
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

// 회전 유예 중에는 동시 heartbeat가 교체 세션에 바인딩된 토큰을 방금 심었을 수 있다.
// marker에 바인딩된 값을 다시 쓰면 그걸 덮어써 이후 변경 요청이 전부 403이 된다.
func (r *Runtime) sessionStatusCSRFToken(req *http.Request, sessionID string) (token string, reissued bool, err error) {
	if cookie, cookieErr := req.Cookie(auth.CSRFCookieName); cookieErr == nil &&
		auth.ValidateCSRFToken(sessionID, cookie.Value, r.cfg.SessionSecret) {
		return cookie.Value, false, nil
	}
	token, err = auth.NewCSRFToken(sessionID, r.cfg.SessionSecret)
	if err != nil {
		return "", false, err
	}
	return token, true, nil
}

func (r *Runtime) handleLogout(c *gin.Context) {
	if sessionID, ok := sessionIDFrom(c); ok {
		// 회전 유예 중에는 sessionID가 marker이고 실세션은 교체본이다 — 교체본을 먼저
		// 회수해야 marker만 지워진 채 세션이 최대 8시간 살아남는 일이 없다.
		if sess, ok := sessionFrom(c); ok && sess != nil && sess.ID != sessionID {
			r.deleteSessionForLogout(c, sess.ID)
		}
		r.deleteSessionForLogout(c, sessionID)
	}
	auth.ClearAuthCookies(c.Writer, r.cfg.Security.ForceHTTPS)
	ginjson.Respond(c, http.StatusOK, statusResponse{Status: "ok"})
}

func (r *Runtime) deleteSessionForLogout(c *gin.Context, sessionID string) {
	if err := r.sessions.Delete(c.Request.Context(), sessionID); err != nil {
		r.logger.Warn("session delete failed during logout", slog.Any("error", err))
	}
}

const maxHeartbeatBodyBytes int64 = 1024

type heartbeatRequest struct {
	Idle bool `json:"idle"`
}

type heartbeatPayload struct {
	Idle jsontext.Value `json:"idle"`
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
	body, err := readHeartbeatBody(req)
	if err != nil {
		return heartbeatRequest{}, err
	}

	hb := heartbeatRequest{}
	if len(bytes.TrimSpace(body)) == 0 {
		return hb, nil
	}

	var payload *heartbeatPayload
	if err := httpx.DecodeJSONBytes(body, &payload); err != nil {
		return hb, err
	}
	if payload == nil {
		return hb, fmt.Errorf("heartbeat body must be a json object")
	}
	if len(payload.Idle) == 0 {
		return hb, nil
	}
	if bytes.Equal(bytes.TrimSpace(payload.Idle), []byte("null")) {
		return hb, fmt.Errorf("heartbeat idle must be a boolean")
	}
	if err := jsonv2.Unmarshal(payload.Idle, &hb.Idle); err != nil {
		return hb, fmt.Errorf("decode heartbeat idle: %w", err)
	}
	return hb, nil
}

func readHeartbeatBody(req *http.Request) ([]byte, error) {
	body, err := httpbody.ReadAllAndClose(req.Body, maxHeartbeatBodyBytes)
	if err != nil {
		if errors.Is(err, httpbody.ErrTooLarge) {
			return nil, fmt.Errorf("heartbeat body exceeds %d bytes", maxHeartbeatBodyBytes)
		}
		return nil, err
	}
	return body, nil
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
