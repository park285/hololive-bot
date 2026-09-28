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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

// lockCache: *cache.Service가 만족하는 최소 인터페이스.
type lockCache interface {
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error)
}

type NotificationLocker interface {
	TryAcquire(ctx context.Context, lockKey string, ttl time.Duration) (token string, acquired bool, err error)
	Release(ctx context.Context, lockKey, token string) error
}

// NewLocker는 Valkey 기반 locker만 만든다. 캐시가 없으면 dedup을 끈 채 진행하던 noop locker로 내려가지 않고 생성
// 오류를 돌려준다(stack-audit 2026-09-26 T11 holo-delivery-noop-locker-fallback). 중복 발송 방지가 꺼진 상태를 기동
// 성공으로 숨기지 않기 위해서다.
func NewLocker(cache lockCache, logger *slog.Logger) (NotificationLocker, error) {
	if cache == nil {
		return nil, errors.New("new notification locker: cache is nil")
	}

	return &valkeyNotificationLocker{cache: cache, logger: logger}, nil
}

// valkeyNotificationLocker: Valkey 기반 NotificationLocker 구현.
// Valkey 오류는 모두 호출자에게 돌려준다. 경고만 남기고 lock·claim 없이 진행하던 graceful degradation은 계약 없는
// 폴백이라 지웠다(stack-audit 2026-09-26 T19 holo-delivery-valkey-locker-warn-proceed). 이제 lock 획득 실패는 digest 실행
// 실패로 남아 다음 주기에 다시 시도되고, 해제 실패는 lock이 TTL로 만료될 때까지 같은 작업을 막는다.
type valkeyNotificationLocker struct {
	cache  lockCache
	logger *slog.Logger
}

func (l *valkeyNotificationLocker) TryAcquire(ctx context.Context, lockKey string, ttl time.Duration) (string, bool, error) {
	token := uuid.New().String()
	// jsonv2.Marshal로 직렬화 (cache.Get이 jsonv2.Unmarshal 사용)
	tokenJSON, err := jsonv2.Marshal(token)
	if err != nil {
		return "", false, fmt.Errorf("marshal lock token: %w", err)
	}

	value := string(tokenJSON)

	acquired, err := l.cache.SetNX(ctx, lockKey, value, ttl)
	if err != nil {
		return "", false, fmt.Errorf("acquire notification lock: set nx: %w", err)
	}

	return token, acquired, nil
}

func (l *valkeyNotificationLocker) Release(ctx context.Context, lockKey, token string) error {
	// jsonv2.Marshal(token) → CompareAndDelete로 원자적 해제
	tokenJSON, err := jsonv2.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal lock token: %w", err)
	}

	value := string(tokenJSON)

	deleted, err := l.cache.CompareAndDelete(ctx, lockKey, value)
	if err != nil {
		return fmt.Errorf("release notification lock: compare and delete: %w", err)
	}

	if !deleted {
		l.logger.Debug("Lock owned by another instance, skipping release",
			privacylog.CacheKeyAttr(lockKey))
	}

	return nil
}
