package httpapi

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/httputil"
	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
	"github.com/kapu/admin-dashboard/internal/session"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (r *API) handleLogin(c *gin.Context) {
	var body loginRequest

	if err := httpx.DecodeJSON(c.Request, &body, 16<<10); err != nil {
		httpx.Abort(c, contract.BadRequest("invalid login payload"))
		r.auditLogin(c, "invalid", http.StatusBadRequest)

		return
	}

	ip := r.clientIP(c.Request)
	subject := r.loginLimiterSubject(body.Username)

	if !r.admitLoginAttempt(c, ip, subject) {
		return
	}

	if !r.acquireLoginHashSlot(c) {
		return
	}

	testAccount, credentialsMatch, err := r.loginCredentialsMatch(c.Request.Context(), body)
	r.releaseLoginHashSlot()

	if err != nil {
		r.logger.Error("test account lookup failed", slog.Any("error", err))
		httpx.Abort(c, contract.StoreUnavailable())

		return
	}

	if !credentialsMatch {
		r.rejectLoginAttempt(c, ip, subject)

		return
	}

	r.completeLogin(c, ip, subject, testAccount)
}

func (r *API) acquireLoginHashSlot(c *gin.Context) bool {
	select {
	case r.loginHashSlots <- struct{}{}:
		return true
	default:
		retry := uint64(1)
		httpx.Abort(c, &contract.AppError{Status: http.StatusTooManyRequests, Body: contract.ErrorResponse{Error: "Too many login attempts", RetryAfter: &retry}})
		r.auditLogin(c, "denied", http.StatusTooManyRequests)

		return false
	}
}

func (r *API) releaseLoginHashSlot() {
	<-r.loginHashSlots
}

func (r *API) admitLoginAttempt(c *gin.Context, ip, subject string) bool {
	localAllowed, localRetryAfter := r.rateLimiter.IsAllowed(ip)

	distributedRetryAfter, err := r.distributedLoginLimiter.Check(c.Request.Context(), ip, subject)
	if err != nil {
		r.logger.Error("distributed login limiter check failed", slog.Any("error", err))
		httpx.Abort(c, contract.StoreUnavailable())

		return false
	}

	if localAllowed && distributedRetryAfter <= 0 {
		return true
	}

	retryAfter := max(localRetryAfter, distributedRetryAfter)
	retry := uint64(max(retryAfter.Seconds(), 1))
	httpx.Abort(c, &contract.AppError{Status: http.StatusTooManyRequests, Body: contract.ErrorResponse{Error: "Too many login attempts", RetryAfter: &retry}})
	r.auditLogin(c, "denied", http.StatusTooManyRequests)

	return false
}

// 사용자명이 틀려도 bcrypt 비교를 건너뛰지 않아야 응답 시간이 사용자명 존재 여부를 흘리지 않는다.
func (r *API) loginCredentialsMatch(ctx context.Context, body loginRequest) (*session.TestAccount, bool, error) {
	usernameOK := httputil.ConstantTimeStringEqual(body.Username, r.cfg.AdminUser)
	hash := r.cfg.AdminPassHash

	var temporary *session.TestAccount

	if !usernameOK && strings.HasPrefix(body.Username, session.TestAccountPrefix) {
		account, found, err := r.sessions.CurrentTestAccount(ctx)
		if err != nil {
			return nil, false, fmt.Errorf("read test account: %w", err)
		}

		if found {
			hash = account.PasswordHash
			usernameOK = httputil.ConstantTimeStringEqual(body.Username, account.Username)
			temporary = &account
		}
	}

	// 존재하지 않는 이름도 같은 bcrypt 경계를 거치며 정상 관리자 실패를 임시 계정으로 재시도하지 않습니다.
	passwordOK := bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Password)) == nil

	return temporary, usernameOK && passwordOK, nil
}

func (r *API) loginLimiterSubject(username string) string {
	if username != r.cfg.AdminUser && strings.HasPrefix(username, session.TestAccountPrefix) {
		// 동시에 한 계정만 발급하므로 공격자가 임의 이름으로 Valkey key를 늘릴 수 없게 고정합니다.
		return "test-account:" + r.cfg.AdminUser
	}

	return r.cfg.AdminUser
}

func (r *API) rejectLoginAttempt(c *gin.Context, ip, subject string) {
	localCount := r.rateLimiter.RecordFailure(ip)

	distributedCount, err := r.distributedLoginLimiter.RecordFailure(c.Request.Context(), ip, subject)
	if err != nil {
		r.logger.Error("distributed login limiter failure record failed", slog.Any("error", err))
		httpx.Abort(c, contract.StoreUnavailable())

		return
	}

	count := max(localCount, distributedCount)
	delay := time.Duration(min(count*500, 3000)) * time.Millisecond

	r.auditLogin(c, "denied", http.StatusUnauthorized)

	if !waitForLoginBackoff(c.Request.Context(), delay) {
		return
	}

	httpx.Abort(c, contract.Unauthorized())
}

