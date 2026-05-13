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

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"gorm.io/gorm"
)

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

	sessionHash, data, err := s.refreshSessionData(ctx, token)
	if err != nil {
		return nil, err
	}

	newSession, err := s.createSession(ctx, data.UserID)
	if err != nil {
		return nil, err
	}

	if err := s.invalidatePreviousRefreshSession(ctx, data.UserID, sessionHash, newSession.Token); err != nil {
		return nil, err
	}

	return newSession, nil
}

func (s *Service) refreshSessionData(ctx context.Context, token string) (string, sessionData, error) {
	sessionHash := sha256Hex(token)
	key := sessionKeyPrefix + sessionHash

	var data sessionData
	if err := s.cacheSvc.Get(ctx, key, &data); err != nil {
		return "", data, newError(CodeInternal, "failed to read session", err)
	}
	if data.UserID == "" || time.Now().UTC().After(data.ExpiresAt) {
		s.deleteExpiredSession(ctx, key, sessionHash, data.UserID)
		return "", data, newError(CodeUnauthorized, "invalid session", nil)
	}

	return sessionHash, data, nil
}

func (s *Service) deleteExpiredSession(ctx context.Context, key, sessionHash, userID string) {
	_ = s.cacheSvc.Del(ctx, key)
	if userID != "" {
		_, _ = s.cacheSvc.SRem(ctx, userSessionsKeyPrefix+userID, []string{sessionHash})
	}
}

func (s *Service) invalidatePreviousRefreshSession(ctx context.Context, userID, sessionHash, newToken string) error {
	invalidation := s.deleteSessionByHash(ctx, userID, sessionHash)
	if invalidation.keyErr != nil {
		return s.refreshInvalidationError(userID, newToken, invalidation.keyErr)
	}

	if invalidation.indexErr != nil && s.logger != nil {
		s.logger.Warn(
			"Failed to remove previous session from user index during refresh",
			slog.String("user_id", userID),
			slog.Any("error", invalidation.indexErr),
		)
	}

	return nil
}

func (s *Service) refreshInvalidationError(userID, newToken string, invalidationErr error) error {
	err := invalidationErr
	rollbackErr := s.deleteSessionByHash(context.Background(), userID, sha256Hex(newToken))
	if rollbackErr.Err() != nil {
		err = stdErrors.Join(err, fmt.Errorf("rollback new session: %w", rollbackErr.Err()))
	}
	return newError(CodeInternal, "failed to invalidate previous session during refresh", err)
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
		s.deleteExpiredSession(ctx, key, sessionHash, data.UserID)
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

	token, sessionHash, err := s.allocateSessionToken(ctx, string(payload))
	if err != nil {
		return nil, err
	}

	if err := s.addSessionIndex(ctx, userID, sessionHash); err != nil {
		return nil, err
	}

	return &Session{
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) allocateSessionToken(ctx context.Context, payload string) (string, string, error) {
	for range 3 {
		raw, err := generateToken(sessionTokenPrefix, 32)
		if err != nil {
			return "", "", newError(CodeInternal, "failed to generate session token", err)
		}

		hash := sha256Hex(raw)
		acquired, err := s.cacheSvc.SetNX(ctx, sessionKeyPrefix+hash, payload, s.cfg.SessionTTL)
		if err != nil {
			return "", "", newError(CodeInternal, "failed to store session", err)
		}
		if acquired {
			return raw, hash, nil
		}
	}

	return "", "", newError(CodeInternal, "failed to allocate unique session token", nil)
}

func (s *Service) addSessionIndex(ctx context.Context, userID, sessionHash string) error {
	userSessionsKey := userSessionsKeyPrefix + userID
	if _, err := s.cacheSvc.SAdd(ctx, userSessionsKey, []string{sessionHash}); err != nil {
		_ = s.cacheSvc.Del(ctx, sessionKeyPrefix+sessionHash)
		return newError(CodeInternal, "failed to update session index", err)
	}
	if err := s.cacheSvc.Expire(ctx, userSessionsKey, s.cfg.UserSessionsTTL); err != nil {
		_, _ = s.cacheSvc.SRem(ctx, userSessionsKey, []string{sessionHash})
		_ = s.cacheSvc.Del(ctx, sessionKeyPrefix+sessionHash)
		return newError(CodeInternal, "failed to expire session index", err)
	}
	return nil
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
		return s.deleteUserSessionsIndex(ctx, userSessionsKey)
	}

	keys := sessionKeysFromHashes(hashes)
	return s.deleteUserSessionKeysAndIndex(ctx, userSessionsKey, keys)
}

func sessionKeysFromHashes(hashes []string) []string {
	keys := make([]string, 0, len(hashes))
	for _, h := range hashes {
		if h != "" {
			keys = append(keys, sessionKeyPrefix+h)
		}
	}
	return keys
}

func (s *Service) deleteUserSessionKeysAndIndex(ctx context.Context, userSessionsKey string, keys []string) error {
	var errs []error
	if _, err := s.cacheSvc.DelMany(ctx, keys); err != nil {
		errs = append(errs, fmt.Errorf("delete session keys: %w", err))
	}
	if err := s.deleteUserSessionsIndex(ctx, userSessionsKey); err != nil {
		errs = append(errs, err)
	}

	return stdErrors.Join(errs...)
}

func (s *Service) deleteUserSessionsIndex(ctx context.Context, userSessionsKey string) error {
	if err := s.cacheSvc.Del(ctx, userSessionsKey); err != nil {
		return fmt.Errorf("delete user session index: %w", err)
	}
	return nil
}
