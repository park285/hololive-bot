package subscriptions

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	alarms, err := as.alarmRepository.FindByRoom(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
	}

	return alarms, nil
}
