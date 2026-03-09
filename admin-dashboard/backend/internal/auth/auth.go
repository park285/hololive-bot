// Package auth: 관리자 인증 및 세션 관리
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/config"
)

const (
	SessionCookieName = "admin_session"
	sessionKeyPrefix  = "session:admin:"
)

// Session: 관리자 세션 정보
type Session struct {
	ID                string    `json:"id"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
	LastRotatedAt     time.Time `json:"last_rotated_at"`
}

// SessionProvider: 세션 저장소 인터페이스
type SessionProvider interface {
	CreateSession(ctx context.Context) (*Session, error)
	GetSession(ctx context.Context, sessionID string) (*Session, error)
	ValidateSession(ctx context.Context, sessionID string) bool
	DeleteSession(ctx context.Context, sessionID string)
	RefreshSession(ctx context.Context, sessionID string) bool
	RefreshSessionWithValidation(ctx context.Context, sessionID string, idle bool) (refreshed bool, absoluteExpired bool, err error)
	RotateSession(ctx context.Context, oldSessionID string) (*Session, error)
}

// ValkeySessionStore: Valkey 기반 세션 저장소
type ValkeySessionStore struct {
	client valkey.Client
	logger *slog.Logger
	ttl    time.Duration
}

// NewValkeySessionStore: Valkey 세션 저장소 생성
func NewValkeySessionStore(client valkey.Client, logger *slog.Logger) *ValkeySessionStore {
	return &ValkeySessionStore{
		client: client,
		logger: logger,
		ttl:    config.SessionConfig.ExpiryDuration,
	}
}

// CreateSession: 새 세션 생성
func (s *ValkeySessionStore) CreateSession(ctx context.Context) (*Session, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}
	now := time.Now()
	session := &Session{
		ID:                sessionID,
		CreatedAt:         now,
		ExpiresAt:         now.Add(s.ttl),
		AbsoluteExpiresAt: now.Add(config.SessionConfig.AbsoluteTimeout),
	}

	if err := s.storeSession(ctx, session); err != nil {
		return nil, err
	}

	s.logger.Debug("Session created",
		slog.String("session_id", truncateSessionID(sessionID)),
		slog.Duration("ttl", s.ttl),
	)
	return session, nil
}

func (s *ValkeySessionStore) storeSession(ctx context.Context, session *Session) error {
	// NOTE: nil context 방어적 fallback - 호출자가 nil 전달 시 패닉 방지
	if ctx == nil {
		ctx = context.Background()
	}
	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	key := sessionKeyPrefix + session.ID
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	cmd := s.client.B().Set().Key(key).Value(string(data)).ExSeconds(int64(s.ttl.Seconds())).Build()
	if err := s.client.Do(storeCtx, cmd).Error(); err != nil {
		s.logger.Error("Failed to store session",
			slog.String("session_id", truncateSessionID(session.ID)),
			slog.Any("error", err),
		)
		return err
	}
	return nil
}

func (s *ValkeySessionStore) expireSession(ctx context.Context, sessionID string, ttl time.Duration) error {
	// NOTE: nil context 방어적 fallback
	if ctx == nil {
		ctx = context.Background()
	}
	expireCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	key := sessionKeyPrefix + sessionID
	resp := s.client.Do(expireCtx, s.client.B().Expire().Key(key).Seconds(int64(ttl.Seconds())).Build())
	return resp.Error()
}

// GetSession: 세션 조회
func (s *ValkeySessionStore) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	// NOTE: nil context 방어적 fallback
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	key := sessionKeyPrefix + sessionID
	resp := s.client.Do(ctx, s.client.B().Get().Key(key).Build())
	if isValkeyNil(resp.Error()) {
		return nil, nil
	}
	if resp.Error() != nil {
		return nil, resp.Error()
	}

	data, err := resp.ToString()
	if err != nil {
		return nil, err
	}

	var session Session
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &session, nil
}

// ValidateSession: 세션 유효성 검증
func (s *ValkeySessionStore) ValidateSession(ctx context.Context, sessionID string) bool {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		return false
	}
	if time.Now().After(session.AbsoluteExpiresAt) {
		s.DeleteSession(ctx, sessionID)
		return false
	}
	return true
}

// DeleteSession: 세션 삭제
func (s *ValkeySessionStore) DeleteSession(ctx context.Context, sessionID string) {
	// NOTE: nil context 방어적 fallback - DeleteSession은 cleanup 등 다양한 컨텍스트에서 호출됨
	if ctx == nil {
		ctx = context.Background()
	}
	deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()

	key := sessionKeyPrefix + sessionID
	if err := s.client.Do(deleteCtx, s.client.B().Del().Key(key).Build()).Error(); err != nil {
		s.logger.Error("Failed to delete session", slog.String("session_id", truncateSessionID(sessionID)), slog.Any("error", err))
	}
}

// RefreshSession: TTL 갱신 (하위 호환성)
func (s *ValkeySessionStore) RefreshSession(ctx context.Context, sessionID string) bool {
	refreshed, _, _ := s.RefreshSessionWithValidation(ctx, sessionID, false)
	return refreshed
}

// RefreshSessionWithValidation: idle 검증 포함 TTL 갱신
func (s *ValkeySessionStore) RefreshSessionWithValidation(ctx context.Context, sessionID string, idle bool) (bool, bool, error) {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return false, false, err
	}
	if session == nil {
		return false, false, nil
	}

	if time.Now().After(session.AbsoluteExpiresAt) {
		s.DeleteSession(ctx, sessionID)
		return false, true, nil
	}

	if idle {
		idleTTL := config.SessionConfig.IdleSessionTTL
		_ = s.expireSession(ctx, sessionID, idleTTL)
		return false, false, nil
	}

	if err := s.expireSession(ctx, sessionID, s.ttl); err != nil {
		return false, false, err
	}
	return true, false, nil
}

// RotateSession: 세션 ID 교체 (토큰 갱신)
func (s *ValkeySessionStore) RotateSession(ctx context.Context, oldSessionID string) (*Session, error) {
	oldSession, err := s.GetSession(ctx, oldSessionID)
	if err != nil {
		return nil, err
	}
	if oldSession == nil {
		return nil, fmt.Errorf("session not found")
	}

	rotationInterval := config.SessionConfig.RotationInterval
	if !oldSession.LastRotatedAt.IsZero() && time.Since(oldSession.LastRotatedAt) < rotationInterval {
		return oldSession, nil
	}

	if time.Now().After(oldSession.AbsoluteExpiresAt) {
		s.DeleteSession(ctx, oldSessionID)
		return nil, fmt.Errorf("session absolute timeout exceeded")
	}

	newSessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}
	now := time.Now()
	newSession := &Session{
		ID:                newSessionID,
		CreatedAt:         oldSession.CreatedAt,
		ExpiresAt:         now.Add(s.ttl),
		AbsoluteExpiresAt: oldSession.AbsoluteExpiresAt,
		LastRotatedAt:     now,
	}

	if err := s.storeSession(ctx, newSession); err != nil {
		return nil, err
	}

	gracePeriod := config.SessionConfig.GracePeriod
	_ = s.expireSession(ctx, oldSessionID, gracePeriod)

	s.logger.Info("Session rotated",
		slog.String("old_session_id", truncateSessionID(oldSessionID)),
		slog.String("new_session_id", truncateSessionID(newSessionID)),
	)
	return newSession, nil
}

// ===== Security Utilities =====

// SignSessionID: HMAC 서명 추가
func SignSessionID(sessionID, secret string) string {
	if secret == "" {
		return sessionID
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(sessionID))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return sessionID + "." + signature
}

// ValidateSessionSignature: HMAC 서명 검증
func ValidateSessionSignature(fullID, secret string) (string, bool) {
	if secret == "" {
		return fullID, true
	}
	parts := strings.SplitN(fullID, ".", 2)
	if len(parts) != 2 {
		return "", false
	}

	sessionID, providedSig := parts[0], parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(sessionID))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return "", false
	}
	return sessionID, true
}

// SetSecureCookie: 보안 쿠키 설정
func SetSecureCookie(c *gin.Context, name, value string, maxAge int, forceHTTPS bool) {
	isSecure := c.Request.TLS != nil || forceHTTPS
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(name, value, maxAge, "/", "", isSecure, true)
}

// ClearSecureCookie: 쿠키 삭제
func ClearSecureCookie(c *gin.Context, name string, forceHTTPS bool) {
	isSecure := c.Request.TLS != nil || forceHTTPS
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(name, "", -1, "/", "", isSecure, true)
}

// SecurityHeadersMiddleware: 보안 헤더 추가
func SecurityHeadersMiddleware(forceHTTPS bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		if c.Request.TLS != nil || forceHTTPS {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Header("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob: https://wsrv.nl https://i.ytimg.com https://*.ytimg.com; "+
				"connect-src 'self' ws: wss:; "+
				"font-src 'self'; "+
				"object-src 'none'; "+
				"base-uri 'self'; "+
				"frame-ancestors 'none'")
		// CSP Report-Only for monitoring (P2-1 Phase 1)
		c.Header("Content-Security-Policy-Report-Only",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob: https://wsrv.nl https://i.ytimg.com https://*.ytimg.com; "+
				"connect-src 'self' ws: wss:; "+
				"font-src 'self'; "+
				"object-src 'none'; "+
				"base-uri 'self'; "+
				"frame-ancestors 'none'")
		c.Next()
	}
}

// AuthMiddleware: 인증 미들웨어
func AuthMiddleware(sessions SessionProvider, sessionSecret string, forceHTTPS bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		signedSessionID, err := c.Cookie(SessionCookieName)
		if err != nil || signedSessionID == "" {
			slog.Warn("auth_failed_no_cookie", slog.String("path", c.Request.URL.Path), slog.Any("err", err))
			c.JSON(401, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		sessionID, valid := ValidateSessionSignature(signedSessionID, sessionSecret)
		if !valid {
			slog.Warn("auth_failed_invalid_signature",
				slog.String("path", c.Request.URL.Path),
				slog.String("session_prefix", truncateSessionID(signedSessionID)),
			)
			c.JSON(401, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		if !sessions.ValidateSession(c.Request.Context(), sessionID) {
			slog.Warn("auth_failed_session_invalid",
				slog.String("path", c.Request.URL.Path),
				slog.String("session_id", truncateSessionID(sessionID)),
			)
			ClearSecureCookie(c, SessionCookieName, forceHTTPS)
			c.JSON(401, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ===== Rate Limiter =====

// LoginRateLimiter: 로그인 시도 횟수 제한
type LoginRateLimiter struct {
	attempts    map[string]*attemptInfo
	mu          sync.RWMutex
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
}

type attemptInfo struct {
	count        int
	firstAttempt time.Time
	lockedUntil  time.Time
}

// NewLoginRateLimiter: Rate Limiter 생성
func NewLoginRateLimiter() *LoginRateLimiter {
	rl := &LoginRateLimiter{
		attempts:    make(map[string]*attemptInfo),
		maxAttempts: 5,
		window:      5 * time.Minute,
		lockout:     15 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

// IsAllowed: 로그인 시도 허용 여부
func (l *LoginRateLimiter) IsAllowed(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	info, exists := l.attempts[ip]
	now := time.Now()

	if !exists {
		l.attempts[ip] = &attemptInfo{count: 0, firstAttempt: now}
		return true, 0
	}

	if now.Before(info.lockedUntil) {
		return false, info.lockedUntil.Sub(now)
	}

	if now.Sub(info.firstAttempt) > l.window {
		info.count = 0
		info.firstAttempt = now
		info.lockedUntil = time.Time{}
	}

	return info.count < l.maxAttempts, 0
}

// RecordFailure: 실패 기록
func (l *LoginRateLimiter) RecordFailure(ip string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	info, exists := l.attempts[ip]
	if !exists {
		info = &attemptInfo{count: 0, firstAttempt: time.Now()}
		l.attempts[ip] = info
	}

	info.count++
	if info.count >= l.maxAttempts {
		info.lockedUntil = time.Now().Add(l.lockout)
	}
	return info.count
}

// RecordSuccess: 성공 시 초기화
func (l *LoginRateLimiter) RecordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

func (l *LoginRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		l.cleanup()
	}
}

func (l *LoginRateLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for ip, info := range l.attempts {
		if now.Sub(info.firstAttempt) > l.window+l.lockout {
			delete(l.attempts, ip)
		}
	}
}

// ===== Helpers =====

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("read random bytes for session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func truncateSessionID(sessionID string) string {
	if len(sessionID) <= 8 {
		return sessionID
	}
	return sessionID[:8] + "..."
}

func isValkeyNil(err error) bool {
	return errors.Is(err, valkey.Nil)
}
