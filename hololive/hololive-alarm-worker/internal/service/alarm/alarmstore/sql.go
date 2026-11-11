package alarmstore

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	addSQL                                   = mustSQL("add.sql")
	clearByRoomSQL                           = mustSQL("clear_by_room.sql")
	countAlarmEntriesSQL                     = mustSQL("count_alarm_entries.sql")
	deleteRoomDisplayNameSQL                 = mustSQL("delete_room_display_name.sql")
	findByChannelAndTypeSQL                  = mustSQL("find_by_channel_and_type.sql")
	findByChannelSQL                         = mustSQL("find_by_channel.sql")
	findByRoomSQL                            = mustSQL("find_by_room.sql")
	getAllDistinctRoomIDsSQL                 = mustSQL("get_all_distinct_room_ids.sql")
	getMemberNameSQL                         = mustSQL("get_member_name.sql")
	listAlarmEntriesSQL                      = mustSQL("list_alarm_entries.sql")
	loadAllSQL                               = mustSQL("load_all.sql")
	loadChannelSubscriberAlarmsByChannelsSQL = mustSQL("load_channel_subscriber_alarms_by_channels.sql")
	removeSubscriptionSQL                    = mustSQL("remove_subscription.sql")
	repositoryChannelSubscriptionsExistSQL   = mustSQL("repository_channel_subscriptions_exist.sql")
	setRoomDisplayNameSQL                    = mustSQL("set_room_display_name.sql")
)
