package cache

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// GetStreams: 캐시된 방송 목록을 조회합니다.
func (c *Service) GetStreams(ctx context.Context, key string) ([]*domain.Stream, bool) {
	var streams []*domain.Stream
	if err := c.Get(ctx, key, &streams); err != nil {
		c.logger.Debug("Cache miss or error", slog.String("key", key))
		return nil, false
	}

	if streams == nil {
		return nil, false
	}

	return streams, true
}

// SetStreams: 방송 목록을 캐시에 저장한다. (TTL 적용)
func (c *Service) SetStreams(ctx context.Context, key string, streams []*domain.Stream, ttl time.Duration) {
	if err := c.Set(ctx, key, streams, ttl); err != nil {
		c.logger.Error("Failed to cache streams", slog.String("key", key), slog.Any("error", err))
	}
}
