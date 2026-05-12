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

package auth

import (
	"context"
	stdErrors "errors"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (s *Service) RequestPasswordReset(ctx context.Context, email, clientIP string) (string, error) {
	email = normalizeEmail(email)
	if !validateEmail(email) {
		return "", newError(CodeInvalidInput, "invalid email", nil)
	}
	if s.cacheSvc != nil {
		if limited, err := s.isPasswordResetRequestRateLimited(ctx, clientIP); err != nil {
			return "", newError(CodeInternal, "password reset rate limit check failed", err)
		} else if limited {
			return "", newError(CodeRateLimited, "rate limited", nil)
		}
	}

	var user userModel
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil {
		if stdErrors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil // 사용자 존재 여부를 노출하지 않음
		}
		return "", newError(CodeInternal, "failed to query user", err)
	}

	// 이전 토큰 정리 (미사용 토큰만)
	_ = s.db.WithContext(ctx).
		Where("user_id = ? AND used_at IS NULL", user.ID).
		Delete(&passwordResetTokenModel{}).Error

	rawToken, err := generateToken(resetTokenPrefix, 32)
	if err != nil {
		return "", newError(CodeInternal, "failed to generate reset token", err)
	}

	now := time.Now().UTC()
	model := &passwordResetTokenModel{
		TokenHash: sha256Hex(rawToken),
		UserID:    user.ID,
		ExpiresAt: now.Add(s.cfg.ResetTokenTTL),
		UsedAt:    nil,
		CreatedAt: now,
	}

	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		return "", newError(CodeInternal, "failed to create reset token", err)
	}

	return rawToken, nil
}

func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if token == "" || !validatePassword(newPassword) {
		return newError(CodeInvalidInput, "invalid token/password", nil)
	}

	tokenHash := sha256Hex(token)
	now := time.Now().UTC()

	var reset passwordResetTokenModel
	err := s.db.WithContext(ctx).
		Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", tokenHash, now).
		First(&reset).Error
	if err != nil {
		if stdErrors.Is(err, gorm.ErrRecordNotFound) {
			return newError(CodeInvalidInput, "invalid reset token", nil)
		}
		return newError(CodeInternal, "failed to query reset token", err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return newError(CodeInternal, "password hash failed", err)
	}

	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return newError(CodeInternal, "failed to begin transaction", tx.Error)
	}

	if err := tx.Model(&userModel{}).
		Where("id = ?", reset.UserID).
		Update("password_hash", string(passwordHash)).Error; err != nil {
		tx.Rollback()
		return newError(CodeInternal, "failed to update password", err)
	}

	usedAt := now
	if err := tx.Model(&passwordResetTokenModel{}).
		Where("token_hash = ?", reset.TokenHash).
		Update("used_at", &usedAt).Error; err != nil {
		tx.Rollback()
		return newError(CodeInternal, "failed to mark token used", err)
	}

	if err := tx.Commit().Error; err != nil {
		return newError(CodeInternal, "failed to commit transaction", err)
	}

	if err := s.revokeAllSessions(ctx, reset.UserID); err != nil {
		if s.logger != nil {
			s.logger.Warn(
				"Failed to revoke existing sessions after password reset",
				slog.String("user_id", reset.UserID),
				slog.Any("error", err),
			)
		}
	}

	return nil
}
