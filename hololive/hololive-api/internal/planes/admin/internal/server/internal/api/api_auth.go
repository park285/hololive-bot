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

package api

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
	"github.com/park285/shared-go/pkg/ginjson"
)

type AuthHandler struct {
	auth   *authsvc.Service
	logger *slog.Logger
}

type authErrorResponse struct {
	Success bool              `json:"success"`
	Error   authsvc.ErrorCode `json:"error"`
}

type authSession struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

type registerUser struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	CreatedAt   string `json:"createdAt"`
}

type sessionUser struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName string  `json:"displayName"`
	AvatarURL   *string `json:"avatarUrl"`
}

type meUser struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName string  `json:"displayName"`
	AvatarURL   *string `json:"avatarUrl"`
	CreatedAt   string  `json:"createdAt"`
}

type registerResponse struct {
	Success bool         `json:"success"`
	User    registerUser `json:"user"`
}

type loginResponse struct {
	Success bool        `json:"success"`
	Session authSession `json:"session"`
	User    sessionUser `json:"user"`
}

type successResponse struct {
	Success bool `json:"success"`
}

type refreshResponse struct {
	Success bool        `json:"success"`
	Session authSession `json:"session"`
}

type meResponse struct {
	Success bool   `json:"success"`
	User    meUser `json:"user"`
}

type resetRequestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
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
	ginjson.Respond(c, status, authErrorResponse{Success: false, Error: code})
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

	ginjson.Respond(c, http.StatusCreated, registerResponse{
		Success: true,
		User: registerUser{
			ID:          user.ID,
			Email:       user.Email,
			DisplayName: user.DisplayName,
			CreatedAt:   user.CreatedAt.UTC().Format(time.RFC3339),
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

	ginjson.Respond(c, http.StatusOK, loginResponse{
		Success: true,
		Session: authSession{
			Token:     session.Token,
			ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339),
		},
		User: sessionUser{
			ID:          user.ID,
			Email:       user.Email,
			DisplayName: user.DisplayName,
			AvatarURL:   user.AvatarURL,
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

	ginjson.Respond(c, http.StatusOK, successResponse{Success: true})
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

	ginjson.Respond(c, http.StatusOK, refreshResponse{
		Success: true,
		Session: authSession{
			Token:     session.Token,
			ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339),
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

	ginjson.Respond(c, http.StatusOK, meResponse{
		Success: true,
		User: meUser{
			ID:          user.ID,
			Email:       user.Email,
			DisplayName: user.DisplayName,
			AvatarURL:   user.AvatarURL,
			CreatedAt:   user.CreatedAt.UTC().Format(time.RFC3339),
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

	ginjson.Respond(c, http.StatusOK, resetRequestResponse{
		Success: true,
		Message: "If the email exists, a reset link has been sent.",
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

	ginjson.Respond(c, http.StatusOK, successResponse{Success: true})
}
