package subscriptions

import (
	"context"
	stdErrors "errors"
	"fmt"

	"github.com/valkey-io/valkey-go"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
)

const addToExistingChannelRegistryScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then
  return -1
end
return redis.call('SADD', KEYS[1], ARGV[1])
`

var addToExistingChannelRegistryLua = valkey.NewLuaScript(addToExistingChannelRegistryScript)

func (as *AlarmService) cacheAlarmChannelRegistry(ctx context.Context, channelID string) error {
	registered, err := as.addToExistingChannelRegistry(ctx, channelID)
	if err != nil {
		return fmt.Errorf("register alarm channel: %w", err)
	}

	if registered {
		return nil
	}

	summary, err := rebuildSubscriberCacheFromRepository(ctx, as.cache, as.alarmRepository)
	if err != nil {
		return fmt.Errorf("restore missing channel registry: %w", err)
	}

	if summary.ChannelCount == 0 {
		if _, addErr := as.cache.SAdd(ctx, alarmkeys.AlarmChannelRegistryKey, []string{channelID}); addErr != nil {
			return fmt.Errorf("initialize empty channel registry: %w", addErr)
		}

		return nil
	}

	registered, err = as.addToExistingChannelRegistry(ctx, channelID)
	if err != nil {
		return fmt.Errorf("register channel after restore: %w", err)
	}

	if !registered {
		return stdErrors.New("channel registry evicted during restore")
	}

	return nil
}

func (as *AlarmService) addToExistingChannelRegistry(ctx context.Context, channelID string) (bool, error) {
	added, err := addToExistingChannelRegistryLua.Exec(ctx, as.cache.GetClient(),
		[]string{alarmkeys.AlarmChannelRegistryKey}, []string{channelID}).AsInt64()
	if err != nil {
		return false, fmt.Errorf("update channel registry: %w", err)
	}

	return added >= 0, nil
}

func (as *AlarmService) markAlarmCacheChanged(ctx context.Context) error {
	if err := as.cache.Del(ctx, alarmkeys.AlarmSubscriberCacheEmptyKey); err != nil {
		return fmt.Errorf("clear empty subscriber cache marker: %w", err)
	}

	return nil
}