func (r *API) completeLogin(c *gin.Context, ip, subject string, temporary *session.TestAccount) {
	if err := r.distributedLoginLimiter.RecordSuccess(c.Request.Context(), ip, subject); err != nil {
		r.logger.Error("distributed login limiter success record failed", slog.Any("error", err))
		httpx.Abort(c, contract.StoreUnavailable())

		return
	}

	r.rateLimiter.RecordSuccess(ip)

	var (
		sess session.Session
		err  error
	)

	if temporary == nil {
		sess, err = r.sessions.Create(c.Request.Context())
	} else {
		var found bool

		sess, found, err = r.sessions.CreateTestSession(c.Request.Context(), *temporary)

		if err == nil && !found {
			httpx.Abort(c, contract.Unauthorized())
			r.auditLogin(c, "denied", http.StatusUnauthorized)

			return
		}
	}

	if err != nil {
		r.logger.Error("session create failed", slog.Any("error", err))
		httpx.Abort(c, contract.StoreUnavailable())

		return
	}

	csrf, err := auth.NewCSRFToken(sess.ID, r.cfg.SessionSecret)
	if err != nil {
		httpx.Abort(c, contract.Internal(err))

		return
	}

	maxAge := r.cfg.Session.ExpiryDuration

	if sess.TestAccount != "" {
		maxAge = min(maxAge, time.Until(sess.AbsoluteExpiresAt))
	}

	auth.SetSessionCookie(c.Writer, auth.SignSessionID(sess.ID, r.cfg.SessionSecret), maxAge, r.cfg.Security.ForceHTTPS)
	auth.SetCSRFCookie(c.Writer, csrf, r.cfg.Security.ForceHTTPS)
	httpx.Respond(c, http.StatusOK, loginResponse{Status: "ok", Message: "Login successful", CSRFToken: csrf})
	c.Set(sessionObjKey, &sess)
	r.auditLogin(c, "success", http.StatusOK)
}

