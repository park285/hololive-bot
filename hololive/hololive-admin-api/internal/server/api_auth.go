// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
	authsvc "github.com/kapu/hololive-shared/pkg/service/auth"
)

type AuthHandler struct {
	auth   *authsvc.Service
	logger *slog.Logger
}

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

var authErrorHTTPStatus = map[authsvc.ErrorCode]int{
	authsvc.CodeInvalidInput:       http.StatusBadRequest,
	authsvc.CodeEmailExists:        http.StatusConflict,
	authsvc.CodeInvalidCredentials: http.StatusUnauthorized,
	authsvc.CodeAccountLocked:      http.StatusForbidden,
	authsvc.CodeRateLimited:        http.StatusTooManyRequests,
	authsvc.CodeUnauthorized:       http.StatusUnauthorized,
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

func (h *AuthHandler) requireAuthService(c *gin.Context) bool {
	if h == nil || h.auth == nil {
		writeAuthError(c, http.StatusServiceUnavailable, authsvc.CodeInternal)
		return false
	}

	return true
}

func mapAuthErrorToHTTP(err error) (status int, code authsvc.ErrorCode) {
	var ae *authsvc.Error
	if !stdErrors.As(err, &ae) {
		return http.StatusInternalServerError, authsvc.CodeInternal
	}

	status, ok := authErrorHTTPStatus[ae.Code]
	if !ok {
		return http.StatusInternalServerError, authsvc.CodeInternal
	}
	return status, ae.Code
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAuthError(c, http.StatusBadRequest, authsvc.CodeInvalidInput)
		return
	}
	if !h.requireAuthService(c) {
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

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAuthError(c, http.StatusBadRequest, authsvc.CodeInvalidInput)
		return
	}
	if !h.requireAuthService(c) {
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

func (h *AuthHandler) Logout(c *gin.Context) {
	token, ok := parseBearerToken(c)
	if !ok {
		writeAuthError(c, http.StatusUnauthorized, authsvc.CodeUnauthorized)
		return
	}
	if !h.requireAuthService(c) {
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

func (h *AuthHandler) Refresh(c *gin.Context) {
	token, ok := parseBearerToken(c)
	if !ok {
		writeAuthError(c, http.StatusUnauthorized, authsvc.CodeUnauthorized)
		return
	}
	if !h.requireAuthService(c) {
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

func (h *AuthHandler) Me(c *gin.Context) {
	token, ok := parseBearerToken(c)
	if !ok {
		writeAuthError(c, http.StatusUnauthorized, authsvc.CodeUnauthorized)
		return
	}
	if !h.requireAuthService(c) {
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

func (h *AuthHandler) ResetRequest(c *gin.Context) {
	var req resetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAuthError(c, http.StatusBadRequest, authsvc.CodeInvalidInput)
		return
	}
	if !h.requireAuthService(c) {
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

func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAuthError(c, http.StatusBadRequest, authsvc.CodeInvalidInput)
		return
	}
	if !h.requireAuthService(c) {
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
