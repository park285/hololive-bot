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

// Package valkey는 ratelimit.Decision 계약을 Valkey sorted-set 슬라이딩 윈도우로 구현한다.
package valkey

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"uuid"

	valkeygo "github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/ratelimit"
)

const defaultKeyPrefix = "ratelimit:sliding"

const allowScript = `
local key = KEYS[1]
local now_ms = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]
local ttl_sec = tonumber(ARGV[5])

redis.call('ZREMRANGEBYSCORE', key, '-inf', now_ms - window_ms)
local count = redis.call('ZCARD', key)
if count >= limit then
  local retry_after_ms = 0
  local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
  if oldest[2] ~= nil then
    local oldest_ms = tonumber(oldest[2])
    retry_after_ms = window_ms - (now_ms - oldest_ms)
    if retry_after_ms < 0 then
      retry_after_ms = 0
    end
  end
  return {0, count, retry_after_ms}
end

redis.call('ZADD', key, now_ms, member)
redis.call('EXPIRE', key, ttl_sec)
return {1, count + 1, 0}
`

type SlidingWindowLimiter struct {
	cacheClient cache.LowLevelCache
	keyPrefix   string
	logger      *slog.Logger
	instanceID  string
	sequence    atomic.Uint64
	now         func() time.Time
}

func NewSlidingWindowLimiter(cacheClient cache.LowLevelCache, keyPrefix string, logger *slog.Logger) (*SlidingWindowLimiter, error) {
	if cacheClient == nil {
		return nil, errors.New("new sliding window limiter: cache service must not be nil")
	}

	if keyPrefix == "" {
		keyPrefix = defaultKeyPrefix
	}

	if logger == nil {
		logger = slog.Default()
	}

	instanceID, err := resolveInstanceID(os.Hostname)
	if err != nil {
		return nil, fmt.Errorf("new sliding window limiter: %w", err)
	}

	return &SlidingWindowLimiter{
		cacheClient: cacheClient,
		keyPrefix:   keyPrefix,
		logger:      logger,
		instanceID:  instanceID + ":" + uuid.New().String(),
		now:         time.Now,
	}, nil
}

func (l *SlidingWindowLimiter) Allow(ctx context.Context, bucket string, limit int, window time.Duration) (ratelimit.Decision, error) {
	if bucket == "" {
		return ratelimit.Decision{}, errors.New("allow: bucket must not be empty")
	}

	if limit <= 0 {
		return ratelimit.Decision{}, errors.New("allow: limit must be greater than zero")
	}

	if window <= 0 {
		return ratelimit.Decision{}, errors.New("allow: window must be greater than zero")
	}

	windowMS := window.Milliseconds()
	if windowMS <= 0 {
		return ratelimit.Decision{}, errors.New("allow: window milliseconds must be greater than zero")
	}

	key := l.buildKey(bucket)
	nowMS := l.nowFunc()().UnixMilli()
	member := l.memberID(nowMS)
	ttlSeconds := ttlSecondsFromWindow(window)

	allowed, current, retryAfter, err := l.allowByScript(ctx, key, nowMS, windowMS, limit, member, ttlSeconds)
	if err != nil {
		return ratelimit.Decision{}, fmt.Errorf("allow: evaluate script: %w", err)
	}

	remaining := max(0, limit-current)

	result := ratelimit.Decision{
		Allowed:   allowed,
		Current:   current,
		Remaining: remaining,
		Limit:     limit,
		Window:    window,
	}
	if allowed {
		return result, nil
	}

	result.RetryAfter = retryAfter

	l.logger.Debug("rate limit denied",
		slog.String("bucket", bucket),
		slog.Int("limit", limit),
		slog.Int("current", current),
		slog.Duration("window", window),
		slog.Duration("retry_after", retryAfter),
	)

	return result, nil
}

