package cache

import "context"

type SetCache interface {
	SAdd(ctx context.Context, key string, members []string) (int64, error)
	SRem(ctx context.Context, key string, members []string) (int64, error)
	SMembers(ctx context.Context, key string) ([]string, error)
}
