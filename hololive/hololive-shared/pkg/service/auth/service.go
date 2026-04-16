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
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
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

const incrWithTTLScript = `
local current = redis.call('INCR', KEYS[1])
if current == 1 and tonumber(ARGV[1]) > 0 then
  redis.call('EXPIRE', KEYS[1], tonumber(ARGV[1]))
end
return current
`

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

func (s *Service) createTablesIfNotExist(ctx context.Context) error {
	db := s.db.WithContext(ctx)

	// auth_users 테이블
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS auth_users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			display_name TEXT NOT NULL,
			avatar_url TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`).Error; err != nil {
		return fmt.Errorf("failed to create auth_users table: %w", err)
	}

	// auth_password_reset_tokens 테이블
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS auth_password_reset_tokens (
			token_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			used_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`).Error; err != nil {
		return fmt.Errorf("failed to create auth_password_reset_tokens table: %w", err)
	}

	return nil
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

func (s *Service) Logout(ctx context.Context, token string) error {
	if s.cacheSvc == nil {
		return newError(CodeInternal, "cache service not configured", nil)
	}

	sessionHash := sha256Hex(token)
	key := sessionKeyPrefix + sessionHash

	var data sessionData
	if err := s.cacheSvc.Get(ctx, key, &data); err != nil {
		return newError(CodeInternal, "failed to read session", err)
	}
	if data.UserID == "" {
		return newError(CodeUnauthorized, "invalid session", nil)
	}

	if err := s.cacheSvc.Del(ctx, key); err != nil {
		return newError(CodeInternal, "failed to delete session", err)
	}
	_, _ = s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+data.UserID, []string{sessionHash})

	return nil
}

func (s *Service) Refresh(ctx context.Context, token string) (*Session, error) {
	if s.cacheSvc == nil {
		return nil, newError(CodeInternal, "cache service not configured", nil)
	}

	sessionHash := sha256Hex(token)
	oldKey := sessionKeyPrefix + sessionHash

	var data sessionData
	if err := s.cacheSvc.Get(ctx, oldKey, &data); err != nil {
		return nil, newError(CodeInternal, "failed to read session", err)
	}
	if data.UserID == "" || time.Now().UTC().After(data.ExpiresAt) {
		_ = s.cacheSvc.Del(ctx, oldKey)
		if data.UserID != "" {
			_, _ = s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+data.UserID, []string{sessionHash})
		}
		return nil, newError(CodeUnauthorized, "invalid session", nil)
	}

	newSession, err := s.createSession(ctx, data.UserID)
	if err != nil {
		return nil, err
	}

	invalidation := s.deleteSessionByHash(ctx, data.UserID, sessionHash)
	if invalidation.keyErr != nil {
		rollbackErr := s.deleteSessionByHash(context.Background(), data.UserID, sha256Hex(newSession.Token))
		if rollbackErr.Err() != nil {
			err = stdErrors.Join(invalidation.keyErr, fmt.Errorf("rollback new session: %w", rollbackErr.Err()))
		} else {
			err = invalidation.keyErr
		}
		return nil, newError(CodeInternal, "failed to invalidate previous session during refresh", err)
	}
	if invalidation.indexErr != nil && s.logger != nil {
		s.logger.Warn(
			"Failed to remove previous session from user index during refresh",
			slog.String("user_id", data.UserID),
			slog.Any("error", invalidation.indexErr),
		)
	}

	return newSession, nil
}

func (s *Service) Me(ctx context.Context, token string) (*User, error) {
	userID, err := s.validateSession(ctx, token)
	if err != nil {
		return nil, err
	}

	var user userModel
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if stdErrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, newError(CodeUnauthorized, "user not found", nil)
		}
		return nil, newError(CodeInternal, "failed to query user", err)
	}

	return toUser(&user), nil
}

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

