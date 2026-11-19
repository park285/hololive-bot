package sharedcache

import (
	"context"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/reflectutil"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type valkeyStore struct {
	cache cache.LowLevelCache
}

func NewValkeyStore(client cache.LowLevelCache) Store {
	if reflectutil.IsNil(client) {
		return nil
	}

	return valkeyStore{cache: client}
}

func (s valkeyStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	data, err := s.cache.GetClient().Do(ctx, s.cache.B().Get().Key(key).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("get shared cache value: %w", err)
	}

	return data, true, nil
}

func (s valkeyStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	cmd := s.cache.B().Set().Key(key).Value(valkey.BinaryString(value)).Px(ttl).Build()
	if err := s.cache.GetClient().Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("set shared cache value: %w", err)
	}

	return nil
}
