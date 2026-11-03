// Package sharedcache는 bot plane이 프로세스 밖에 두어도 되는 파생 cache 값을 읽고 쓴다.
// 값은 정본이 아니며 PostgreSQL 조회와 렌더 경로가 결과를 소유한다.
package sharedcache

import (
	"context"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/reflectutil"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// Store는 key별 TTL bytes 저장소다. 네트워크 client는 synctest bubble의 가짜 시간을 멈추게 하므로 bubble 시험은 메모리 구현을 쓴다.
type Store interface {
	// Get은 값이 있으면 true를, 없으면 false와 nil 오류를 돌려준다.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

type valkeyStore struct {
	cache cache.LowLevelCache
}

// NewValkeyStore는 Valkey 저장소를 만든다. 주입한 client가 없으면 nil을 돌려 호출자가 cache 없이 동작하게 한다.
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
