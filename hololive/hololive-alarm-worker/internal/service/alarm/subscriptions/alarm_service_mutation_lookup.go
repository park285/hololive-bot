package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var errAlarmRecordNotFound = errors.New("alarm record not found")

func (as *AlarmService) findAlarmRecordForMutation(ctx context.Context, roomID, channelID, hostID string) (*domain.Alarm, error) {
	roomID = strings.TrimSpace(roomID)
	channelID = strings.TrimSpace(channelID)

	if roomID == "" || channelID == "" {
		return nil, errAlarmRecordNotFound
	}

	alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepository, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
	}

	for _, alarm := range alarms {
		if alarm == nil || strings.TrimSpace(alarm.ChannelID) != channelID || alarm.HostID != hostID {
			continue
		}

		cloned := *alarm

		return &cloned, nil
	}

	return nil, errAlarmRecordNotFound
}

func (as *AlarmService) loadRoomAlarmsForMutation(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepository, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
	}

	return alarms, nil
}

func uniqueAlarmChannelIDs(alarms []*domain.Alarm) []string {
	if len(alarms) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(alarms))
	channelIDs := make([]string, 0, len(alarms))

	for _, alarm := range alarms {
		if alarm == nil || alarm.ChannelID == "" {
			continue
		}

		if _, ok := seen[alarm.ChannelID]; ok {
			continue
		}

		seen[alarm.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, alarm.ChannelID)
	}

	return channelIDs
}
