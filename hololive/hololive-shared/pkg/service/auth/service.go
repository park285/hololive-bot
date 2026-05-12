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
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	sessionTokenPrefix = "sess_"
	resetTokenPrefix   = "reset_"

	sessionKeyPrefix        = "auth:sess:"
	userSessionsKeyPrefix   = "auth:user_sessions:"
	loginRateLimitKeyPrefix = "auth:rl:login:"
	resetReqRateLimitPrefix = "auth:rl:reset_req:"
	loginFailKeyPrefix      = "auth:login_fail:"
	accountLockKeyPrefix    = "auth:lock:"
)

type Session struct {
	Token     string
	ExpiresAt time.Time
}

type Service struct {
	db       *gorm.DB
	cacheSvc cache.Client
	logger   *slog.Logger
	cfg      Config
}

func NewService(ctx context.Context, db *gorm.DB, cacheSvc cache.Client, logger *slog.Logger, cfg Config) (*Service, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.SessionTTL <= 0 {
		cfg = DefaultConfig()
	}

	svc := &Service{
		db:       db,
		cacheSvc: cacheSvc,
		logger:   logger,
		cfg:      cfg,
	}

	if cfg.AutoPrepareSchema {
		if err := svc.createTablesIfNotExist(ctx); err != nil {
			return nil, err
		}
	}

	return svc, nil
}

func (s *Service) Register(ctx context.Context, email, password, displayName string) (*User, error) {
	email = normalizeEmail(email)
	displayName = normalizeDisplayName(displayName)

	if !validateEmail(email) || !validatePassword(password) || !validateDisplayName(displayName) {
		return nil, newError(CodeInvalidInput, "invalid email/password/displayName", nil)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, newError(CodeInternal, "password hash failed", err)
	}

	now := time.Now().UTC()
	model := &userModel{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: string(passwordHash),
		DisplayName:  displayName,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKeyError(err) {
			return nil, newError(CodeEmailExists, "email already exists", err)
		}
		return nil, newError(CodeInternal, "failed to create user", err)
	}

	return toUser(model), nil
}

func (s *Service) Login(ctx context.Context, email, password, clientIP string) (*Session, *User, error) {
	email = normalizeEmail(email)

	if !validateEmail(email) || password == "" {
		return nil, nil, newError(CodeInvalidInput, "invalid email/password", nil)
	}

	if s.cacheSvc != nil {
		if limited, err := s.isLoginRateLimited(ctx, clientIP); err != nil {
			return nil, nil, newError(CodeInternal, "rate limit check failed", err)
		} else if limited {
			return nil, nil, newError(CodeRateLimited, "rate limited", nil)
		}

		if locked, err := s.isAccountLocked(ctx, email); err != nil {
			return nil, nil, newError(CodeInternal, "lock check failed", err)
		} else if locked {
			return nil, nil, newError(CodeAccountLocked, "account locked", nil)
		}
	}

	var user userModel
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil {
		if stdErrors.Is(err, gorm.ErrRecordNotFound) {
			s.onLoginFailed(ctx, email)
			return nil, nil, newError(CodeInvalidCredentials, "invalid credentials", nil)
		}
		return nil, nil, newError(CodeInternal, "failed to query user", err)
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		s.onLoginFailed(ctx, email)
		return nil, nil, newError(CodeInvalidCredentials, "invalid credentials", nil)
	}

	s.onLoginSucceeded(ctx, email)

	session, err := s.createSession(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}

	return session, toUser(&user), nil
}

func normalizeDisplayName(name string) string {
	return stringutil.TrimSpace(name)
}
