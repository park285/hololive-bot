// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package subscriptions

import (
	"context"
	stdErrors "errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

// 채널 registry가 유실되면 일부 채널만으로 재생성하지 않고 먼저 DB 전체를 복구한다.
const addToExistingChannelRegistryScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then
  return -1
end
return redis.call('SADD', KEYS[1], ARGV[1])
`

func (as *AlarmService) cacheAlarm(ctx context.Context, record *domain.Alarm) error {
	if record == nil {
		return stdErrors.New("alarm is nil")
	}

	if err := as.CacheMemberName(ctx, record.ChannelID, as.resolveCacheMemberName(ctx, record.ChannelID)); err != nil {
		return fmt.Errorf("cache alarm member name: %w", err)
	}

	return nil
}

// 호출자는 mutation mutex를 보유한다. DB가 비어 있었던 경우에만 singleton registry를 만들 수 있다.
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
		if _, addErr := as.cache.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{channelID}); addErr != nil {
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
	command := as.cache.Builder().Eval().Script(addToExistingChannelRegistryScript).
		Numkeys(1).Key(sharedalarmkeys.AlarmChannelRegistryKey).Arg(channelID).Build()
	results := as.cache.DoMulti(ctx, command)

	if len(results) != 1 {
		return false, fmt.Errorf("update channel registry: unexpected result count: %d", len(results))
	}

	added, err := results[0].AsInt64()
	if err != nil {
		return false, fmt.Errorf("update channel registry: %w", err)
	}

	return added >= 0, nil
}

// markAlarmCacheChanged는 구독 변경 뒤 빈 cache 표식을 지워 target 조회가 subscriber set을 다시 읽게 한다.
func (as *AlarmService) markAlarmCacheChanged(ctx context.Context) error {
	if err := as.cache.Del(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey); err != nil {
		return fmt.Errorf("clear empty subscriber cache marker: %w", err)
	}

	return nil
}
