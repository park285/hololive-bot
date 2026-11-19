package subscriptions

import (
	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) channelSubscribersKeyByType(channelID string, alarmType domain.AlarmType) string {
	return alarmkeys.BuildChannelSubscriberKey(channelID, alarmType)
}
