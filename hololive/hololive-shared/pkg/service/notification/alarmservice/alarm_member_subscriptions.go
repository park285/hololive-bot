package alarmservice

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 채널 캐시는 방의 전체·멤버별 구독 합집합이므로 한 멤버 해지로 다른 구독을 지우면 안 된다.
func (as *AlarmService) refreshRoomChannelSubscriptions(ctx context.Context, roomID, channelID string) (bool, error) {
	alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepository, roomID)
	if err != nil {
		return false, fmt.Errorf("load remaining member subscriptions: %w", err)
	}

	aggregate := domain.Alarm{RoomID: roomID, ChannelID: channelID}

	for _, alarm := range alarms {
		if alarm == nil || alarm.ChannelID != channelID {
			continue
		}

		types, err := normalizeAlarmTypesStrict(alarm.AlarmTypes, domain.DefaultAlarmTypes)
		if err != nil {
			return false, fmt.Errorf("normalize remaining member subscription: %w", err)
		}

		aggregate.AlarmTypes = mergeAlarmTypes(aggregate.AlarmTypes, types)
	}

	removeTypes := subtractAlarmTypes(domain.AllAlarmTypes, aggregate.AlarmTypes)
	if _, err := as.removeAlarmFromCache(ctx, roomID, channelID, removeTypes, len(aggregate.AlarmTypes) == 0); err != nil {
		return false, fmt.Errorf("remove unused channel subscription types: %w", err)
	}

	if len(aggregate.AlarmTypes) > 0 {
		if _, err := as.cacheAlarm(ctx, &aggregate); err != nil {
			return false, fmt.Errorf("restore remaining channel subscriptions: %w", err)
		}
	}

	return true, nil
}
