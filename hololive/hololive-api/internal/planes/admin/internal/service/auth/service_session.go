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

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/pkg/json"
)

func (s *Service) Logout(ctx context.Context, token string) error {
	if s.cacheClient == nil {
		return newError(CodeInternal, "cache service not configured", nil)
	}

	sessionHash := sha256Hex(token)
	key := sessionKeyPrefix + sessionHash

	var data sessionData
	if err := s.cacheClient.Get(ctx, key, &data); err != nil {
		return newError(CodeInternal, "failed to read session", err)
	}
	if data.UserID == "" {
		return newError(CodeUnauthorized, "invalid session", nil)
	}

	if err := s.cacheClient.Del(ctx, key); err != nil {
		return newError(CodeInternal, "failed to delete session", err)
	}
	if _, err := s.cacheClient.SRem(ctx, userSessionsKeyPrefix+data.UserID, []string{sessionHash}); err != nil && s.logger != nil {
		s.logger.Warn("Failed to remove logged out session from user index", slog.String("user_id", data.UserID), slog.Any("error", err))
	}

	return nil
}

// Refresh는 세션을 원자적으로 회전한다. 기존 토큰을 compare-and-delete로 먼저 claim한 뒤
// 새 세션을 발급하므로, 동일 토큰에 대한 동시/연속 refresh는 정확히 한 번만 성공한다(replay 차단).
func (s *Service) Refresh(ctx context.Context, token string) (*Session, error) {
	if s.cacheClient == nil {
		return nil, newError(CodeInternal, "cache service not configured", nil)
	}

	userID, err := s.claimSessionForRotation(ctx, token)
	if err != nil {
		return nil, err
	}

	newSession, err := s.createSession(ctx, userID)
	if err != nil {
		return nil, err
	}

	return newSession, nil
}

// claimSessionForRotation은 기존 세션 키를 저장된 값과 비교해 원자적으로 삭제한다.
// 삭제에 성공한 호출자만 회전 권한을 갖는다. 이미 소비/만료/회전된 토큰은 CodeUnauthorized.
func (s *Service) claimSessionForRotation(ctx context.Context, token string) (string, error) {
	sessionHash := sha256Hex(token)
	key := sessionKeyPrefix + sessionHash

	rawPayload, data, err := s.loadValidSessionPayload(ctx, key, sessionHash)
	if err != nil {
		return "", err
	}

	deleted, err := s.cacheClient.CompareAndDelete(ctx, key, rawPayload)
	if err != nil {
		return "", newError(CodeInternal, "failed to claim session for rotation", err)
	}
	if !deleted {
		// 다른 요청이 같은 토큰을 먼저 회전/소비했다 (동시 refresh replay).
		return "", newError(CodeUnauthorized, "invalid session", nil)
	}

	s.removeSessionIndex(ctx, data.UserID, sessionHash)
	return data.UserID, nil
}

// loadValidSessionPayload는 세션 키의 raw payload와 디코드된 데이터를 읽고 유효성을 검증한다.
// CAS 회전을 위해 저장된 정확한 raw 문자열을 그대로 반환한다(re-marshal 불일치 방지).
func (s *Service) loadValidSessionPayload(ctx context.Context, key, sessionHash string) (string, sessionData, error) {
	rawPayload, hit, err := s.cacheClient.GetString(ctx, key)
	if err != nil {
		return "", sessionData{}, newError(CodeInternal, "failed to read session", err)
	}
	if !hit {
		return "", sessionData{}, newError(CodeUnauthorized, "invalid session", nil)
	}

	var data sessionData
	if err := json.Unmarshal([]byte(rawPayload), &data); err != nil {
		return "", sessionData{}, newError(CodeInternal, "failed to decode session", err)
	}
	if data.UserID == "" || time.Now().UTC().After(data.ExpiresAt) {
		s.deleteExpiredSession(ctx, key, sessionHash, data.UserID)
		return "", sessionData{}, newError(CodeUnauthorized, "invalid session", nil)
	}

	return rawPayload, data, nil
}

// removeSessionIndex는 user 세션 인덱스에서 hash를 제거한다(best-effort).
// 세션 키는 이미 원자적으로 삭제됐으므로 인덱스 정리 실패는 회전을 막지 않는다.
func (s *Service) removeSessionIndex(ctx context.Context, userID, sessionHash string) {
	if userID == "" {
		return
	}
	if _, err := s.cacheClient.SRem(ctx, userSessionsKeyPrefix+userID, []string{sessionHash}); err != nil && s.logger != nil {
		s.logger.Warn(
			"Failed to remove previous session from user index during refresh",
			slog.String("user_id", userID),
			slog.Any("error", err),
		)
	}
}

