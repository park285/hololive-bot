package cache

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"
)

// compareAndDeleteScript: 원자적 compare-and-delete Lua 스크립트
const compareAndDeleteScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
else
  return 0
end`

// 분산 락의 안전한 해제에 사용됩니다.
func (c *Service) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	cmd := c.client.B().Eval().Script(compareAndDeleteScript).Numkeys(1).Key(key).Arg(expectedValue).Build()
	resp := c.client.Do(ctx, cmd)
	if resp.Error() != nil {
		c.logger.Error("Cache compare-and-delete failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, NewCacheError("compare-and-delete failed", "cas", key, resp.Error())
	}

	result, err := resp.AsInt64()
	if err != nil {
		return false, NewCacheError("compare-and-delete conversion failed", "cas", key, err)
	}

	return result == 1, nil
}

// compareAndExpireScript: 원자적 compare-and-expire Lua 스크립트
const compareAndExpireScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('EXPIRE', KEYS[1], ARGV[2])
else
  return 0
end`

// 분산 락 renew 시 소유권 보장을 위해 사용됩니다.
func (c *Service) CompareAndExpire(ctx context.Context, key, expectedValue string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, fmt.Errorf("compare-and-expire: ttl must be greater than zero")
	}

	ttlSeconds := int64(math.Ceil(ttl.Seconds()))
	if ttlSeconds <= 0 {
		return false, fmt.Errorf("compare-and-expire: ttl seconds must be greater than zero")
	}

	cmd := c.client.B().Eval().
		Script(compareAndExpireScript).
		Numkeys(1).
		Key(key).
		Arg(expectedValue, strconv.FormatInt(ttlSeconds, 10)).
		Build()
	resp := c.client.Do(ctx, cmd)
	if resp.Error() != nil {
		c.logger.Error("Cache compare-and-expire failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, NewCacheError("compare-and-expire failed", "cas-expire", key, resp.Error())
	}

	result, err := resp.AsInt64()
	if err != nil {
		return false, NewCacheError("compare-and-expire conversion failed", "cas-expire", key, err)
	}

	return result == 1, nil
}