func (r *API) handleSessionStatus(c *gin.Context) {
	sessionID, ok := sessionIDFrom(c)
	if !ok {
		httpx.Abort(c, contract.Unauthorized())

		return
	}

	sess, ok := sessionFrom(c)
	if !ok {
		httpx.Abort(c, contract.Unauthorized())

		return
	}

	csrf, reissued, err := r.sessionStatusCSRFToken(c.Request, sessionID)
	if err != nil {
		httpx.Abort(c, contract.Internal(err))

		return
	}

	if reissued {
		auth.SetCSRFCookie(c.Writer, csrf, r.cfg.Security.ForceHTTPS)
	}

	httpx.Respond(c, http.StatusOK, sessionStatusResponse{
		Status:            "ok",
		Authenticated:     true,
		Username:          r.sessionUsername(sess),
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
// 여기서 marker에 바인딩된 값을 다시 쓰면 그걸 덮어써 이후 변경 요청이 전부 403이 된다.
func (r *API) sessionStatusCSRFToken(req *http.Request, sessionID string) (token string, reissued bool, err error) {
	if cookie, cookieErr := req.Cookie(auth.CSRFCookieName); cookieErr == nil &&
		auth.ValidateCSRFToken(sessionID, cookie.Value, r.cfg.SessionSecret) {
		return cookie.Value, false, nil
	}

	token, err = auth.NewCSRFToken(sessionID, r.cfg.SessionSecret)
	if err != nil {
		return "", false, fmt.Errorf("CSRF token: %w", err)
	}

	return token, true, nil
}

func (r *API) handleLogout(c *gin.Context) {
	auth.ClearAuthCookies(c.Writer, r.cfg.Security.ForceHTTPS)

	sess, ok := sessionFrom(c)
	if !ok || sess == nil {
		httpx.Abort(c, contract.StoreUnavailable())

		return
	}

	if err := r.sessions.RevokeFamily(c.Request.Context(), sess.FamilyID); err != nil {
		r.logger.Warn("session family revocation failed during logout", slog.Any("error", err))
		httpx.Abort(c, contract.StoreUnavailable())

		return
	}

	httpx.Respond(c, http.StatusOK, statusResponse{Status: "ok"})
}

const maxHeartbeatBodyBytes int64 = 1024

type heartbeatRequest struct {
	Idle bool `json:"idle"`
}

type heartbeatPayload struct {
	Idle jsontext.Value `json:"idle"`
}

func (r *API) handleHeartbeat(c *gin.Context) {
	sessionID, ok := sessionIDFrom(c)
	if !ok {
		httpx.Abort(c, contract.Unauthorized())

		return
	}

	hb, err := parseHeartbeat(c.Request)
	if err != nil {
		httpx.Abort(c, contract.BadRequest("Invalid heartbeat payload"))

		return
	}

	result, err := r.sessions.Refresh(c.Request.Context(), sessionID, hb.Idle)
	if err != nil {
		r.logger.Error("session refresh failed", slog.Any("error", err))
		httpx.Abort(c, contract.StoreUnavailable())

		return
	}

	r.writeHeartbeatResult(c, sessionID, result)
}

func parseHeartbeat(req *http.Request) (heartbeatRequest, error) {
	body, err := readHeartbeatBody(req)
	if err != nil {
		return heartbeatRequest{}, fmt.Errorf("read heartbeat body: %w", err)
	}

	hb := heartbeatRequest{}

	if len(bytes.TrimSpace(body)) == 0 {
		return hb, nil
	}

	var payload *heartbeatPayload

	if err := httpx.DecodeJSONBytes(body, &payload); err != nil {
		return hb, fmt.Errorf("decode JSON bytes: %w", err)
	}

	if payload == nil {
		return hb, errors.New("heartbeat body must be a json object")
	}

	if len(payload.Idle) == 0 {
		return hb, nil
	}

	if bytes.Equal(bytes.TrimSpace(payload.Idle), []byte("null")) {
		return hb, errors.New("heartbeat idle must be a boolean")
	}

	if err := jsonv2.Unmarshal(payload.Idle, &hb.Idle); err != nil {
		return hb, fmt.Errorf("decode heartbeat idle: %w", err)
	}

	return hb, nil
}

func readHeartbeatBody(req *http.Request) ([]byte, error) {
	body, err := httputil.ReadAllAndCloseWithDrainLimit(req.Body, maxHeartbeatBodyBytes, 64<<10)
	if err != nil {
		if errors.Is(err, httputil.ErrResponseBodyTooLarge) {
			return nil, fmt.Errorf("heartbeat body exceeds %d bytes", maxHeartbeatBodyBytes)
		}

		return nil, fmt.Errorf("read all and close: %w", err)
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

func (r *API) writeHeartbeatResult(c *gin.Context, sessionID string, result session.RefreshResult) {
	if result.Kind == session.RefreshRefreshed {
		r.heartbeatRefreshed(c, sessionID, result)

		return
	}

	r.writeTerminalHeartbeatResult(c, result)
}

func (r *API) writeTerminalHeartbeatResult(c *gin.Context, result session.RefreshResult) {
	if r.writeRotatedHeartbeatResult(c, result) {
		return
	}

	if r.writeIdleHeartbeatResult(c, result) {
		return
	}

	r.heartbeatDenied(c, result.Kind)
}

func (r *API) writeRotatedHeartbeatResult(c *gin.Context, result session.RefreshResult) bool {
	if result.Kind != session.RefreshRotated {
		return false
	}

	r.writeHeartbeatSession(c, result.Session, true)

	return true
}

func (r *API) writeIdleHeartbeatResult(c *gin.Context, result session.RefreshResult) bool {
	if result.Kind != session.RefreshIdleShortened {
		return false
	}

	httpx.Respond(c, http.StatusOK, heartbeatIdleResponse{Status: "idle", IdleRejected: true})

	return true
}

func (r *API) heartbeatRefreshed(c *gin.Context, sessionID string, result session.RefreshResult) {
	if r.cfg.Session.TokenRotationEnabled {
		rotated, ok, err := r.sessions.Rotate(c.Request.Context(), sessionID)
		if err != nil {
			r.logger.Error("session rotate failed", slog.Any("error", err))
			httpx.Abort(c, contract.StoreUnavailable())

			return
		}

		if ok {
			r.writeHeartbeatSession(c, &rotated, true)

			return
		}
	}

	maxAge := max(time.Until(result.Session.ExpiresAt), time.Second)
	auth.SetSessionCookie(c.Writer, auth.SignSessionID(sessionID, r.cfg.SessionSecret), maxAge, r.cfg.Security.ForceHTTPS)
	httpx.Respond(c, http.StatusOK, heartbeatOKResponse{Status: "ok", AbsoluteExpiresAt: result.Session.AbsoluteExpiresAt.Unix()})
}

func (r *API) heartbeatDenied(c *gin.Context, kind session.RefreshKind) {
	auth.ClearAuthCookies(c.Writer, r.cfg.Security.ForceHTTPS)

	if kind == session.RefreshAbsoluteExpired {
		absolute := true
		httpx.Abort(c, &contract.AppError{Status: http.StatusUnauthorized, Body: contract.ErrorResponse{Error: "Session expired", AbsoluteExpired: &absolute}})

		return
	}

	httpx.Abort(c, contract.Unauthorized())
}

func (r *API) writeHeartbeatSession(c *gin.Context, sess *session.Session, rotated bool) {
	csrf, err := auth.NewCSRFToken(sess.ID, r.cfg.SessionSecret)
	if err != nil {
		httpx.Abort(c, contract.Internal(err))

		return
	}

	maxAge := max(time.Until(sess.ExpiresAt), time.Second)
	auth.SetSessionCookie(c.Writer, auth.SignSessionID(sess.ID, r.cfg.SessionSecret), maxAge, r.cfg.Security.ForceHTTPS)
	auth.SetCSRFCookie(c.Writer, csrf, r.cfg.Security.ForceHTTPS)
	httpx.Respond(c, http.StatusOK, heartbeatRotatedResponse{Status: "ok", Rotated: rotated, AbsoluteExpiresAt: sess.AbsoluteExpiresAt.Unix(), CSRFToken: csrf})
}

func durationMillis(duration time.Duration) uint64 {
	if duration <= 0 {
		return 0
	}

	return uint64(duration / time.Millisecond)
}
