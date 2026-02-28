package server

import (
	"context"
	stdErrors "errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"

	authsvc "github.com/kapu/hololive-kakao-bot-go/internal/service/auth"
)

// AuthHandler: /api/auth 엔드포인트를 처리하는 핸들러
type AuthHandler struct {
	auth   *authsvc.Service
	logger *slog.Logger
}

// NewAuthHandler: AuthHandler 인스턴스를 생성합니다.
func NewAuthHandler(auth *authsvc.Service, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{auth: auth, logger: logger}
}

type registerRequest struct {
	Email       string `json:"email" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"displayName" binding:"required"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type resetRequest struct {
	Email string `json:"email" binding:"required"`
}

type resetPasswordRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}

func writeAuthError(c *gin.Context, status int, code authsvc.ErrorCode) {
	c.JSON(status, gin.H{
		"success": false,
		"error":   code,
	})
}

func parseBearerToken(c *gin.Context) (string, bool) {
	raw := strings.TrimSpace(c.GetHeader("Authorization"))
	if raw == "" {
		return "", false
	}
	parts := strings.Fields(raw)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}

func mapAuthErrorToHTTP(err error) (status int, code authsvc.ErrorCode) {
	var ae *authsvc.Error
	if !stdErrors.As(err, &ae) {
		return http.StatusInternalServerError, authsvc.CodeInternal
	}

	switch ae.Code {
	case authsvc.CodeInvalidInput:
		return http.StatusBadRequest, ae.Code
	case authsvc.CodeEmailExists:
		return http.StatusConflict, ae.Code
	case authsvc.CodeInvalidCredentials:
		return http.StatusUnauthorized, ae.Code
	case authsvc.CodeAccountLocked:
		return http.StatusForbidden, ae.Code
	case authsvc.CodeRateLimited:
		return http.StatusTooManyRequests, ae.Code
	case authsvc.CodeUnauthorized:
		return http.StatusUnauthorized, ae.Code
	default:
		return http.StatusInternalServerError, authsvc.CodeInternal
	}
}

// Register: POST /api/auth/register
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAuthError(c, http.StatusBadRequest, authsvc.CodeInvalidInput)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	user, err := h.auth.Register(ctx, req.Email, req.Password, req.DisplayName)
	if err != nil {
		status, code := mapAuthErrorToHTTP(err)
		writeAuthError(c, status, code)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"user": gin.H{
			"id":          user.ID,
			"email":       user.Email,
			"displayName": user.DisplayName,
			"createdAt":   user.CreatedAt.UTC().Format(time.RFC3339),
		},
	})
}

// Login: POST /api/auth/login
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAuthError(c, http.StatusBadRequest, authsvc.CodeInvalidInput)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	session, user, err := h.auth.Login(ctx, req.Email, req.Password, c.ClientIP())
	if err != nil {
		status, code := mapAuthErrorToHTTP(err)
		writeAuthError(c, status, code)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"session": gin.H{
			"token":     session.Token,
			"expiresAt": session.ExpiresAt.UTC().Format(time.RFC3339),
		},
		"user": gin.H{
			"id":          user.ID,
			"email":       user.Email,
			"displayName": user.DisplayName,
			"avatarUrl":   user.AvatarURL,
		},
	})
}

// Logout: POST /api/auth/logout
func (h *AuthHandler) Logout(c *gin.Context) {
	token, ok := parseBearerToken(c)
	if !ok {
		writeAuthError(c, http.StatusUnauthorized, authsvc.CodeUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.auth.Logout(ctx, token); err != nil {
		status, code := mapAuthErrorToHTTP(err)
		writeAuthError(c, status, code)
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// Refresh: POST /api/auth/refresh
func (h *AuthHandler) Refresh(c *gin.Context) {
	token, ok := parseBearerToken(c)
	if !ok {
		writeAuthError(c, http.StatusUnauthorized, authsvc.CodeUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	session, err := h.auth.Refresh(ctx, token)
	if err != nil {
		status, code := mapAuthErrorToHTTP(err)
		writeAuthError(c, status, code)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"session": gin.H{
			"token":     session.Token,
			"expiresAt": session.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})
}

// Me: GET /api/auth/me
func (h *AuthHandler) Me(c *gin.Context) {
	token, ok := parseBearerToken(c)
	if !ok {
		writeAuthError(c, http.StatusUnauthorized, authsvc.CodeUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	user, err := h.auth.Me(ctx, token)
	if err != nil {
		status, code := mapAuthErrorToHTTP(err)
		writeAuthError(c, status, code)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"user": gin.H{
			"id":          user.ID,
			"email":       user.Email,
			"displayName": user.DisplayName,
			"avatarUrl":   user.AvatarURL,
			"createdAt":   user.CreatedAt.UTC().Format(time.RFC3339),
		},
	})
}

// ResetRequest: POST /api/auth/password/reset-request
func (h *AuthHandler) ResetRequest(c *gin.Context) {
	var req resetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAuthError(c, http.StatusBadRequest, authsvc.CodeInvalidInput)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if _, err := h.auth.RequestPasswordReset(ctx, req.Email, c.ClientIP()); err != nil {
		status, code := mapAuthErrorToHTTP(err)
		writeAuthError(c, status, code)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "If the email exists, a reset link has been sent.",
	})
}

// ResetPassword: POST /api/auth/password/reset
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAuthError(c, http.StatusBadRequest, authsvc.CodeInvalidInput)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), constants.RequestTimeout.AdminRequest)
	defer cancel()

	if err := h.auth.ResetPassword(ctx, req.Token, req.NewPassword); err != nil {
		status, code := mapAuthErrorToHTTP(err)
		writeAuthError(c, status, code)
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
