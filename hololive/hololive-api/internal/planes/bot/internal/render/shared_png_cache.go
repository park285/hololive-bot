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
