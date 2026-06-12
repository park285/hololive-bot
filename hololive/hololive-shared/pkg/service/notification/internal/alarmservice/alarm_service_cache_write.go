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
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/valkey-io/valkey-go"
)

// 캐시 갱신은 의도적으로 비원자(sequential)다: alarm 키들은 hash tag가 없어 단일
// EVAL이 Cluster에서 cross-slot이 되고, 중간 실패의 부분 상태는 호출부의 repository
// rebuild와 마지막 version 키 갱신(reader 재구성 트리거)이 흡수한다.
func (as *AlarmService) cacheAlarm(ctx context.Context, record *domain.Alarm) (int64, error) {
	if record == nil {
		return 0, stdErrors.New("alarm is nil")
	}

	alarmTypes, err := normalizeAlarmTypesStrict(record.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		return 0, err
	}
	cacheRecord := *record
	cacheRecord.AlarmTypes = alarmTypes
	cacheRecord.MemberName = as.resolveCacheMemberName(ctx, cacheRecord.ChannelID, cacheRecord.MemberName)

	return as.cacheAlarmSequential(ctx, &cacheRecord)
}

func (as *AlarmService) cacheAlarmSequential(ctx context.Context, record *domain.Alarm) (int64, error) {
	alarmKey := as.getAlarmKey(record.RoomID)
	added, err := as.cache.SAdd(ctx, alarmKey, []string{record.ChannelID})
	if err != nil {
		return 0, fmt.Errorf("add room alarm: %w", err)
	}

	registryKey := as.getRegistryKey(record.RoomID)
	if _, err := as.cache.SAdd(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
		return 0, fmt.Errorf("add room registry: %w", err)
	}

	if err := as.cacheAlarmSubscribersSequential(ctx, record, registryKey); err != nil {
		return 0, err
	}

	if _, err := as.cache.SAdd(ctx, AlarmChannelRegistryKey, []string{record.ChannelID}); err != nil {
		return 0, fmt.Errorf("add channel registry: %w", err)
	}

	if err := as.cacheAlarmMetadataSequential(ctx, record); err != nil {
		return 0, err
	}

	if err := as.markAlarmCacheChanged(ctx); err != nil {
		return 0, fmt.Errorf("mark alarm cache changed: %w", err)
	}

	return added, nil
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

func (as *AlarmService) cacheAlarmMetadataSequential(ctx context.Context, record *domain.Alarm) error {
	if err := as.CacheMemberName(ctx, record.ChannelID, record.MemberName); err != nil {
		return fmt.Errorf("cache member name: %w", err)
	}

	if record.RoomName != "" {
		if err := as.cache.HSet(ctx, RoomNamesCacheKey, record.RoomID, record.RoomName); err != nil {
			return fmt.Errorf("cache room name: %w", err)
		}
	}

	if record.UserName != "" && record.UserID != "" {
		if err := as.cache.HSet(ctx, UserNamesCacheKey, record.UserID, record.UserName); err != nil {
			return fmt.Errorf("cache user name: %w", err)
		}
	}

	return nil
}

func (as *AlarmService) markAlarmCacheChanged(ctx context.Context) error {
	if err := as.cache.Del(ctx, AlarmSubscriberCacheEmptyKey); err != nil {
		return fmt.Errorf("clear empty subscriber cache marker: %w", err)
	}
	if err := as.cache.Set(ctx, AlarmChannelRegistryVersionKey, time.Now().UTC().UnixNano(), 0); err != nil {
		return fmt.Errorf("set channel registry version: %w", err)
	}

	return nil
}
