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
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/valkey-io/valkey-go"
)

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
		if err := s.cacheClient.Set(ctx, lockKey, "1", s.config.LoginLockDuration); err != nil {
			s.logger.Warn("account_lock_set_failed", slog.Any("error", err))
		}
		if err := s.cacheClient.Del(ctx, key); err != nil {
			s.logger.Warn("login_fail_counter_delete_failed", slog.Any("error", err))
		}
	}
}

func (s *Service) onLoginSucceeded(ctx context.Context, email string) {
	if s.cacheClient == nil {
		return
	}
	if err := s.cacheClient.Del(ctx, loginFailKeyPrefix+email); err != nil {
		s.logger.Warn("login_fail_counter_delete_failed", slog.Any("error", err))
	}
	if err := s.cacheClient.Del(ctx, accountLockKeyPrefix+email); err != nil {
		s.logger.Warn("account_lock_delete_failed", slog.Any("error", err))
	}
}

func incrWithTTL(ctx context.Context, cacheClient cache.Client, key string, ttl time.Duration) (int64, error) {
	ttlSeconds := ceilTTLSeconds(ttl)
	builder := cacheClient.B()
	cmds := incrWithTTLCommands(builder, key, ttlSeconds)

	results := cacheClient.DoMulti(ctx, cmds...)
	if len(results) == 0 {
		return 0, fmt.Errorf("increment with ttl: empty pipeline results")
	}
	if err := validateIncrWithTTLResults(key, results, len(cmds), ttlSeconds > 0); err != nil {
		return 0, err
	}

	count, err := results[0].AsInt64()
	if err != nil {
		return 0, fmt.Errorf("increment with ttl: parse incr %s: %w", key, err)
	}

	return count, nil
}

func ceilTTLSeconds(ttl time.Duration) int64 {
	if ttl <= 0 {
		return 0
	}

	seconds := int64(math.Ceil(ttl.Seconds()))
	if seconds <= 0 {
		return 1
	}

	return seconds
}

func incrWithTTLCommands(builder valkey.Builder, key string, ttlSeconds int64) []valkey.Completed {
	cmds := make([]valkey.Completed, 0, 2)
	cmds = append(cmds, builder.Incr().Key(key).Build())
	if ttlSeconds <= 0 {
		return cmds
	}

	return append(cmds, builder.Expire().Key(key).Seconds(ttlSeconds).Nx().Build())
}

func validateIncrWithTTLResults(key string, results []valkey.ValkeyResult, want int, hasTTL bool) error {
	if len(results) != want {
		return fmt.Errorf("increment with ttl: unexpected result count: %d", len(results))
	}

	if err := results[0].Error(); err != nil {
		return fmt.Errorf("increment with ttl: incr %s: %w", key, err)
	}

	if hasTTL {
		return validateIncrTTLExpireResult(key, results[1])
	}

	return nil
}

func validateIncrTTLExpireResult(key string, result valkey.ValkeyResult) error {
	if err := result.Error(); err != nil {
		return fmt.Errorf("increment with ttl: expire nx %s: %w", key, err)
	}

	return nil
}
