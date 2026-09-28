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
	jsonv2 "encoding/json/v2"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) Logout(ctx context.Context, token string) error {
	if s.cacheClient == nil {
		return newError(CodeInternal, "cache service not configured", nil)
	}

	key := sessionKeyPrefix + sha256Hex(token)

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

	return nil
}

// Refresh는 세션을 원자적으로 회전한다. 기존 토큰을 compare-and-delete로 먼저 claim한 뒤
// 새 세션을 발급하므로, 동일 토큰에 대한 동시/연속 refresh는 정확히 한 번만 성공한다(replay 차단).
// Claim 전에 PG의 현재 세션 세대를 확인해 비밀번호 reset 이전 세션은 회전하지 않는다. 확인과 발급 사이에
// reset이 커밋되면 새 세션은 이전 세대를 담지만, Me·Refresh가 사용할 때마다 PG 세대와 다시 비교하므로 거부된다.
func (s *Service) Refresh(ctx context.Context, token string) (*Session, error) {
	if s.cacheClient == nil {
		return nil, newError(CodeInternal, "cache service not configured", nil)
	}

	userID, generation, err := s.claimSessionForRotation(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("claim session for rotation: %w", err)
	}

	newSession, err := s.createSession(ctx, userID, generation)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return newSession, nil
}

// claimSessionForRotation은 기존 세션이 현재 세대인지 확인한 뒤 저장된 값과 비교해 원자적으로 삭제한다.
// 삭제에 성공한 호출자만 회전 권한을 갖고, 새 세션에 쓸 PG 현재 세대를 함께 돌려받는다.
// 이미 소비/만료/회전/폐기된 토큰은 CodeUnauthorized.
func (s *Service) claimSessionForRotation(ctx context.Context, token string) (string, int64, error) {
	key := sessionKeyPrefix + sha256Hex(token)

	rawPayload, data, err := s.loadValidSessionPayload(ctx, key)
	if err != nil {
		return "", 0, fmt.Errorf("load valid session payload: %w", err)
	}

	generation, err := s.findSessionGeneration(ctx, data.UserID)
	if err != nil {
		return "", 0, fmt.Errorf("find session generation: %w", err)
	}

	if rejectErr := s.rejectRevokedSession(ctx, key, data.SessionGeneration, generation); rejectErr != nil {
		return "", 0, fmt.Errorf("reject revoked session: %w", rejectErr)
	}

	deleted, err := s.cacheClient.CompareAndDelete(ctx, key, rawPayload)
	if err != nil {
		return "", 0, newError(CodeInternal, "failed to claim session for rotation", err)
	}

	if !deleted {
		// 다른 요청이 같은 토큰을 먼저 회전/소비했다 (동시 refresh replay).
		return "", 0, newError(CodeUnauthorized, "invalid session", nil)
	}

	return data.UserID, generation, nil
}

// loadValidSessionPayload는 세션 키의 raw payload와 디코드된 데이터를 읽고 유효성을 검증한다.
// CAS 회전을 위해 저장된 정확한 raw 문자열을 그대로 반환한다(re-marshal 불일치 방지).
func (s *Service) loadValidSessionPayload(ctx context.Context, key string) (string, sessionData, error) {
	rawPayload, hit, err := s.cacheClient.GetString(ctx, key)
	if err != nil {
		return "", sessionData{}, newError(CodeInternal, "failed to read session", err)
	}

	if !hit {
		return "", sessionData{}, newError(CodeUnauthorized, "invalid session", nil)
	}

	var data sessionData

	if err := jsonv2.Unmarshal([]byte(rawPayload), &data); err != nil {
		return "", sessionData{}, newError(CodeInternal, "failed to decode session", err)
	}

	if data.UserID == "" || time.Now().UTC().After(data.ExpiresAt) {
		s.deleteSession(ctx, key)

		return "", sessionData{}, newError(CodeUnauthorized, "invalid session", nil)
	}

	return rawPayload, data, nil
}

// findSessionGeneration은 PG의 현재 세션 세대를 읽는다. 사용자가 없으면 CodeUnauthorized.
func (s *Service) findSessionGeneration(ctx context.Context, userID string) (int64, error) {
	var generation int64

	if err := s.db.QueryRow(ctx, mustSQL("service_session_0144_01.sql"), userID).Scan(&generation); err != nil {
		if stdErrors.Is(err, pgx.ErrNoRows) {
			return 0, newError(CodeUnauthorized, "user not found", nil)
		}

		return 0, newError(CodeInternal, "failed to query session generation", err)
	}

	return generation, nil
}

