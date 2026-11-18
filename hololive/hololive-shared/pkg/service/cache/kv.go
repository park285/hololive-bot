package cache

import (
	"context"
	"time"
)

type KeyValueReader interface {
	Get(ctx context.Context, key string, dest any) error
	Exists(ctx context.Context, key string) (bool, error)
	ScanKeyPages(ctx context.Context, pattern string, batchSize int64, visit func(keys []string) error) error
}

type KeyValueWriter interface {
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
}

type KeyValueLifecycle interface {
	Del(ctx context.Context, key string) error
	DelMany(ctx context.Context, keys []string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

type KeyValueCache interface {
	KeyValueReader
	KeyValueWriter
	KeyValueLifecycle
}
