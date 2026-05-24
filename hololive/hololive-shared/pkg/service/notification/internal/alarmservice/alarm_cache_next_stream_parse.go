package alarmservice

import (
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) parseNextStreamInfo(channelID string, data map[string]string) *domain.NextStreamInfo {
	return as.cacheState.ParseNextStreamInfo(channelID, data)
}