// rejectRevokedSession은 세션 payload의 세대가 PG 현재 세대와 다르면 세션 키를 지우고 CodeUnauthorized를 돌려준다.
// 세대는 비밀번호 reset마다 증가하므로, 불일치는 발급 이후 reset이 커밋됐다는 뜻이다.
func (s *Service) rejectRevokedSession(ctx context.Context, key string, sessionGeneration, currentGeneration int64) error {
	if sessionGeneration == currentGeneration {
		return nil
	}

	s.deleteSession(ctx, key)

	return newError(CodeUnauthorized, "session revoked", nil)
}

// deleteSession은 무효로 판정된 세션 키를 지운다(best-effort). 판정은 PG 세대·만료 시각이 소유하므로
// 삭제 실패가 거부 결과를 바꾸지 않고, 남은 키는 다음 사용에서 다시 거부되며 TTL로 사라진다.
func (s *Service) deleteSession(ctx context.Context, key string) {
	if err := s.cacheClient.Del(ctx, key); err != nil && s.logger != nil {
		s.logger.Warn("Failed to delete invalid session", slog.Any("error", err))
	}
}

func (s *Service) Me(ctx context.Context, token string) (*User, error) {
	key, data, err := s.validateSession(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("validate session: %w", err)
	}

	user, err := s.findUserByID(ctx, data.UserID)
	if err != nil {
		if stdErrors.Is(err, pgx.ErrNoRows) {
			return nil, newError(CodeUnauthorized, "user not found", nil)
		}

		return nil, newError(CodeInternal, "failed to query user", err)
	}

	if err := s.rejectRevokedSession(ctx, key, data.SessionGeneration, user.SessionGeneration); err != nil {
		return nil, fmt.Errorf("reject revoked session: %w", err)
	}

	return toUser(&user), nil
}

type sessionData struct {
	UserID    string    `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
	// SessionGeneration은 발급 당시 auth_users.session_generation이다. 이 필드가 없는 payload(migration 231
	// 배포 전 발급 세션)는 0으로 해석된다. 이는 migration 231의 DEFAULT 0과 같은 값이라 호환 경로가 아니며,
	// 그런 세션은 첫 비밀번호 reset 전까지 유효하고 reset 후 무효로 수렴한다.
	SessionGeneration int64 `json:"sessionGeneration"`
}

// validateSession은 토큰의 세션 키와 만료되지 않은 세션 payload를 돌려준다. 세대 비교는 호출자가 PG 조회와 함께 한다.
func (s *Service) validateSession(ctx context.Context, token string) (string, sessionData, error) {
	if s.cacheClient == nil {
		return "", sessionData{}, newError(CodeInternal, "cache service not configured", nil)
	}

	if token == "" {
		return "", sessionData{}, newError(CodeUnauthorized, "missing token", nil)
	}

	key := sessionKeyPrefix + sha256Hex(token)

	var data sessionData

	if err := s.cacheClient.Get(ctx, key, &data); err != nil {
		return "", sessionData{}, newError(CodeInternal, "failed to read session", err)
	}

	if data.UserID == "" || time.Now().UTC().After(data.ExpiresAt) {
		s.deleteSession(ctx, key)

		return "", sessionData{}, newError(CodeUnauthorized, "invalid session", nil)
	}

	return key, data, nil
}

// createSession은 generation(발급 시점의 PG 세션 세대)을 담은 세션을 저장한다.
func (s *Service) createSession(ctx context.Context, userID string, generation int64) (*Session, error) {
	if s.cacheClient == nil {
		return nil, newError(CodeInternal, "cache service not configured", nil)
	}

	if userID == "" {
		return nil, newError(CodeInternal, "userID is empty", nil)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.config.SessionTTL)
	data := sessionData{
		UserID:            userID,
		ExpiresAt:         expiresAt,
		CreatedAt:         now,
		SessionGeneration: generation,
	}

	payload, err := jsonv2.Marshal(&data)
	if err != nil {
		return nil, newError(CodeInternal, "failed to marshal session", err)
	}

	token, err := s.allocateSessionToken(ctx, string(payload))
	if err != nil {
		return nil, fmt.Errorf("allocate session token: %w", err)
	}

	return &Session{
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

// allocateSessionToken은 원문 session token을 만들고 그 SHA-256 hash key에 payload를 저장한다.
func (s *Service) allocateSessionToken(ctx context.Context, payload string) (string, error) {
	for range 3 {
		raw, err := generateToken(sessionTokenPrefix, 32)
		if err != nil {
			return "", newError(CodeInternal, "failed to generate session token", err)
		}

		acquired, err := s.cacheClient.SetNX(ctx, sessionKeyPrefix+sha256Hex(raw), payload, s.config.SessionTTL)
		if err != nil {
			return "", newError(CodeInternal, "failed to store session", err)
		}

		if acquired {
			return raw, nil
		}
	}

	return "", newError(CodeInternal, "failed to allocate unique session token", nil)
}
