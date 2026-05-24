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

package delivery

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/park285/shared-go/pkg/json"
)

// lockCache: *cache.Service가 만족하는 최소 인터페이스
type lockCache interface {
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error)
	DelMany(ctx context.Context, keys []string) (int64, error)
}

type NotificationLocker interface {
	TryAcquire(ctx context.Context, lockKey string, ttl time.Duration) (token string, acquired bool, err error)
	Release(ctx context.Context, lockKey, token string) error
	ClaimRoom(ctx context.Context, claimKey string, ttl time.Duration) (acquired bool, err error)
	ReleaseRoomClaims(ctx context.Context, claimKeys []string) error
}

func NewLocker(cache lockCache, logger *slog.Logger) NotificationLocker {
	if cache == nil {
		return noopNotificationLocker{}
	}
	return &valkeyNotificationLocker{cache: cache, logger: logger}
}

// valkeyNotificationLocker: Valkey 기반 NotificationLocker 구현
type valkeyNotificationLocker struct {
	cache  lockCache
	logger *slog.Logger
}

func (l *valkeyNotificationLocker) TryAcquire(ctx context.Context, lockKey string, ttl time.Duration) (string, bool, error) {
	token := uuid.New().String()
	// json.Marshal로 직렬화 (cache.Get이 json.Unmarshal 사용)
	tokenJSON, _ := json.Marshal(token)
	value := string(tokenJSON)

	acquired, err := l.cache.SetNX(ctx, lockKey, value, ttl)
	if err != nil {
		// Valkey 장애 시 graceful degradation: 락 없이 진행
		l.logger.Warn("Lock SetNX failed, proceeding without lock",
			slog.String("key", lockKey),
			slog.String("error", err.Error()))
		return token, true, nil
	}
	return token, acquired, nil
}

func (l *valkeyNotificationLocker) Release(ctx context.Context, lockKey, token string) error {
	// json.Marshal(token) → CompareAndDelete로 원자적 해제
	tokenJSON, _ := json.Marshal(token)
	value := string(tokenJSON)

	deleted, err := l.cache.CompareAndDelete(ctx, lockKey, value)
	if err != nil {
		l.logger.Warn("Lock CompareAndDelete failed during release",
			slog.String("key", lockKey),
			slog.String("error", err.Error()))
		return nil
	}
	if !deleted {
		l.logger.Debug("Lock owned by another instance, skipping release",
			slog.String("key", lockKey))
	}
	return nil
}

func (l *valkeyNotificationLocker) ClaimRoom(ctx context.Context, claimKey string, ttl time.Duration) (bool, error) {
	acquired, err := l.cache.SetNX(ctx, claimKey, "1", ttl)
	if err != nil {
		// Valkey 장애 시 graceful degradation: claim 없이 진행
		l.logger.Warn("Room claim SetNX failed, proceeding",
			slog.String("key", claimKey),
			slog.String("error", err.Error()))
		return true, nil
	}
	return acquired, nil
}

func (l *valkeyNotificationLocker) ReleaseRoomClaims(ctx context.Context, claimKeys []string) error {
	if len(claimKeys) == 0 {
		return nil
	}
	if _, err := l.cache.DelMany(ctx, claimKeys); err != nil {
		l.logger.Warn("ReleaseRoomClaims failed",
			slog.Int("count", len(claimKeys)),
			slog.String("error", err.Error()))
	}
	return nil
}

// noopNotificationLocker: cache nil 시 fallback (dedup 비활성화)
type noopNotificationLocker struct{}

func (noopNotificationLocker) TryAcquire(_ context.Context, _ string, _ time.Duration) (string, bool, error) {
	return "", true, nil
}

func (noopNotificationLocker) Release(_ context.Context, _, _ string) error {
	return nil
}

func (noopNotificationLocker) ClaimRoom(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (noopNotificationLocker) ReleaseRoomClaims(_ context.Context, _ []string) error {
	return nil
}
