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
	"github.com/park285/shared-go/pkg/stringutil"
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
	db          *gorm.DB
	cacheClient cache.Client
	logger      *slog.Logger
	config      Config
}

func NewService(ctx context.Context, db *gorm.DB, cacheClient cache.Client, logger *slog.Logger, config Config) (*Service, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}
	if db == nil {
		return nil, fmt.Errorf("db must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if config.SessionTTL <= 0 {
		config = DefaultConfig()
	}
	if config.BcryptCost < bcrypt.MinCost || config.BcryptCost > bcrypt.MaxCost {
		config.BcryptCost = DefaultBcryptCost
	}

	service := &Service{
		db:          db,
		cacheClient: cacheClient,
		logger:      logger,
		config:      config,
	}

	if config.AutoPrepareSchema {
		if err := service.createTablesIfNotExist(ctx); err != nil {
			return nil, err
		}
	}

	return service, nil
}

func (s *Service) Register(ctx context.Context, email, password, displayName string) (*User, error) {
	email = normalizeEmail(email)
	displayName = normalizeDisplayName(displayName)

	if !validateEmail(email) || !validatePassword(password) || !validateDisplayName(displayName) {
		return nil, newError(CodeInvalidInput, "invalid email/password/displayName", nil)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), s.config.BcryptCost)
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

	if err := s.validateLoginGuards(ctx, email, clientIP); err != nil {
		return nil, nil, err
	}

	user, err := s.findLoginUser(ctx, email)
	if err != nil {
		return nil, nil, err
	}

	if err := s.validateLoginPassword(ctx, email, user.PasswordHash, password); err != nil {
		return nil, nil, err
	}

	s.onLoginSucceeded(ctx, email)

	session, err := s.createSession(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}

	return session, toUser(&user), nil
}

func (s *Service) validateLoginGuards(ctx context.Context, email, clientIP string) error {
	if s.cacheClient == nil {
		return nil
	}

	if err := s.validateLoginRateLimit(ctx, clientIP); err != nil {
		return err
	}
	return s.validateAccountLock(ctx, email)
}

func (s *Service) validateLoginRateLimit(ctx context.Context, clientIP string) error {
	limited, err := s.isLoginRateLimited(ctx, clientIP)
	if err != nil {
		return newError(CodeInternal, "rate limit check failed", err)
	}
	if limited {
		return newError(CodeRateLimited, "rate limited", nil)
	}
	return nil
}

func (s *Service) validateAccountLock(ctx context.Context, email string) error {
	locked, err := s.isAccountLocked(ctx, email)
	if err != nil {
		return newError(CodeInternal, "lock check failed", err)
	}
	if locked {
		return newError(CodeAccountLocked, "account locked", nil)
	}
	return nil
}

func (s *Service) findLoginUser(ctx context.Context, email string) (userModel, error) {
	var user userModel
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err == nil {
		return user, nil
	}
	if stdErrors.Is(err, gorm.ErrRecordNotFound) {
		s.onLoginFailed(ctx, email)
		return user, newError(CodeInvalidCredentials, "invalid credentials", nil)
	}
	return user, newError(CodeInternal, "failed to query user", err)
}

func (s *Service) validateLoginPassword(ctx context.Context, email, passwordHash, password string) error {
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil {
		return nil
	}
	s.onLoginFailed(ctx, email)
	return newError(CodeInvalidCredentials, "invalid credentials", nil)
}

func normalizeDisplayName(name string) string {
	return stringutil.TrimSpace(name)
}
