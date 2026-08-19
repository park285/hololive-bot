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
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/testutil"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateToken_Uniqueness(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for range 100 {
		token, err := generateToken("sess_", 32)
		require.NoError(t, err)
		assert.NotEmpty(t, token)
		assert.Contains(t, token, "sess_")
		_, exists := seen[token]
		assert.False(t, exists, "중복 토큰 생성됨: %s", token)
		seen[token] = struct{}{}
	}
}

func TestGenerateToken_DifferentPrefixes(t *testing.T) {
	t.Parallel()

	sessToken, err := generateToken("sess_", 32)
	require.NoError(t, err)
	assert.True(t, len(sessToken) > len("sess_"))

	resetToken, err := generateToken("reset_", 32)
	require.NoError(t, err)
	assert.True(t, len(resetToken) > len("reset_"))

	// 접두사가 올바른지 확인
	assert.Contains(t, sessToken, "sess_")
	assert.Contains(t, resetToken, "reset_")
}

func TestGenerateToken_InvalidByteLen(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		byteLen int
	}{
		"0 바이트": {byteLen: 0},
		"음수":    {byteLen: -1},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := generateToken("test_", tc.byteLen)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "byteLen must be positive")
		})
	}
}

func TestSha256Hex_Deterministic(t *testing.T) {
	t.Parallel()

	input := "test-input-string"
	hash1 := sha256Hex(input)
	hash2 := sha256Hex(input)

	assert.Equal(t, hash1, hash2, "동일 입력에 대해 동일 해시가 나와야 함")
	assert.Len(t, hash1, 64, "SHA-256 hex 길이는 64자")
}

func TestSha256Hex_DifferentInputs(t *testing.T) {
	t.Parallel()

	hash1 := sha256Hex("input-a")
	hash2 := sha256Hex("input-b")
	assert.NotEqual(t, hash1, hash2)
}

func TestSha256Hex_EmptyString(t *testing.T) {
	t.Parallel()

	hash := sha256Hex("")
	assert.Len(t, hash, 64)
	assert.NotEmpty(t, hash)
}

func TestCreateSession_Success(t *testing.T) {
	cache := testutil.NewTestCacheService(t, context.Background())

	config := DefaultConfig()
	config.SessionTTL = 30 * time.Minute
	config.UserSessionsTTL = 2 * time.Hour

	service, err := NewService(context.Background(), newTestDB(t), cache, sharedlogging.NewTestLogger(), config)
	require.NoError(t, err)

	session, err := service.createSession(context.Background(), "user-123")
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.NotEmpty(t, session.Token)
	assert.False(t, session.ExpiresAt.IsZero())
	// 만료 시각은 SessionTTL 이후여야 함
	assert.True(t, session.ExpiresAt.After(time.Now().UTC()))
}

func TestCreateSession_StoresJSONSessionDataAndUserIndex(t *testing.T) {
	cache := testutil.NewTestCacheService(t, context.Background())

	config := DefaultConfig()
	config.SessionTTL = 30 * time.Minute
	config.UserSessionsTTL = 2 * time.Hour

	service, err := NewService(context.Background(), newTestDB(t), cache, sharedlogging.NewTestLogger(), config)
	require.NoError(t, err)

	session, err := service.createSession(context.Background(), "user-123")
	require.NoError(t, err)

	sessionHash := sha256Hex(session.Token)

	var stored sessionData
	require.NoError(t, cache.Get(context.Background(), sessionKeyPrefix+sessionHash, &stored))
	assert.Equal(t, "user-123", stored.UserID)
	assert.WithinDuration(t, session.ExpiresAt, stored.ExpiresAt, time.Second)
	assert.False(t, stored.CreatedAt.IsZero())

	userSessions, err := cache.SMembers(context.Background(), userSessionsKeyPrefix+"user-123")
	require.NoError(t, err)
	assert.Contains(t, userSessions, sessionHash)
}

func TestCreateSession_NoCacheService(t *testing.T) {
	db := newTestDB(t)
	service, err := NewService(context.Background(), db, nil, sharedlogging.NewTestLogger(), DefaultConfig())
	require.NoError(t, err)

	_, err = service.createSession(context.Background(), "user-123")
	require.Error(t, err)
	assertAuthCode(t, err, CodeInternal)
}

func TestCreateSession_EmptyUserID(t *testing.T) {
	cache := testutil.NewTestCacheService(t, context.Background())

	service, err := NewService(context.Background(), newTestDB(t), cache, sharedlogging.NewTestLogger(), DefaultConfig())
	require.NoError(t, err)

	_, err = service.createSession(context.Background(), "")
	require.Error(t, err)
	assertAuthCode(t, err, CodeInternal)
}

func TestCreateSession_UniqueSessions(t *testing.T) {
	cache := testutil.NewTestCacheService(t, context.Background())

	service, err := NewService(context.Background(), newTestDB(t), cache, sharedlogging.NewTestLogger(), DefaultConfig())
	require.NoError(t, err)

	s1, err := service.createSession(context.Background(), "user-123")
	require.NoError(t, err)

	s2, err := service.createSession(context.Background(), "user-123")
	require.NoError(t, err)

	assert.NotEqual(t, s1.Token, s2.Token, "동일 사용자여도 세션 토큰이 달라야 함")
}

// -- Error 타입 테스트 --

func TestAuthError_String(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  *Error
		want string
	}{
		"nil": {
			err:  nil,
			want: "<nil>",
		},
		"코드만": {
			err:  &Error{Code: CodeInvalidInput},
			want: "auth error code=INVALID_INPUT",
		},
		"메시지 포함": {
			err:  &Error{Code: CodeInvalidInput, Message: "bad input"},
			want: "auth error code=INVALID_INPUT: bad input",
		},
		"에러 래핑": {
			err:  &Error{Code: CodeInternal, Err: assert.AnError},
			want: "auth error code=INTERNAL_ERROR: assert.AnError general error for testing",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.err.Error())
		})
	}
}

func TestAuthError_Unwrap(t *testing.T) {
	t.Parallel()

	inner := assert.AnError
	err := &Error{Code: CodeInternal, Err: inner}
	assert.Equal(t, inner, err.Unwrap())
}