func (s *Service) deleteExpiredSession(ctx context.Context, key, sessionHash, userID string) {
	if err := s.cacheClient.Del(ctx, key); err != nil && s.logger != nil {
		s.logger.Warn("Failed to delete expired session", slog.Any("error", err))
	}
	if userID != "" {
		if _, err := s.cacheClient.SRem(ctx, userSessionsKeyPrefix+userID, []string{sessionHash}); err != nil && s.logger != nil {
			s.logger.Warn("Failed to remove expired session from user index", slog.String("user_id", userID), slog.Any("error", err))
		}
	}
}

func (s *Service) Me(ctx context.Context, token string) (*User, error) {
	userID, err := s.validateSession(ctx, token)
	if err != nil {
		return nil, err
	}

	user, err := s.findUserByID(ctx, userID)
	if err != nil {
		if stdErrors.Is(err, pgx.ErrNoRows) {
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
	if s.cacheClient == nil {
		return "", newError(CodeInternal, "cache service not configured", nil)
	}
	if token == "" {
		return "", newError(CodeUnauthorized, "missing token", nil)
	}

	sessionHash := sha256Hex(token)
	key := sessionKeyPrefix + sessionHash
	var data sessionData
	if err := s.cacheClient.Get(ctx, key, &data); err != nil {
		return "", newError(CodeInternal, "failed to read session", err)
	}
	if data.UserID == "" || time.Now().UTC().After(data.ExpiresAt) {
		s.deleteExpiredSession(ctx, key, sessionHash, data.UserID)
		return "", newError(CodeUnauthorized, "invalid session", nil)
	}
	return data.UserID, nil
}

func (s *Service) createSession(ctx context.Context, userID string) (*Session, error) {
	if s.cacheClient == nil {
		return nil, newError(CodeInternal, "cache service not configured", nil)
	}
	if userID == "" {
		return nil, newError(CodeInternal, "userID is empty", nil)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.config.SessionTTL)
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

func (s *Service) allocateSessionToken(ctx context.Context, payload string) (value0, value1 string, err error) {
	for range 3 {
		raw, err := generateToken(sessionTokenPrefix, 32)
		if err != nil {
			return "", "", newError(CodeInternal, "failed to generate session token", err)
		}

		hash := sha256Hex(raw)
		acquired, err := s.cacheClient.SetNX(ctx, sessionKeyPrefix+hash, payload, s.config.SessionTTL)
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
	if _, err := s.cacheClient.SAdd(ctx, userSessionsKey, []string{sessionHash}); err != nil {
		cleanupErr := s.cacheClient.Del(ctx, sessionKeyPrefix+sessionHash)
		return newError(CodeInternal, "failed to update session index", stdErrors.Join(err, cleanupErr))
	}
	if err := s.cacheClient.Expire(ctx, userSessionsKey, s.config.UserSessionsTTL); err != nil {
		_, removeErr := s.cacheClient.SRem(ctx, userSessionsKey, []string{sessionHash})
		deleteErr := s.cacheClient.Del(ctx, sessionKeyPrefix+sessionHash)
		return newError(CodeInternal, "failed to expire session index", stdErrors.Join(err, removeErr, deleteErr))
	}
	return nil
}

func (s *Service) revokeAllSessions(ctx context.Context, userID string) error {
	if s.cacheClient == nil || userID == "" {
		return nil
	}

	userSessionsKey := userSessionsKeyPrefix + userID
	hashes, err := s.cacheClient.SMembers(ctx, userSessionsKey)
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
	if _, err := s.cacheClient.DelMany(ctx, keys); err != nil {
		errs = append(errs, fmt.Errorf("delete session keys: %w", err))
	}
	if err := s.deleteUserSessionsIndex(ctx, userSessionsKey); err != nil {
		errs = append(errs, err)
	}

	return stdErrors.Join(errs...)
}

func (s *Service) deleteUserSessionsIndex(ctx context.Context, userSessionsKey string) error {
	if err := s.cacheClient.Del(ctx, userSessionsKey); err != nil {
		return fmt.Errorf("delete user session index: %w", err)
	}
	return nil
}
