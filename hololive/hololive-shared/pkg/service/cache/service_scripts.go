package cache

import (
	"context"
	"log/slog"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

// compareAndDeleteScript: 원자적 compare-and-delete Lua 스크립트.
const compareAndDeleteScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
else
  return 0
end`

// compareAndDeleteLua는 EVALSHA를 먼저 보내고 NOSCRIPT일 때만 valkey-go가 본문 EVAL로 다시 보낸다.
var compareAndDeleteLua = valkey.NewLuaScript(compareAndDeleteScript)

// 분산 락의 안전한 해제에 사용됩니다.
func (c *Service) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	resp := compareAndDeleteLua.Exec(ctx, c.client, []string{key}, []string{expectedValue})

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