func (l *SlidingWindowLimiter) allowByScript(
	ctx context.Context,
	key string,
	nowMS int64,
	windowMS int64,
	limit int,
	member string,
	ttlSeconds int64,
) (bool, int, time.Duration, error) {
	cmd := l.cacheClient.B().
		Eval().
		Script(allowScript).
		Numkeys(1).
		Key(key).
		Arg(
			strconv.FormatInt(nowMS, 10),
			strconv.FormatInt(windowMS, 10),
			strconv.Itoa(limit),
			member,
			strconv.FormatInt(ttlSeconds, 10),
		).
		Build()
	results := l.cacheClient.DoMulti(ctx, cmd)

	if len(results) != 1 {
		return false, 0, 0, fmt.Errorf("eval allow script: unexpected result count: %d", len(results))
	}

	if results[0].Error() != nil {
		return false, 0, 0, fmt.Errorf("eval allow script: %w", results[0].Error())
	}

	out1, out2, out3, err := parseAllowScriptResult(results[0])
	if err != nil {
		return out1, out2, out3, fmt.Errorf("parse allow script result: %w", err)
	}

	return out1, out2, out3, nil
}

func parseAllowScriptResult(result valkeygo.ValkeyResult) (bool, int, time.Duration, error) {
	values, err := result.ToArray()
	if err != nil {
		return false, 0, 0, fmt.Errorf("parse allow script result: %w", err)
	}

	if len(values) != 3 {
		return false, 0, 0, fmt.Errorf("invalid allow script result length: %d", len(values))
	}

	flag, err := values[0].AsInt64()
	if err != nil {
		return false, 0, 0, fmt.Errorf("parse allow flag: %w", err)
	}

	count, err := parseNonNegativeScriptInt(values[1], "current count", "current count result")
	if err != nil {
		return false, 0, 0, fmt.Errorf("parse non negative script int: %w", err)
	}

	retryAfterMS, err := parseNonNegativeScriptInt(values[2], "retry after", "retry after result")
	if err != nil {
		return false, 0, 0, fmt.Errorf("parse non negative script int: %w", err)
	}

	return flag == 1, int(count), time.Duration(retryAfterMS) * time.Millisecond, nil
}

func parseNonNegativeScriptInt(message valkeygo.ValkeyMessage, name, resultName string) (int64, error) {
	value, err := message.AsInt64()
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}

	if value < 0 {
		return 0, fmt.Errorf("invalid %s: %d", resultName, value)
	}

	return value, nil
}

func (l *SlidingWindowLimiter) nowFunc() func() time.Time {
	if l.now == nil {
		return time.Now
	}

	return l.now
}

func (l *SlidingWindowLimiter) buildKey(bucket string) string {
	return l.keyPrefix + ":" + bucket
}

func (l *SlidingWindowLimiter) memberID(nowMS int64) string {
	seq := l.sequence.Add(1)
	return strconv.FormatInt(nowMS, 10) + ":" + l.instanceID + ":" + strconv.FormatUint(seq, 10)
}

// resolveInstanceID는 sorted set member의 필수 hostname 구분자를 읽는다. 생성자는 UUID를 덧붙여
// 같은 호스트의 서로 다른 limiter와 프로세스가 같은 밀리초·순번을 써도 표식이 겹치지 않게 한다.
// T18(2026-09-26)에서 INSTANCE_ID가 어느 운영 env에도 없음을 확인해 hostname을 필수 출처로 정했다.
// 퇴역한 INSTANCE_ID 키의 존재 거절은 settings.LoadConfig runtime(config_ratelimit_retired_env.go)이 소유한다.
// 호스트 이름을 얻지 못하면 random이나 "local"로 바꾸지 않고 오류다. 같은 member를 두 인스턴스가 만들면 한도 판정이 어긋나기 때문이다.
func resolveInstanceID(hostname func() (string, error)) (string, error) {
	host, err := hostname()
	if err != nil {
		return "", fmt.Errorf("resolve instance id: hostname: %w", err)
	}

	if strings.TrimSpace(host) == "" {
		return "", errors.New("resolve instance id: hostname is empty")
	}

	return sanitizeInstanceID(host), nil
}

func sanitizeInstanceID(value string) string {
	value = strings.TrimSpace(value)

	var b strings.Builder

	for _, r := range value {
		if isInstanceIDRune(r) {
			b.WriteRune(r)

			continue
		}

		b.WriteByte('_')
	}

	return b.String()
}

func isInstanceIDRune(r rune) bool {
	return strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.", r)
}

func ttlSecondsFromWindow(window time.Duration) int64 {
	if window <= time.Second {
		return 1
	}

	ttl := int64(window / time.Second)
	if window%time.Second != 0 {
		ttl++
	}

	if ttl < 1 {
		return 1
	}

	return ttl
}
