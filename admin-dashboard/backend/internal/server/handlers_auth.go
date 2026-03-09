// Package server: HTTP 서버 및 라우팅
package server

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/auth"
	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/middleware"
)

// ===== Auth Handlers =====

// handleLogin godoc
// @Summary      User login
// @Description  Authenticate with username and password. Returns session cookie on success.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      LoginRequest  true  "Login credentials"
// @Success      200      {object}  LoginResponse
// @Failure      400      {object}  ErrorResponse  "Invalid request body"
// @Failure      429      {object}  ErrorResponse  "Too many login attempts"
// @Router       /auth/login [post]
func (s *Server) handleLogin(c *gin.Context) {
	ip := c.ClientIP()

	allowed, remaining := s.rateLimiter.IsAllowed(ip)
	if !allowed {
		s.logger.Warn("Login rate limited", slog.String("ip", ip))
		c.Header("Retry-After", strconv.Itoa(int(remaining.Seconds())))
		c.JSON(429, gin.H{"error": "Too many login attempts", "retry_after": remaining.Seconds()})
		return
	}

	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "Invalid request"})
		return
	}

	if req.Username != s.cfg.AdminUser {
		s.handleLoginFailure(c, ip, req.Username, "invalid_username")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(s.cfg.AdminPassHash), []byte(req.Password)); err != nil {
		s.handleLoginFailure(c, ip, req.Username, "invalid_password")
		return
	}

	s.rateLimiter.RecordSuccess(ip)

	session, err := s.sessions.CreateSession(c.Request.Context())
	if err != nil {
		s.logger.Error("Failed to create session", slog.Any("error", err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Session store unavailable"})
		return
	}

	signedSessionID := auth.SignSessionID(session.ID, s.cfg.AdminSecretKey)
	auth.SetSecureCookie(c, auth.SessionCookieName, signedSessionID, 0, s.cfg.ForceHTTPS)

	csrfToken := middleware.NewCSRFToken(session.ID, s.cfg.AdminSecretKey)
	if csrfToken == "" {
		// CSRF 토큰을 만들 수 없다면 로그인 플로우를 실패로 처리한다.
		// (POST 라우트가 CSRF를 요구하므로, 부분 성공은 UX를 악화시킴)
		s.sessions.DeleteSession(c.Request.Context(), session.ID)
		auth.ClearSecureCookie(c, auth.SessionCookieName, s.cfg.ForceHTTPS)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "CSRF token generation failed"})
		return
	}
	middleware.SetCSRFCookie(c, csrfToken, s.cfg.ForceHTTPS)

	s.logger.Info("Admin logged in", slog.String("username", req.Username), slog.String("ip", ip))
	c.JSON(200, gin.H{"status": "ok", "message": "Login successful", "csrf_token": csrfToken})
}

func (s *Server) handleLoginFailure(c *gin.Context, ip, username, reason string) {
	failCount := s.rateLimiter.RecordFailure(ip)

	s.logger.Warn("Failed login attempt",
		slog.String("username", username),
		slog.String("ip", ip),
		slog.String("reason", reason),
		slog.Int("fail_count", failCount),
	)

	delay := min(time.Duration(failCount)*500*time.Millisecond, 3*time.Second)
	time.Sleep(delay)

	c.JSON(200, gin.H{"success": false, "error": "Authentication failed"})
}

// handleLogout godoc
// @Summary      User logout
// @Description  Invalidate session and clear cookies
// @Tags         auth
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Success      200  {object}  StatusResponse
// @Router       /auth/logout [post]
func (s *Server) handleLogout(c *gin.Context) {
	signedSessionID, _ := c.Cookie(auth.SessionCookieName)
	if signedSessionID != "" {
		if sessionID, valid := auth.ValidateSessionSignature(signedSessionID, s.cfg.AdminSecretKey); valid {
			s.sessions.DeleteSession(c.Request.Context(), sessionID)
		}
	}

	auth.ClearSecureCookie(c, auth.SessionCookieName, s.cfg.ForceHTTPS)
	middleware.ClearCSRFCookie(c, s.cfg.ForceHTTPS)
	c.JSON(200, gin.H{"status": "ok", "message": "Logout successful"})
}

// handleHeartbeat godoc
// @Summary      Session heartbeat
// @Description  Keep session alive and optionally rotate session token
// @Tags         auth
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Param        request  body      HeartbeatRequest  false  "Heartbeat options"
// @Success      200      {object}  HeartbeatResponse
// @Failure      401      {object}  ErrorResponse  "Session expired or invalid"
// @Router       /auth/heartbeat [post]
func (s *Server) handleHeartbeat(c *gin.Context) {
	signedSessionID, err := c.Cookie(auth.SessionCookieName)
	if err != nil || signedSessionID == "" {
		c.JSON(401, gin.H{"error": "Unauthorized"})
		return
	}

	sessionID, valid := auth.ValidateSessionSignature(signedSessionID, s.cfg.AdminSecretKey)
	if !valid {
		c.JSON(401, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		Idle bool `json:"idle"`
	}
	_ = c.ShouldBindJSON(&req)

	ctx := c.Request.Context()
	refreshed, absoluteExpired, err := s.sessions.RefreshSessionWithValidation(ctx, sessionID, req.Idle)
	if err != nil {
		c.JSON(500, gin.H{"error": "Internal server error"})
		return
	}

	if absoluteExpired {
		auth.ClearSecureCookie(c, auth.SessionCookieName, s.cfg.ForceHTTPS)
		c.JSON(401, gin.H{"error": "Session expired", "absolute_expired": true})
		return
	}

	if req.Idle && !refreshed {
		c.JSON(200, gin.H{"status": "idle", "idle_rejected": true})
		return
	}

	if !refreshed {
		auth.ClearSecureCookie(c, auth.SessionCookieName, s.cfg.ForceHTTPS)
		c.JSON(401, gin.H{"error": "Session expired"})
		return
	}

	response := gin.H{"status": "ok"}

	if s.cfg.SessionTokenRotation {
		newSession, rotateErr := s.sessions.RotateSession(ctx, sessionID)
		if rotateErr == nil {
			csrfToken := middleware.NewCSRFToken(newSession.ID, s.cfg.AdminSecretKey)
			if csrfToken == "" {
				c.JSON(500, gin.H{"error": "CSRF token generation failed"})
				return
			}

			newSignedSessionID := auth.SignSessionID(newSession.ID, s.cfg.AdminSecretKey)
			auth.SetSecureCookie(c, auth.SessionCookieName, newSignedSessionID, 0, s.cfg.ForceHTTPS)
			middleware.SetCSRFCookie(c, csrfToken, s.cfg.ForceHTTPS)
			response["rotated"] = true
			response["absolute_expires_at"] = newSession.AbsoluteExpiresAt.Unix()
			response["csrf_token"] = csrfToken
		}
	}

	c.JSON(200, response)
}
