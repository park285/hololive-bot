package cache

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

// compareAndDeleteScript: 원자적 compare-and-delete Lua 스크립트.
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
		c.logger.Error("Cache compare-and-delete failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return false, NewCacheError("cas", key, resp.Error())
	}

	result, err := resp.AsInt64()
	if err != nil {
		return false, NewCacheError("cas", key, err)
	}

	return result == 1, nil
}
