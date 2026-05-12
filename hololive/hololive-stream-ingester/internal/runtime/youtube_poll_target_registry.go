package runtime

import (
	"context"

	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type youTubePollRegistryVersionSource struct {
	cacheService cache.Client
}

func (s *youTubePollRegistryVersionSource) Version(ctx context.Context) (version int64, trusted bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			version = 0
			trusted = false
			err = nil
		}
	}()

	exists, err := s.cacheService.Exists(ctx, sharedalarmkeys.AlarmChannelRegistryVersionKey)
	if err != nil {
		return 0, false, err
	}
	if !exists {
		return 0, false, nil
	}

	if err := s.cacheService.Get(ctx, sharedalarmkeys.AlarmChannelRegistryVersionKey, &version); err != nil {
		return 0, false, err
	}
	if version <= 0 {
		return 0, false, nil
	}

	return version, true, nil
}
