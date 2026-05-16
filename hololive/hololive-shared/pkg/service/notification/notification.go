package notification

import alarmservice "github.com/kapu/hololive-shared/pkg/service/notification/internal/alarmservice"

type AlarmService = alarmservice.AlarmService

type NotifiedData = alarmservice.NotifiedData

type UpcomingEventNotifiedData = alarmservice.UpcomingEventNotifiedData

const (
	AlarmKeyPrefix                 = alarmservice.AlarmKeyPrefix
	AlarmRegistryKey               = alarmservice.AlarmRegistryKey
	AlarmChannelRegistryKey        = alarmservice.AlarmChannelRegistryKey
	AlarmChannelRegistryVersionKey = alarmservice.AlarmChannelRegistryVersionKey
	AlarmSubscriberCacheEmptyKey   = alarmservice.AlarmSubscriberCacheEmptyKey
	ChannelSubscribersKeyPrefix    = alarmservice.ChannelSubscribersKeyPrefix
	ChzzkChannelMapKey             = alarmservice.ChzzkChannelMapKey
	ChzzkChannelMapEmptyKey        = alarmservice.ChzzkChannelMapEmptyKey
	TwitchLoginMapKey              = alarmservice.TwitchLoginMapKey
	TwitchLoginMapEmptyKey         = alarmservice.TwitchLoginMapEmptyKey
	TwitchChannelLoginMapKey       = alarmservice.TwitchChannelLoginMapKey
	TwitchChannelLoginMapEmptyKey  = alarmservice.TwitchChannelLoginMapEmptyKey
	MemberNameKey                  = alarmservice.MemberNameKey
	RoomNamesCacheKey              = alarmservice.RoomNamesCacheKey
	UserNamesCacheKey              = alarmservice.UserNamesCacheKey
	NotifiedKeyPrefix              = alarmservice.NotifiedKeyPrefix
	NotifyClaimKeyPrefix           = alarmservice.NotifyClaimKeyPrefix
	NotifyLogicalClaimKeyPrefix    = alarmservice.NotifyLogicalClaimKeyPrefix
	UpcomingEventKeyPrefix         = alarmservice.UpcomingEventKeyPrefix
	ScheduleTransitionKeyPrefix    = alarmservice.ScheduleTransitionKeyPrefix
	NextStreamKeyPrefix            = alarmservice.NextStreamKeyPrefix

	ChannelSubscribersCommunityPrefix = alarmservice.ChannelSubscribersCommunityPrefix
	ChannelSubscribersShortsPrefix    = alarmservice.ChannelSubscribersShortsPrefix

	ChzzkLiveNotifiedKeyPrefix  = alarmservice.ChzzkLiveNotifiedKeyPrefix
	IntegratedNotifiedKeyPrefix = alarmservice.IntegratedNotifiedKeyPrefix
)

var NewAlarmService = alarmservice.NewAlarmService

var CloseAllAlarmServices = alarmservice.CloseAllAlarmServices
