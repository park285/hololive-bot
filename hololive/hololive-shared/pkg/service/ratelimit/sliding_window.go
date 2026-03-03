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
  return 0
end

redis.call('ZADD', key, now_ms, member)
redis.call('EXPIRE', key, ttl_sec)
return 1
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

	allowed, err := l.allowByScript(ctx, key, nowMS, windowMS, limit, member, ttlSeconds)
	if err != nil {
		return Decision{}, fmt.Errorf("allow: evaluate script: %w", err)
	}

	current, err := l.currentCount(ctx, key, nowMS, windowMS)
	if err != nil {
		return Decision{}, fmt.Errorf("allow: read current count: %w", err)
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

	retryAfter, err := l.retryAfter(ctx, key, nowMS, windowMS)
	if err != nil {
		return Decision{}, fmt.Errorf("allow: calculate retry after: %w", err)
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
) (bool, error) {
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
		return false, fmt.Errorf("eval allow script: %w", resp.Error())
	}

	flag, err := resp.AsInt64()
	if err != nil {
		return false, fmt.Errorf("parse allow flag: %w", err)
	}
	return flag == 1, nil
}

func (l *SlidingWindowLimiter) currentCount(ctx context.Context, key string, nowMS, windowMS int64) (int, error) {
	minScore := strconv.FormatInt(nowMS-windowMS, 10)
	cmd := l.cacheSvc.B().Zcount().Key(key).Min(minScore).Max("+inf").Build()
	resp := l.cacheSvc.GetClient().Do(ctx, cmd)
	if resp.Error() != nil {
		return 0, fmt.Errorf("zcount: %w", resp.Error())
	}

	count, err := resp.AsInt64()
	if err != nil {
		return 0, fmt.Errorf("parse zcount: %w", err)
	}
	if count < 0 {
		return 0, fmt.Errorf("invalid zcount result: %d", count)
	}
	return int(count), nil
}

func (l *SlidingWindowLimiter) retryAfter(ctx context.Context, key string, nowMS, windowMS int64) (time.Duration, error) {
	cmd := l.cacheSvc.B().Zrange().Key(key).Min("0").Max("0").Withscores().Build()
	resp := l.cacheSvc.GetClient().Do(ctx, cmd)
	if resp.Error() != nil {
		return 0, fmt.Errorf("zrange oldest entry: %w", resp.Error())
	}

	values, err := resp.AsStrSlice()
	if err != nil {
		return 0, fmt.Errorf("parse oldest entry: %w", err)
	}
	if len(values) < 2 {
		return 0, nil
	}

	oldestScoreFloat, err := strconv.ParseFloat(values[1], 64)
	if err != nil {
		return 0, fmt.Errorf("parse oldest score: %w", err)
	}
	oldestMS := int64(oldestScoreFloat)

	retryMS := windowMS - (nowMS - oldestMS)
	if retryMS < 0 {
		retryMS = 0
	}

	return time.Duration(retryMS) * time.Millisecond, nil
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
