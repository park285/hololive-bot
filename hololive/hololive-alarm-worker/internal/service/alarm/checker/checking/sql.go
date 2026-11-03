package checking

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	loadConfirmedPremiereIDsSQL            = mustSQL("load_confirmed_premiere_ids.sql")
	loadOfficialCollaboTalentNamesSQL      = mustSQL("load_official_collabo_talent_names.sql")
	loadRecentLiveChannelIDsSQL            = mustSQL("load_recent_live_channel_ids.sql")
	queryPersistedRecentSessionsSQL        = mustSQL("query_persisted_recent_sessions.sql")
	queryRecentlySettledLiveStreamRoomsSQL = mustSQL("query_recently_settled_live_stream_rooms.sql")
	recentlyDispatchedStreamIDsSQL         = mustSQL("recently_dispatched_stream_ids.sql")
)
