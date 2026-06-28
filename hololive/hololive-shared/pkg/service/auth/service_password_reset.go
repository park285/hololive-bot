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

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/hololive-shared/internal/dbx"
)

func (s *Service) RequestPasswordReset(ctx context.Context, email, clientIP string) (string, error) {
	email = normalizeEmail(email)
	if !validateEmail(email) {
		return "", newError(CodeInvalidInput, "invalid email", nil)
	}
	if err := s.checkPasswordResetRequestRateLimit(ctx, clientIP); err != nil {
		return "", err
	}

	user, found, err := s.findPasswordResetUser(ctx, email)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}

	if _, err := s.db.Exec(ctx, `DELETE FROM auth_password_reset_tokens WHERE user_id = $1 AND used_at IS NULL`, user.ID); err != nil {
		return "", newError(CodeInternal, "failed to clear existing reset tokens", err)
	}

	rawToken, err := generateToken(resetTokenPrefix, 32)
	if err != nil {
		return "", newError(CodeInternal, "failed to generate reset token", err)
	}

	now := time.Now().UTC()
	model := &passwordResetTokenModel{
		TokenHash: sha256Hex(rawToken),
		UserID:    user.ID,
		ExpiresAt: now.Add(s.config.ResetTokenTTL),
		UsedAt:    nil,
		CreatedAt: now,
	}

	if _, err := s.db.Exec(ctx, `
		INSERT INTO auth_password_reset_tokens (token_hash, user_id, expires_at, used_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, model.TokenHash, model.UserID, model.ExpiresAt, model.UsedAt, model.CreatedAt); err != nil {
		return "", newError(CodeInternal, "failed to create reset token", err)
	}

	return rawToken, nil
}

func (s *Service) checkPasswordResetRequestRateLimit(ctx context.Context, clientIP string) error {
	if s.cacheClient == nil {
		return nil
	}
	limited, err := s.isPasswordResetRequestRateLimited(ctx, clientIP)
	if err != nil {
		return newError(CodeInternal, "password reset rate limit check failed", err)
	}
	if limited {
		return newError(CodeRateLimited, "rate limited", nil)
	}
	return nil
}

func (s *Service) findPasswordResetUser(ctx context.Context, email string) (userModel, bool, error) {
	user, err := s.findUserByEmail(ctx, email)
	if err == nil {
		return user, true, nil
	}
	if stdErrors.Is(err, pgx.ErrNoRows) {
		return userModel{}, false, nil
	}
	return userModel{}, false, newError(CodeInternal, "failed to query user", err)
}

func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if token == "" || !validatePassword(newPassword) {
		return newError(CodeInvalidInput, "invalid token/password", nil)
	}

	now := time.Now().UTC()
	tokenHash := sha256Hex(token)
	if _, err := s.findValidPasswordResetToken(ctx, tokenHash, now); err != nil {
		return err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.config.BcryptCost)
	if err != nil {
		return newError(CodeInternal, "password hash failed", err)
	}

	userID, err := s.applyPasswordReset(ctx, tokenHash, string(passwordHash), now)
	if err != nil {
		return err
	}
	s.warnIfPasswordResetSessionRevokeFails(ctx, userID)
	return nil
}

func (s *Service) findValidPasswordResetToken(ctx context.Context, tokenHash string, now time.Time) (passwordResetTokenModel, error) {
	reset, err := scanPasswordResetToken(s.db.QueryRow(ctx, `
		SELECT token_hash, user_id, expires_at, used_at, created_at
		FROM auth_password_reset_tokens
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2
	`, tokenHash, now))
	if err == nil {
		return reset, nil
	}
	if stdErrors.Is(err, pgx.ErrNoRows) {
		return passwordResetTokenModel{}, newError(CodeInvalidInput, "invalid reset token", nil)
	}
	return passwordResetTokenModel{}, newError(CodeInternal, "failed to query reset token", err)
}

func scanPasswordResetToken(row rowScanner) (passwordResetTokenModel, error) {
	var reset passwordResetTokenModel
	err := row.Scan(
		&reset.TokenHash,
		&reset.UserID,
		&reset.ExpiresAt,
		&reset.UsedAt,
		&reset.CreatedAt,
	)
	return reset, err
}

func (s *Service) applyPasswordReset(
	ctx context.Context,
	tokenHash string,
	passwordHash string,
	now time.Time,
) (string, error) {
	userID, err := dbx.InPgxTxWithResult(ctx, s.db, func(tx dbx.Tx) (string, error) {
		return claimTokenAndUpdatePassword(ctx, tx, tokenHash, passwordHash, now)
	})
	if err != nil {
		var authErr *Error
		if stdErrors.As(err, &authErr) {
			return "", authErr
		}
		return "", newError(CodeInternal, "failed to apply password reset transaction", err)
	}
	return userID, nil
}

func claimTokenAndUpdatePassword(ctx context.Context, tx dbx.Tx, tokenHash, passwordHash string, now time.Time) (string, error) {
	var claimedUserID string
	if err := tx.QueryRow(ctx, `
		UPDATE auth_password_reset_tokens
		SET used_at = $1
		WHERE token_hash = $2 AND used_at IS NULL AND expires_at > $1
		RETURNING user_id
	`, now, tokenHash).Scan(&claimedUserID); err != nil {
		if stdErrors.Is(err, pgx.ErrNoRows) {
			return "", newError(CodeInvalidInput, "invalid reset token", nil)
		}
		return "", newError(CodeInternal, "failed to claim reset token", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE auth_users
		SET password_hash = $1, updated_at = $2
		WHERE id = $3
	`, passwordHash, now, claimedUserID); err != nil {
		return "", newError(CodeInternal, "failed to update password", err)
	}

	return claimedUserID, nil
}

func (s *Service) warnIfPasswordResetSessionRevokeFails(ctx context.Context, userID string) {
	if s.logger == nil {
		return
	}
	if err := s.revokeAllSessions(ctx, userID); err != nil {
		s.logger.Warn(
			"Failed to revoke existing sessions after password reset",
			slog.String("user_id", userID),
			slog.Any("error", err),
		)
	}
}
