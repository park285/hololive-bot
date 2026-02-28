package notification

import (
	"github.com/kapu/hololive-shared/pkg/domain"
)

// getAlarmKey: 방 기반 알람 키 (room_id가 PRIMARY)
func (as *AlarmService) getAlarmKey(roomID string) string {
	return AlarmKeyPrefix + roomID
}

// getRegistryKey: 방 기반 레지스트리 키 (room_id가 PRIMARY)
func (as *AlarmService) getRegistryKey(roomID string) string {
	return roomID
}

func (as *AlarmService) channelSubscribersKey(channelID string) string {
	return ChannelSubscribersKeyPrefix + channelID
}

func (as *AlarmService) channelSubscribersKeyByType(channelID string, alarmType domain.AlarmType) string {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		return ChannelSubscribersCommunityPrefix + channelID
	case domain.AlarmTypeShorts:
		return ChannelSubscribersShortsPrefix + channelID
	default:
		return ChannelSubscribersKeyPrefix + channelID
	}
}
