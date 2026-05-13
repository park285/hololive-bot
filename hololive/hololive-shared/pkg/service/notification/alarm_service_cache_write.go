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

package notification

import (
	"context"
	stdErrors "errors"
	"fmt"
	"strconv"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/valkey-io/valkey-go"
)

const cacheAlarmAtomicScript = `
local roomAlarmKey = ARGV[1]
local alarmRegistryKey = ARGV[2]
local channelRegistryKey = ARGV[3]
local memberNameKey = ARGV[4]
local roomNamesKey = ARGV[5]
local userNamesKey = ARGV[6]
local roomID = ARGV[7]
local channelID = ARGV[8]
local memberName = ARGV[9]
local roomName = ARGV[10]
local userID = ARGV[11]
local userName = ARGV[12]
local registryKey = ARGV[13]
local emptySubscriberCacheKey = ARGV[14]
local channelRegistryVersionKey = ARGV[15]
local channelRegistryVersionValue = ARGV[16]

local added = redis.call('SADD', roomAlarmKey, channelID)
redis.call('SADD', alarmRegistryKey, registryKey)
redis.call('SADD', channelRegistryKey, channelID)

if memberName ~= '' then
  redis.call('HSET', memberNameKey, channelID, memberName)
end
if roomID ~= '' and roomName ~= '' then
  redis.call('HSET', roomNamesKey, roomID, roomName)
end
if userID ~= '' and userName ~= '' then
  redis.call('HSET', userNamesKey, userID, userName)
end

for i = 17, #ARGV do
  redis.call('SADD', ARGV[i], registryKey)
end
redis.call('DEL', emptySubscriberCacheKey)
redis.call('SET', channelRegistryVersionKey, channelRegistryVersionValue)

return added
`

func (as *AlarmService) cacheAlarm(ctx context.Context, record *domain.Alarm) (int64, error) {
	if record == nil {
		return 0, stdErrors.New("alarm is nil")
	}

	alarmTypes, err := normalizeAlarmTypesStrict(record.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		return 0, err
	}
	record.AlarmTypes = alarmTypes

	added, err := as.cacheAlarmAtomic(ctx, record)
	if err == nil {
		return added, nil
	}

	return 0, err
}

func (as *AlarmService) cacheAlarmAtomic(ctx context.Context, record *domain.Alarm) (int64, error) {
	client, builder, ok := as.rawAlarmCacheEvalClient()
	if !ok {
		return as.cacheAlarmSequential(ctx, record)
	}

	registryKey := as.getRegistryKey(record.RoomID)
	args := []string{
		as.getAlarmKey(record.RoomID),
		AlarmRegistryKey,
		AlarmChannelRegistryKey,
		MemberNameKey,
		RoomNamesCacheKey,
		UserNamesCacheKey,
		record.RoomID,
		record.ChannelID,
		record.MemberName,
		record.RoomName,
		record.UserID,
		record.UserName,
		registryKey,
		AlarmSubscriberCacheEmptyKey,
		AlarmChannelRegistryVersionKey,
		alarmCacheMutationVersion(),
	}
	for _, alarmType := range record.AlarmTypes {
		args = append(args, as.channelSubscribersKeyByType(record.ChannelID, alarmType))
	}

	resp := client.Do(ctx, builder.Eval().Script(cacheAlarmAtomicScript).Numkeys(0).Arg(args...).Build())
	added, err := resp.AsInt64()
	if err != nil {
		return 0, fmt.Errorf("atomic cache alarm: %w", err)
	}

	return added, nil
}

func (as *AlarmService) rawAlarmCacheEvalClient() (_ valkey.Client, _ valkey.Builder, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	client := as.cache.GetClient()
	builder := as.cache.B()
	if client == nil {
		return nil, valkey.Builder{}, false
	}

	return client, builder, true
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

func alarmCacheMutationVersion() string {
	return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
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
