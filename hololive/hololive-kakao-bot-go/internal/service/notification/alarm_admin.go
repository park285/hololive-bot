package notification

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// GetAllAlarmKeys: 관리자 대시보드용 모든 알람 정보를 반환합니다.
func (as *AlarmService) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	registryKeys, err := as.cache.SMembers(ctx, AlarmRegistryKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get alarm registry: %w", err)
	}

	// 이름 맵 미리 로드
	roomNamesMap, _ := as.cache.HGetAll(ctx, RoomNamesCacheKey)

	alarms := make([]*domain.AlarmEntry, 0)

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
			memberName, _ := as.GetMemberName(ctx, channelID)

			roomName := roomNamesMap[roomID]
			if roomName == "" {
				roomName = roomID
			}

			alarms = append(alarms, &domain.AlarmEntry{
				RoomID:     roomID,
				RoomName:   roomName,
				ChannelID:  channelID,
				MemberName: memberName,
			})
		}
	}

	return alarms, nil
}

// GetDistinctRooms: 알람이 설정된 고유한 방 ID 목록을 반환합니다.
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
