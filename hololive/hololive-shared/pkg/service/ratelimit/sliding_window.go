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

package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
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

// Decision: 분산 슬라이딩 윈도우 판정 결과
type Decision struct {
	Allowed    bool
	Current    int
	Remaining  int
	Limit      int
	Window     time.Duration
	RetryAfter time.Duration
}

// SlidingWindowLimiter: Valkey ZSET 기반 분산 슬라이딩 윈도우 레이트 리미터
type SlidingWindowLimiter struct {
	cacheSvc  cache.Client
	keyPrefix string
	logger    *slog.Logger
	sequence  atomic.Uint64
}

// NewSlidingWindowLimiter: 분산 슬라이딩 윈도우 레이트 리미터를 생성합니다.
func NewSlidingWindowLimiter(cacheSvc cache.Client, keyPrefix string, logger *slog.Logger) (*SlidingWindowLimiter, error) {
	if cacheSvc == nil {
		return nil, fmt.Errorf("new sliding window limiter: cache service must not be nil")
	}
	if keyPrefix == "" {
		keyPrefix = defaultKeyPrefix
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SlidingWindowLimiter{
		cacheSvc:  cacheSvc,
		keyPrefix: keyPrefix,
		logger:    logger,
	}, nil
}

// Allow: bucket 단위로 요청 허용 여부를 판정합니다.
func (l *SlidingWindowLimiter) Allow(ctx context.Context, bucket string, limit int, window time.Duration) (Decision, error) {
	if bucket == "" {
		return Decision{}, fmt.Errorf("allow: bucket must not be empty")
	}
	if limit <= 0 {
		return Decision{}, fmt.Errorf("allow: limit must be greater than zero")
	}
	if window <= 0 {
		return Decision{}, fmt.Errorf("allow: window must be greater than zero")
	}

	windowMS := window.Milliseconds()
	if windowMS <= 0 {
		return Decision{}, fmt.Errorf("allow: window milliseconds must be greater than zero")
	}

	key := l.buildKey(bucket)
	nowMS := time.Now().UnixMilli()
	member := l.memberID(nowMS)
	ttlSeconds := ttlSecondsFromWindow(window)

	allowed, current, retryAfter, err := l.allowByScript(ctx, key, nowMS, windowMS, limit, member, ttlSeconds)
	if err != nil {
		return Decision{}, fmt.Errorf("allow: evaluate script: %w", err)
	}

	remaining := limit - current
	if remaining < 0 {
		remaining = 0
	}

	result := Decision{
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
	cmd := l.cacheSvc.B().
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
	resp := l.cacheSvc.GetClient().Do(ctx, cmd)
	if resp.Error() != nil {
		return false, 0, 0, fmt.Errorf("eval allow script: %w", resp.Error())
	}

	values, err := resp.ToArray()
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
	count, err := values[1].AsInt64()
	if err != nil {
		return false, 0, 0, fmt.Errorf("parse current count: %w", err)
	}
	if count < 0 {
		return false, 0, 0, fmt.Errorf("invalid current count result: %d", count)
	}
	retryAfterMS, err := values[2].AsInt64()
	if err != nil {
		return false, 0, 0, fmt.Errorf("parse retry after: %w", err)
	}
	if retryAfterMS < 0 {
		return false, 0, 0, fmt.Errorf("invalid retry after result: %d", retryAfterMS)
	}
	return flag == 1, int(count), time.Duration(retryAfterMS) * time.Millisecond, nil
}

func (l *SlidingWindowLimiter) buildKey(bucket string) string {
	return l.keyPrefix + ":" + bucket
}

func (l *SlidingWindowLimiter) memberID(nowMS int64) string {
	seq := l.sequence.Add(1)
	return strconv.FormatInt(nowMS, 10) + "-" + strconv.FormatUint(seq, 10)
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