type sessionData struct {
	UserID    string    `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Service) validateSession(ctx context.Context, token string) (string, error) {
	if s.cacheSvc == nil {
		return "", newError(CodeInternal, "cache service not configured", nil)
	}
	if token == "" {
		return "", newError(CodeUnauthorized, "missing token", nil)
	}

	sessionHash := sha256Hex(token)
	key := sessionKeyPrefix + sessionHash
	var data sessionData
	if err := s.cacheSvc.Get(ctx, key, &data); err != nil {
		return "", newError(CodeInternal, "failed to read session", err)
	}
	if data.UserID == "" || time.Now().UTC().After(data.ExpiresAt) {
		_ = s.cacheSvc.Del(ctx, key)
		if data.UserID != "" {
			_, _ = s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+data.UserID, []string{sessionHash})
		}
		return "", newError(CodeUnauthorized, "invalid session", nil)
	}
	return data.UserID, nil
}

type sessionInvalidationResult struct {
	keyErr   error
	indexErr error
}

func (r sessionInvalidationResult) Err() error {
	return stdErrors.Join(r.keyErr, r.indexErr)
}

func (s *Service) deleteSessionByHash(ctx context.Context, userID, sessionHash string) sessionInvalidationResult {
	if s.cacheSvc == nil {
		return sessionInvalidationResult{
			keyErr: newError(CodeInternal, "cache service not configured", nil),
		}
	}

	key := sessionKeyPrefix + sessionHash
	result := sessionInvalidationResult{}
	if err := s.cacheSvc.Del(ctx, key); err != nil {
		result.keyErr = fmt.Errorf("delete session key: %w", err)
	}
	if _, err := s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+userID, []string{sessionHash}); err != nil {
		result.indexErr = fmt.Errorf("remove session index: %w", err)
	}

	return result
}

func (s *Service) createSession(ctx context.Context, userID string) (*Session, error) {
	if s.cacheSvc == nil {
		return nil, newError(CodeInternal, "cache service not configured", nil)
	}
	if userID == "" {
		return nil, newError(CodeInternal, "userID is empty", nil)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.SessionTTL)
	data := sessionData{
		UserID:    userID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	payload, err := json.Marshal(&data)
	if err != nil {
		return nil, newError(CodeInternal, "failed to marshal session", err)
	}

	var token string
	var sessionHash string

	for range 3 {
		raw, err := generateToken(sessionTokenPrefix, 32)
		if err != nil {
			return nil, newError(CodeInternal, "failed to generate session token", err)
		}
		hash := sha256Hex(raw)
		k := sessionKeyPrefix + hash

		acquired, err := s.cacheSvc.SetNX(ctx, k, string(payload), s.cfg.SessionTTL)
		if err != nil {
			return nil, newError(CodeInternal, "failed to store session", err)
		}
		if acquired {
			token = raw
			sessionHash = hash
			break
		}
	}
	if token == "" {
		return nil, newError(CodeInternal, "failed to allocate unique session token", nil)
	}

	// 유저별 세션 인덱스 유지 (비밀번호 변경 시 전체 폐기 용도)
	userSessionsKey := userSessionsKeyPrefix + userID
	if _, err := s.cacheSvc.SAdd(ctx, userSessionsKey, []string{sessionHash}); err != nil {
		_ = s.cacheSvc.Del(ctx, sessionKeyPrefix+sessionHash)
		return nil, newError(CodeInternal, "failed to update session index", err)
	}
	if err := s.cacheSvc.Expire(ctx, userSessionsKey, s.cfg.UserSessionsTTL); err != nil {
		_, _ = s.cacheSvc.SRem(ctx, userSessionsKey, []string{sessionHash})
		_ = s.cacheSvc.Del(ctx, sessionKeyPrefix+sessionHash)
		return nil, newError(CodeInternal, "failed to expire session index", err)
	}

	return &Session{
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) revokeAllSessions(ctx context.Context, userID string) error {
	if s.cacheSvc == nil || userID == "" {
		return nil
	}

	userSessionsKey := userSessionsKeyPrefix + userID
	hashes, err := s.cacheSvc.SMembers(ctx, userSessionsKey)
	if err != nil {
		return fmt.Errorf("cache smembers failed: %w", err)
	}
	if len(hashes) == 0 {
		if err := s.cacheSvc.Del(ctx, userSessionsKey); err != nil {
			return fmt.Errorf("delete user session index: %w", err)
		}
		return nil
	}

	keys := make([]string, 0, len(hashes))
	for _, h := range hashes {
		if h == "" {
			continue
		}
		keys = append(keys, sessionKeyPrefix+h)
	}

	var errs []error
	if _, err := s.cacheSvc.DelMany(ctx, keys); err != nil {
		errs = append(errs, fmt.Errorf("delete session keys: %w", err))
	}
	if err := s.cacheSvc.Del(ctx, userSessionsKey); err != nil {
		errs = append(errs, fmt.Errorf("delete user session index: %w", err))
	}

	return stdErrors.Join(errs...)
}

func (s *Service) isLoginRateLimited(ctx context.Context, clientIP string) (bool, error) {
	if clientIP == "" || s.cacheSvc == nil {
		return false, nil
	}

	key := loginRateLimitKeyPrefix + clientIP
	count, err := incrWithTTL(ctx, s.cacheSvc, key, time.Minute)
	if err != nil {
		return false, err
	}

	return count > s.cfg.LoginRateLimitPerMinute, nil
}

func (s *Service) isPasswordResetRequestRateLimited(ctx context.Context, clientIP string) (bool, error) {
	if clientIP == "" || s.cacheSvc == nil {
		return false, nil
	}

	key := resetReqRateLimitPrefix + clientIP
	count, err := incrWithTTL(ctx, s.cacheSvc, key, time.Minute)
	if err != nil {
		return false, err
	}

	return count > s.cfg.PasswordResetRequestRateLimitPerMinute, nil
}

func (s *Service) isAccountLocked(ctx context.Context, email string) (bool, error) {
	if s.cacheSvc == nil {
		return false, nil
	}
	key := accountLockKeyPrefix + email
	exists, err := s.cacheSvc.Exists(ctx, key)
	if err != nil {
		return false, fmt.Errorf("cache exists failed: %w", err)
	}
	return exists, nil
}

func (s *Service) onLoginFailed(ctx context.Context, email string) {
	if s.cacheSvc == nil {
		return
	}

	key := loginFailKeyPrefix + email
	count, err := incrWithTTL(ctx, s.cacheSvc, key, s.cfg.LoginFailWindow)
	if err != nil {
		s.logger.Warn("login_fail_increment_failed", slog.Any("error", err))
		return
	}

	if count >= s.cfg.LoginFailLimit {
		lockKey := accountLockKeyPrefix + email
		_ = s.cacheSvc.Set(ctx, lockKey, "1", s.cfg.LoginLockDuration)
		_ = s.cacheSvc.Del(ctx, key)
	}
}

func (s *Service) onLoginSucceeded(ctx context.Context, email string) {
	if s.cacheSvc == nil {
		return
	}
	_ = s.cacheSvc.Del(ctx, loginFailKeyPrefix+email)
	_ = s.cacheSvc.Del(ctx, accountLockKeyPrefix+email)
}

func incrWithTTL(ctx context.Context, cacheSvc cache.Client, key string, ttl time.Duration) (int64, error) {
	ttlSeconds := int64(0)
	if ttl > 0 {
		ttlSeconds = int64(math.Ceil(ttl.Seconds()))
		if ttlSeconds <= 0 {
			ttlSeconds = 1
		}
	}

	cmd := cacheSvc.B().
		Eval().
		Script(incrWithTTLScript).
		Numkeys(1).
		Key(key).
		Arg(strconv.FormatInt(ttlSeconds, 10)).
		Build()
	results := cacheSvc.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return 0, fmt.Errorf("increment with ttl: unexpected result count: %d", len(results))
	}
	if results[0].Error() != nil {
		return 0, results[0].Error()
	}
	count, err := results[0].AsInt64()
	if err != nil {
		return 0, err
	}
	return count, nil
}

func normalizeDisplayName(name string) string {
	return stringutil.TrimSpace(name)
}
