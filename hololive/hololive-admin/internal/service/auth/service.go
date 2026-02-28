package auth

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
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

// Session: API 응답용 세션 정보
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// Service: DB(유저) + Valkey(세션/레이트리밋) 기반 인증 서비스
type Service struct {
	db       *gorm.DB
	cacheSvc *cache.Service
	logger   *slog.Logger
	cfg      Config
}

// NewService: 인증 서비스를 생성하고 필요한 테이블을 준비합니다.
func NewService(ctx context.Context, db *gorm.DB, cacheSvc *cache.Service, logger *slog.Logger, cfg Config) (*Service, error) {
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

// Register: 신규 사용자 등록
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

// Login: 로그인 및 세션 토큰 발급
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

// Logout: 세션 무효화
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

// Refresh: 세션 토큰 갱신 (기존 세션을 무효화하고 새 토큰 발급)
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

	// 기존 세션 무효화
	_ = s.cacheSvc.Del(ctx, oldKey)
	_, _ = s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+data.UserID, []string{sessionHash})

	return newSession, nil
}

// Me: 현재 사용자 정보 조회 (세션 검증 포함)
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

// RequestPasswordReset: 비밀번호 재설정 토큰 생성 (이메일 발송은 외부에서 수행)
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

// ResetPassword: 비밀번호 재설정 실행
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

	// 보안: 비밀번호 변경 시 기존 세션 전부 폐기
	_ = s.revokeAllSessions(ctx, reset.UserID)

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

func (s *Service) createSession(ctx context.Context, userID string) (*Session, error) {
	if s.cacheSvc == nil {
		return nil, newError(CodeInternal, "cache service not configured", nil)
	}
	if userID == "" {
		return nil, newError(CodeInternal, "userID is empty", nil)
	}

	var token string
	var sessionHash string
	var key string

	for range 3 {
		raw, err := generateToken(sessionTokenPrefix, 32)
		if err != nil {
			return nil, newError(CodeInternal, "failed to generate session token", err)
		}
		hash := sha256Hex(raw)
		k := sessionKeyPrefix + hash

		exists, err := s.cacheSvc.Exists(ctx, k)
		if err != nil {
			return nil, newError(CodeInternal, "failed to check session existence", err)
		}
		if !exists {
			token = raw
			sessionHash = hash
			key = k
			break
		}
	}
	if token == "" {
		return nil, newError(CodeInternal, "failed to allocate unique session token", nil)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.SessionTTL)
	data := sessionData{
		UserID:    userID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}

	if err := s.cacheSvc.Set(ctx, key, &data, s.cfg.SessionTTL); err != nil {
		return nil, newError(CodeInternal, "failed to store session", err)
	}

	// 유저별 세션 인덱스 유지 (비밀번호 변경 시 전체 폐기 용도)
	userSessionsKey := userSessionsKeyPrefix + userID
	_, _ = s.cacheSvc.SAdd(ctx, userSessionsKey, []string{sessionHash})
	_ = s.cacheSvc.Expire(ctx, userSessionsKey, s.cfg.UserSessionsTTL)

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
		_ = s.cacheSvc.Del(ctx, userSessionsKey)
		return nil
	}

	keys := make([]string, 0, len(hashes))
	for _, h := range hashes {
		if h == "" {
			continue
		}
		keys = append(keys, sessionKeyPrefix+h)
	}

	_, _ = s.cacheSvc.DelMany(ctx, keys)
	_ = s.cacheSvc.Del(ctx, userSessionsKey)

	return nil
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

func incrWithTTL(ctx context.Context, cacheSvc *cache.Service, key string, ttl time.Duration) (int64, error) {
	client := cacheSvc.GetClient()
	resp := client.Do(ctx, client.B().Incr().Key(key).Build())
	if resp.Error() != nil {
		return 0, resp.Error()
	}
	count, err := resp.AsInt64()
	if err != nil {
		return 0, err
	}
	// 최초 생성 시에만 TTL 부여
	if count == 1 && ttl > 0 {
		_ = cacheSvc.Expire(ctx, key, ttl)
	}
	return count, nil
}

func normalizeDisplayName(name string) string {
	return stringutil.TrimSpace(name)
}
