package subscriptions

import (
	"github.com/kapu/hololive-shared/pkg/domain"
)

type addAlarmMutation struct {
	record          *domain.Alarm
	newlyAddedTypes domain.AlarmTypes
}

type removeAlarmMutation struct {
	effectiveRemovalTypes domain.AlarmTypes
	remainingTypes        domain.AlarmTypes
	removeRoomChannel     bool
	updatedRecord         *domain.Alarm
}
