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
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const incrWithTTLScript = `
local current = redis.call('INCR', KEYS[1])
if current == 1 and tonumber(ARGV[1]) > 0 then
  redis.call('EXPIRE', KEYS[1], tonumber(ARGV[1]))
end
return current
`

func (s *Service) isLoginRateLimited(ctx context.Context, clientIP string) (bool, error) {
	if clientIP == "" || s.cacheClient == nil {
		return false, nil
	}

	key := loginRateLimitKeyPrefix + clientIP
	count, err := incrWithTTL(ctx, s.cacheClient, key, time.Minute)
	if err != nil {
		return false, err
	}

	return count > s.config.LoginRateLimitPerMinute, nil
}

func (s *Service) isPasswordResetRequestRateLimited(ctx context.Context, clientIP string) (bool, error) {
	if clientIP == "" || s.cacheClient == nil {
		return false, nil
	}

	key := resetReqRateLimitPrefix + clientIP
	count, err := incrWithTTL(ctx, s.cacheClient, key, time.Minute)
	if err != nil {
		return false, err
	}

	return count > s.config.PasswordResetRequestRateLimitPerMinute, nil
}

func (s *Service) isAccountLocked(ctx context.Context, email string) (bool, error) {
	if s.cacheClient == nil {
		return false, nil
	}
	key := accountLockKeyPrefix + email
	exists, err := s.cacheClient.Exists(ctx, key)
	if err != nil {
		return false, fmt.Errorf("cache exists failed: %w", err)
	}
	return exists, nil
}

func (s *Service) onLoginFailed(ctx context.Context, email string) {
	if s.cacheClient == nil {
		return
	}

	key := loginFailKeyPrefix + email
	count, err := incrWithTTL(ctx, s.cacheClient, key, s.config.LoginFailWindow)
	if err != nil {
		s.logger.Warn("login_fail_increment_failed", slog.Any("error", err))
		return
	}

	if count >= s.config.LoginFailLimit {
		lockKey := accountLockKeyPrefix + email
		_ = s.cacheClient.Set(ctx, lockKey, "1", s.config.LoginLockDuration)
		_ = s.cacheClient.Del(ctx, key)
	}
}

func (s *Service) onLoginSucceeded(ctx context.Context, email string) {
	if s.cacheClient == nil {
		return
	}
	_ = s.cacheClient.Del(ctx, loginFailKeyPrefix+email)
	_ = s.cacheClient.Del(ctx, accountLockKeyPrefix+email)
}

func incrWithTTL(ctx context.Context, cacheClient cache.Client, key string, ttl time.Duration) (int64, error) {
	ttlSeconds := int64(0)
	if ttl > 0 {
		ttlSeconds = int64(math.Ceil(ttl.Seconds()))
		if ttlSeconds <= 0 {
			ttlSeconds = 1
		}
	}

	cmd := cacheClient.B().
		Eval().
		Script(incrWithTTLScript).
		Numkeys(1).
		Key(key).
		Arg(strconv.FormatInt(ttlSeconds, 10)).
		Build()
	results := cacheClient.DoMulti(ctx, cmd)
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
