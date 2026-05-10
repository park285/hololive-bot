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
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	registryKeys, err := as.cache.SMembers(ctx, AlarmRegistryKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get alarm registry: %w", err)
	}

	// 이름 맵 미리 로드
	roomNamesMap, _ := as.cache.HGetAll(ctx, RoomNamesCacheKey)

	alarms := make([]*domain.AlarmEntry, 0)
	channelIDsForNames := make([]string, 0)

	// 방 기반: registry key = roomID
	for _, roomID := range registryKeys {
		if roomID == "" {
			continue
		}

		alarmKey := as.getAlarmKey(roomID)

		channelIDs, err := as.cache.SMembers(ctx, alarmKey)
		if err != nil {
			continue
		}

		for _, channelID := range channelIDs {
			roomName := roomNamesMap[roomID]
			if roomName == "" {
				roomName = roomID
			}

			channelIDsForNames = append(channelIDsForNames, channelID)
			alarms = append(alarms, &domain.AlarmEntry{
				RoomID:    roomID,
				RoomName:  roomName,
				ChannelID: channelID,
			})
		}
	}

	memberNames, _ := as.getMemberNamesBatch(ctx, channelIDsForNames)
	for _, alarm := range alarms {
		alarm.MemberName = memberNames[alarm.ChannelID]
	}

	return alarms, nil
}

func (as *AlarmService) GetDistinctRooms(ctx context.Context) ([]string, error) {
	// 방 기반: registry key = roomID (추가 파싱 불필요)
	registryKeys, err := as.cache.SMembers(ctx, AlarmRegistryKey)
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
