package membernews

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	getRoomMembersSQL        = mustSQL("get_room_members.sql")
	isSubscribedSQL          = mustSQL("is_subscribed.sql")
	listSubscribedRoomsSQL   = mustSQL("list_subscribed_rooms.sql")
	repositoryQueryPeriodSQL = mustSQL("repository_query_period.sql")
	subscribeSQL             = mustSQL("subscribe.sql")
	unsubscribeSQL           = mustSQL("unsubscribe.sql")
)
