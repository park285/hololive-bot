package alarm

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

type subscriberCacheWarmData struct {
	summary            CacheWarmSummary
	rooms              map[string]struct{}
	channels           map[string]struct{}
	channelSubscribers map[string][]string
	memberNames        map[string]string
	channelRegistry    []string
}

func newSubscriberCacheWarmData(alarms []*domain.Alarm) *subscriberCacheWarmData {
	return &subscriberCacheWarmData{
		rooms:              make(map[string]struct{}, len(alarms)),
		channels:           make(map[string]struct{}, len(alarms)),
		channelSubscribers: make(map[string][]string, len(alarms)),
		memberNames:        make(map[string]string, len(alarms)),
		channelRegistry:    make([]string, 0, len(alarms)),
	}
}

func (data *subscriberCacheWarmData) addAlarm(alarmRecord *domain.Alarm) {
	roomID, channelID, ok := normalizedWarmAlarmIdentity(alarmRecord)
	if !ok {
		return
	}

	data.channelRegistry = append(data.channelRegistry, channelID)
	data.addChannelSubscribers(channelID, alarmRecord.RegistryKey(), alarmRecord.AlarmTypes)

	if alarmRecord.HostID == "" && alarmRecord.MemberName != "" {
		data.memberNames[channelID] = alarmRecord.MemberName
	}

	data.summary.AlarmCount++

	data.rooms[roomID] = struct{}{}
	data.channels[channelID] = struct{}{}
}

func (data *subscriberCacheWarmData) addChannelSubscribers(channelID, registryKey string, alarmTypes domain.AlarmTypes) {
	if len(alarmTypes) == 0 {
		alarmTypes = domain.DefaultAlarmTypes
	}

	for _, alarmType := range alarmTypes {
		key := sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)

		data.channelSubscribers[key] = append(data.channelSubscribers[key], registryKey)
	}
}

func (data *subscriberCacheWarmData) finish() CacheWarmSummary {
	data.summary.RoomCount = len(data.rooms)
	data.summary.ChannelCount = len(data.channels)

	return data.summary
}
