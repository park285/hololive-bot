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
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func (s *Service) RequestPasswordReset(ctx context.Context, email, clientIP string) (string, error) {
	email = normalizeEmail(email)
	if !validateEmail(email) {
		return "", newError(CodeInvalidInput, "invalid email", nil)
	}

	if err := s.checkPasswordResetRequestRateLimit(ctx, clientIP); err != nil {
		return "", fmt.Errorf("check password reset request rate limit: %w", err)
	}

	user, found, err := s.findPasswordResetUser(ctx, email)
	if err != nil {
		return "", fmt.Errorf("find password reset user: %w", err)
	}

	if !found {
		return "", nil
	}

	if _, execErr := s.db.Exec(ctx, mustSQL("service_password_reset_0052_01.sql"), user.ID); execErr != nil {
		return "", newError(CodeInternal, "failed to clear existing reset tokens", execErr)
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

	if _, err := s.db.Exec(ctx, mustSQL("service_password_reset_0070_02.sql"), model.TokenHash, model.UserID, model.ExpiresAt, model.UsedAt, model.CreatedAt); err != nil {
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
		return fmt.Errorf("find valid password reset token: %w", err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.config.BcryptCost)
	if err != nil {
		return newError(CodeInternal, "password hash failed", err)
	}

	// 비밀번호 갱신과 같은 UPDATE 문장이 session_generation을 +1하므로, 커밋 시점에 기존 세션은 모두
	// Me·Refresh의 세대 비교에서 거부된다. Valkey 세션 키는 사용 시 삭제되거나 TTL로 사라진다.
	if err := s.applyPasswordReset(ctx, tokenHash, string(passwordHash), now); err != nil {
		return fmt.Errorf("apply password reset: %w", err)
	}

	return nil
}

func (s *Service) findValidPasswordResetToken(ctx context.Context, tokenHash string, now time.Time) (passwordResetTokenModel, error) {
	reset, err := scanPasswordResetToken(s.db.QueryRow(ctx, mustSQL("service_password_reset_0130_03.sql"), tokenHash, now))
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
	if err != nil {
		return reset, fmt.Errorf("scan password reset token row: %w", err)
	}

	return reset, nil
}

func (s *Service) applyPasswordReset(
	ctx context.Context,
	tokenHash string,
	passwordHash string,
	now time.Time,
) error {
	err := dbx.InPgxTx(ctx, s.db, func(tx dbx.Tx) error {
		return claimTokenAndUpdatePassword(ctx, tx, tokenHash, passwordHash, now)
	})
	if err != nil {
		if authErr, ok := stdErrors.AsType[*Error](err); ok {
			return authErr
		}

		return newError(CodeInternal, "failed to apply password reset transaction", err)
	}

	return nil
}

// claimTokenAndUpdatePassword는 reset token을 소비하고 비밀번호를 바꾼다. 비밀번호 UPDATE 문장이
// session_generation도 +1해 같은 트랜잭션 커밋으로 기존 세션 폐기가 확정된다.
func claimTokenAndUpdatePassword(ctx context.Context, tx dbx.Tx, tokenHash, passwordHash string, now time.Time) error {
	var claimedUserID string

	if err := tx.QueryRow(ctx, mustSQL("service_password_reset_0177_04.sql"), now, tokenHash).Scan(&claimedUserID); err != nil {
		if stdErrors.Is(err, pgx.ErrNoRows) {
			return newError(CodeInvalidInput, "invalid reset token", nil)
		}

		return newError(CodeInternal, "failed to claim reset token", err)
	}

	if _, err := tx.Exec(ctx, mustSQL("service_password_reset_0189_05.sql"), passwordHash, now, claimedUserID); err != nil {
		return newError(CodeInternal, "failed to update password", err)
	}

	return nil
}
