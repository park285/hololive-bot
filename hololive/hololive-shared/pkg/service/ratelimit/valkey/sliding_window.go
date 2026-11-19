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

var allowLua = valkeygo.NewLuaScript(allowScript)

type Decision struct {
	Allowed    bool
	Current    int
	Remaining  int
	Limit      int
	Window     time.Duration
	RetryAfter time.Duration
}

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

func (l *SlidingWindowLimiter) Allow(ctx context.Context, bucket string, limit int, window time.Duration) (Decision, error) {
	if bucket == "" {
		return Decision{}, errors.New("allow: bucket must not be empty")
	}

	if limit <= 0 {
		return Decision{}, errors.New("allow: limit must be greater than zero")
	}

	if window <= 0 {
		return Decision{}, errors.New("allow: window must be greater than zero")
	}

	windowMS := window.Milliseconds()
	if windowMS <= 0 {
		return Decision{}, errors.New("allow: window milliseconds must be greater than zero")
	}

	key := l.buildKey(bucket)
	nowMS := l.nowFunc()().UnixMilli()
	member := l.memberID(nowMS)
	ttlSeconds := ttlSecondsFromWindow(window)

	allowed, current, retryAfter, err := l.allowByScript(ctx, key, nowMS, windowMS, limit, member, ttlSeconds)
	if err != nil {
		return Decision{}, fmt.Errorf("allow: evaluate script: %w", err)
	}

	remaining := max(0, limit-current)

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
	result := allowLua.Exec(ctx, l.cacheClient.GetClient(), []string{key}, []string{
		strconv.FormatInt(nowMS, 10),
		strconv.FormatInt(windowMS, 10),
		strconv.Itoa(limit),
		member,
		strconv.FormatInt(ttlSeconds, 10),
	})
	if err := result.Error(); err != nil {
		return false, 0, 0, fmt.Errorf("eval allow script: %w", err)
	}

	out1, out2, out3, err := parseAllowScriptResult(result)
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
