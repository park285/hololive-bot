package platformmap

import (
	"context"
	"fmt"

	"github.com/park285/shared-go/pkg/stringutil"
)

func (m *Mapper) removeOwnedTwitchLoginMappingsExcept(ctx context.Context, channelID, desiredLogin string) error {
	loginMap, err := m.cache.HGetAll(ctx, TwitchLoginMapKey)
	if err != nil {
		return fmt.Errorf("get twitch login mappings: %w", err)
	}

	for login, ownerChannelID := range loginMap {
		if !isStaleOwnedTwitchLoginMapping(login, ownerChannelID, channelID, desiredLogin) {
			continue
		}
		if err := m.cache.HDel(ctx, TwitchLoginMapKey, login); err != nil {
			return fmt.Errorf("delete stale twitch login mapping: %w", err)
		}
	}

	return nil
}

func isStaleOwnedTwitchLoginMapping(login, ownerChannelID, channelID, desiredLogin string) bool {
	normalizedLogin := stringutil.Normalize(login)
	if normalizedLogin == "" {
		return false
	}
	if stringutil.TrimSpace(ownerChannelID) != channelID {
		return false
	}
	return normalizedLogin != desiredLogin || login != normalizedLogin
}
