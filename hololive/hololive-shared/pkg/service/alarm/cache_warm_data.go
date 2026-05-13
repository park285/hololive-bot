package alarm

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

type subscriberCacheWarmData struct {
	summary            CacheWarmSummary
	rooms              map[string]struct{}
	channels           map[string]struct{}
	roomAlarmMembers   map[string][]string
	channelSubscribers map[string][]string
	memberNames        map[string]string
	roomNames          map[string]string
	userNames          map[string]string
	registryRooms      []string
	channelRegistry    []string
}

func newSubscriberCacheWarmData(alarms []*domain.Alarm) *subscriberCacheWarmData {
	return &subscriberCacheWarmData{
		rooms:              make(map[string]struct{}, len(alarms)),
		channels:           make(map[string]struct{}, len(alarms)),
		roomAlarmMembers:   make(map[string][]string, len(alarms)),
		channelSubscribers: make(map[string][]string, len(alarms)),
		memberNames:        make(map[string]string, len(alarms)),
		roomNames:          make(map[string]string, len(alarms)),
		userNames:          make(map[string]string, len(alarms)),
		registryRooms:      make([]string, 0, len(alarms)),
		channelRegistry:    make([]string, 0, len(alarms)),
	}
}

func (data *subscriberCacheWarmData) addAlarm(alarmRecord *domain.Alarm) {
	roomID, channelID, ok := normalizedWarmAlarmIdentity(alarmRecord)
	if !ok {
		return
	}

	registryKey := alarmRecord.RegistryKey()
	data.addRoomAlarmMember(roomID, channelID)
	data.registryRooms = append(data.registryRooms, registryKey)
	data.channelRegistry = append(data.channelRegistry, channelID)
	data.addChannelSubscribers(channelID, registryKey, alarmRecord.AlarmTypes)
	data.addNames(alarmRecord, roomID, channelID)

	data.summary.AlarmCount++
	data.rooms[roomID] = struct{}{}
	data.channels[channelID] = struct{}{}
}

func (data *subscriberCacheWarmData) addRoomAlarmMember(roomID string, channelID string) {
	key := sharedalarmkeys.BuildRoomAlarmKey(roomID)
	data.roomAlarmMembers[key] = append(data.roomAlarmMembers[key], channelID)
}

func (data *subscriberCacheWarmData) addChannelSubscribers(channelID string, registryKey string, alarmTypes domain.AlarmTypes) {
	if len(alarmTypes) == 0 {
		alarmTypes = domain.DefaultAlarmTypes
	}
	for _, alarmType := range alarmTypes {
		key := sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)
		data.channelSubscribers[key] = append(data.channelSubscribers[key], registryKey)
	}
}

func (data *subscriberCacheWarmData) addNames(alarmRecord *domain.Alarm, roomID string, channelID string) {
	if alarmRecord.MemberName != "" {
		data.memberNames[channelID] = alarmRecord.MemberName
	}
	if alarmRecord.RoomName != "" {
		data.roomNames[roomID] = alarmRecord.RoomName
	}
	if alarmRecord.UserName != "" && alarmRecord.UserID != "" {
		data.userNames[alarmRecord.UserID] = alarmRecord.UserName
	}
}

func (data *subscriberCacheWarmData) finish() CacheWarmSummary {
	data.summary.RoomCount = len(data.rooms)
	data.summary.ChannelCount = len(data.channels)
	return data.summary
}
