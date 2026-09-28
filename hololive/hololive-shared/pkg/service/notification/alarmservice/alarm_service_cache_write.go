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

package alarmservice

import (
	"context"
	stdErrors "errors"
	"fmt"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

// 캐시 갱신은 의도적으로 비원자(sequential)다: alarm 키들은 hash tag가 없어 단일
// EVAL이 Cluster에서 cross-slot이 되고, 중간 실패의 부분 상태는 호출부의 repository
// rebuild가 흡수한다.
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
	if cacheRecord.HostID != "" {
		cacheRecord.MemberName = ""
	}

	cacheRecord.MemberName = as.resolveCacheMemberName(ctx, cacheRecord.ChannelID, cacheRecord.MemberName)

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

	if _, err := as.cache.SAdd(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{record.ChannelID}); err != nil {
		return fmt.Errorf("add channel registry: %w", err)
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
	saddCmds := make([]valkey.Completed, len(record.AlarmTypes))

	for i, alarmType := range record.AlarmTypes {
		subsKey := as.channelSubscribersKeyByType(record.ChannelID, alarmType)

		saddCmds[i] = builder.Sadd().Key(subsKey).Member(registryKey).Build()
	}

	results := as.cache.DoMulti(ctx, saddCmds...)
	if len(results) != len(saddCmds) {
		return fmt.Errorf("add channel subscribers: unexpected result count: %d", len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("add channel subscriber type %s: %w", record.AlarmTypes[i], err)
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
