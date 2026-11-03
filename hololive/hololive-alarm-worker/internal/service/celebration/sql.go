package celebration

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	findBirthdaySessionsSQL              = mustSQL("find_birthday_sessions.sql")
	findPublishedCelebrationEventsSQL    = mustSQL("find_published_celebration_events.sql")
	findSentRoomsByEventKeysSQL          = mustSQL("find_sent_rooms_by_event_keys.sql")
	listPublishedBirthdayStreamEventsSQL = mustSQL("list_published_birthday_stream_events.sql")
)
