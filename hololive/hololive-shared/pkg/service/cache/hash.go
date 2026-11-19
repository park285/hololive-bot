package cache

import "context"

type HashReader interface {
	HGet(ctx context.Context, key, field string) (string, error)
	BatchHGet(ctx context.Context, key string, fields []string) (map[string]string, error)
	HGetAll(ctx context.Context, key string) (map[string]string, error)
}

type HashWriter interface {
	HSet(ctx context.Context, key, field, value string) error
	HMSet(ctx context.Context, key string, fields map[string]any) error
}

type HashCache interface {
	HashReader
	HashWriter
}
