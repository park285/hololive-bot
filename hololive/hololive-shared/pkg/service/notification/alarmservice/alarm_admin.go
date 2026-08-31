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
	"fmt"
	"log/slog"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

const alarmRoomMembershipBatchSize = 100

func (as *AlarmService) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	registryKeys, err := as.cache.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get alarm registry: %w", err)
	}

	// 이름 맵 미리 로드
	roomNamesMap, err := as.cache.HGetAll(ctx, sharedalarmkeys.RoomNamesCacheKey)
	if err != nil {
		if as.logger != nil {
			as.logger.Warn("failed to load cached room names", slog.Any("error", err))
		}

		roomNamesMap = map[string]string{}
	}

	roomIDs, roomChannels := as.loadRoomAlarmChannels(ctx, registryKeys)
	alarms, channelIDsForNames := collectAlarmEntries(roomIDs, roomChannels, roomNamesMap)

	memberNames, err := as.getMemberNamesBatch(ctx, channelIDsForNames)
	if err != nil {
		if as.logger != nil {
			as.logger.Warn("failed to load member names", slog.Any("error", err))
		}

		memberNames = map[string]string{}
	}

	for _, alarm := range alarms {
		alarm.MemberName = memberNames[alarm.ChannelID]
	}

	return alarms, nil
}

func (as *AlarmService) loadRoomAlarmChannels(ctx context.Context, registryKeys []string) ([]string, [][]string) {
	roomIDs := make([]string, 0, len(registryKeys))
	for _, roomID := range registryKeys {
		if roomID == "" {
			continue
		}

		roomIDs = append(roomIDs, roomID)
	}

	if len(roomIDs) == 0 {
		return roomIDs, nil
	}

	builder := as.cache.B()
	roomChannels := make([][]string, len(roomIDs))

	for batchStart := 0; batchStart < len(roomIDs); batchStart += alarmRoomMembershipBatchSize {
		batchEnd := min(batchStart+alarmRoomMembershipBatchSize, len(roomIDs))
		commands := make([]valkey.Completed, batchEnd-batchStart)

		for index, roomID := range roomIDs[batchStart:batchEnd] {
			commands[index] = builder.Smembers().Key(as.getAlarmKey(roomID)).Build()
		}

		results := as.cache.DoMulti(ctx, commands...)
		for index, result := range results {
			if index >= len(commands) {
				break
			}

			channelIDs, err := result.AsStrSlice()
			if err != nil {
				continue
			}

			roomChannels[batchStart+index] = channelIDs
		}
	}

	return roomIDs, roomChannels
}

func collectAlarmEntries(
	roomIDs []string,
	roomChannels [][]string,
	roomNamesMap map[string]string,
) (result0 []*domain.AlarmEntry, result1 []string) {
	alarms := make([]*domain.AlarmEntry, 0)
	channelIDsForNames := make([]string, 0)

	for index, roomID := range roomIDs {
		channelIDs := roomChannels[index]
		roomAlarms := buildRoomAlarmEntries(roomID, roomNamesMap[roomID], channelIDs)

		channelIDsForNames = append(channelIDsForNames, channelIDs...)
		alarms = append(alarms, roomAlarms...)
	}

	return alarms, channelIDsForNames
}

func buildRoomAlarmEntries(roomID, roomName string, channelIDs []string) []*domain.AlarmEntry {
	if roomName == "" {
		roomName = roomID
	}

	alarms := make([]*domain.AlarmEntry, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		alarms = append(alarms, &domain.AlarmEntry{
			RoomID:    roomID,
			RoomName:  roomName,
			ChannelID: channelID,
		})
	}

	return alarms
}

func (as *AlarmService) GetDistinctRooms(ctx context.Context) ([]string, error) {
	// 방 기반: registry key = roomID (추가 파싱 불필요)
	registryKeys, err := as.cache.SMembers(ctx, sharedalarmkeys.AlarmRegistryKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get alarm registry: %w", err)
	}

	rooms := make([]string, 0, len(registryKeys))
	for _, roomID := range registryKeys {
		if roomID != "" {
			rooms = append(rooms, roomID)
		}
	}

	return rooms, nil
}
