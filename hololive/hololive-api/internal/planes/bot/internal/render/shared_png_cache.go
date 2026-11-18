package render

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/sharedcache"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

const sharedPNGCacheMaxBytes = 8 << 20

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

type sharedRenderCache struct {
	store sharedcache.Store
	ttl   time.Duration
	event string
}

// sharedPNGCache는 렌더한 PNG를 입력 해시 뒤에 붙여 공유 cache(Valkey)에 둔다. 값의 해시가 지금 입력과 다르면 miss로 보고
// 다시 렌더해 같은 key를 덮어쓴다. 읽기 중 cache 오류도 miss로 보고 렌더 실패로 올리지 않는다.
type sharedPNGCache sharedRenderCache

func (c sharedPNGCache) get(ctx context.Context, key, hash string) ([]byte, bool) {
	data, ok := sharedRenderCache(c).read(ctx, key)
	if !ok || len(data) > len(hash)+sharedPNGCacheMaxBytes {
		return nil, false
	}

	image, found := bytes.CutPrefix(data, []byte(hash))
	if !found || !isPNGData(image) {
		return nil, false
	}

	return image, true
}

func (c sharedPNGCache) put(ctx context.Context, key, hash string, data []byte) {
	if c.store == nil || !isPNGData(data) || len(data) > sharedPNGCacheMaxBytes {
		return
	}

	sharedRenderCache(c).write(ctx, key, slices.Concat([]byte(hash), data))
}

func (c sharedRenderCache) read(ctx context.Context, key string) ([]byte, bool) {
	if c.store == nil {
		return nil, false
	}

	data, ok, err := c.store.Get(ctx, key)
	if err != nil {
		c.logFailure(ctx, "read", key, err)

		return nil, false
	}

	return data, ok
}

func (c sharedRenderCache) write(ctx context.Context, key string, data []byte) {
	if c.store == nil {
		return
	}

	if err := c.store.Set(ctx, key, data, c.ttl); err != nil {
		c.logFailure(ctx, "write", key, err)
	}
}

// 호출자 취소·기한 만료로 끝난 cache 연산은 Valkey 장애가 아니므로 경고로 남기지 않는다.
func (c sharedRenderCache) logFailure(ctx context.Context, operation, key string, err error) {
	if ctx.Err() != nil {
		return
	}

	slog.Default().Warn(c.event,
		slog.String("operation", operation),
		privacylog.CacheKeyAttr(key),
		slog.Any("error", err),
	)
}

func isPNGData(data []byte) bool {
	return bytes.HasPrefix(data, pngSignature)
}
