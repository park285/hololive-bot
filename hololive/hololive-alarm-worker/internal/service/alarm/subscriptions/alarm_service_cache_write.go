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

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

// 캐시 set이 유실되면 새 구독 하나만으로 부분 집합을 만들지 않는다.
// 기존 set의 증분 갱신만 원자적으로 허용하고, 누락은 기존 PG 조회 경로가 처리한다.
const addToExistingSubscriberSetScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then
  return -1
end
return redis.call('SADD', KEYS[1], ARGV[1])
`

func (as *AlarmService) cacheAlarm(ctx context.Context, record *domain.Alarm) error {
	if record == nil {
		return stdErrors.New("alarm is nil")
	}

	alarmTypes, err := normalizeAlarmTypesStrict(record.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		return fmt.Errorf("normalize alarm types strict: %w", err)
	}

	cacheRecord := *record

	cacheRecord.AlarmTypes = alarmTypes
	cacheRecord.MemberName = as.resolveCacheMemberName(ctx, cacheRecord.ChannelID)

	if err := as.cacheAlarmSequential(ctx, &cacheRecord); err != nil {
		return fmt.Errorf("cache alarm sequential: %w", err)
	}

	return nil
}

func (as *AlarmService) cacheAlarmSequential(ctx context.Context, record *domain.Alarm) error {
	registryKey := as.getRegistryKey(record.RoomID)
	if err := as.cacheAlarmSubscribersSequential(ctx, record, registryKey); err != nil {
		return fmt.Errorf("cache alarm subscribers sequential: %w", err)
	}

	if err := as.cacheAlarmChannelRegistry(ctx, record.ChannelID); err != nil {
		return fmt.Errorf("register alarm channel: %w", err)
	}

	if err := as.CacheMemberName(ctx, record.ChannelID, record.MemberName); err != nil {
		return fmt.Errorf("cache member name: %w", err)
	}

	if err := as.markAlarmCacheChanged(ctx); err != nil {
		return fmt.Errorf("mark alarm cache changed: %w", err)
	}

	return nil
}

func (as *AlarmService) cacheAlarmSubscribersSequential(ctx context.Context, record *domain.Alarm, registryKey string) error {
	builder := as.cache.Builder()
	commands := make([]valkey.Completed, 2*len(record.AlarmTypes))

	for i, alarmType := range record.AlarmTypes {
		subsKey := as.channelSubscribersKeyByType(record.ChannelID, alarmType)

		commands[2*i] = builder.Del().Key(sharedalarmkeys.BuildChannelSubscriberEmptyKey(record.ChannelID, alarmType)).Build()
		commands[2*i+1] = builder.Eval().Script(addToExistingSubscriberSetScript).Numkeys(1).Key(subsKey).Arg(registryKey).Build()
	}

	results := as.cache.DoMulti(ctx, commands...)
	if len(results) != len(commands) {
		return fmt.Errorf("add channel subscribers: unexpected result count: %d", len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("update channel subscriber type %s: %w", record.AlarmTypes[i/2], err)
		}
	}

	return nil
}

// 호출자는 구독 변경 mutex를 보유한다. 누락된 채널 registry는 DB 전체로 복구한다.
func (as *AlarmService) cacheAlarmChannelRegistry(ctx context.Context, channelID string) error {
	command := as.cache.Builder().Eval().Script(addToExistingSubscriberSetScript).
		Numkeys(1).Key(sharedalarmkeys.AlarmChannelRegistryKey).Arg(channelID).Build()
	results := as.cache.DoMulti(ctx, command)

	if len(results) != 1 {
		return fmt.Errorf("update channel registry: unexpected result count: %d", len(results))
	}

	added, err := results[0].AsInt64()
	if err != nil {
		return fmt.Errorf("update channel registry: %w", err)
	}

	if added < 0 {
		if _, err := rebuildSubscriberCacheFromRepository(ctx, as.cache, as.alarmRepository); err != nil {
			return fmt.Errorf("restore missing channel registry: %w", err)
		}
	}

	return nil
}

// markAlarmCacheChanged는 구독 변경 뒤 빈 cache 표식을 지워 target 조회가 subscriber set을 다시 읽게 한다.
func (as *AlarmService) markAlarmCacheChanged(ctx context.Context) error {
	if err := as.cache.Del(ctx, sharedalarmkeys.AlarmSubscriberCacheEmptyKey); err != nil {
		return fmt.Errorf("clear empty subscriber cache marker: %w", err)
	}

	return nil
}
