package acl

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL은 패키지 초기화 때 한 번만 읽는다. 없는 자산은 그 경로의 첫 호출이 아니라 기동에서 드러난다.
var (
	createRoomSQL    = mustSQL("create_room.sql")
	createSettingSQL = mustSQL("create_setting.sql")
	deleteRoomSQL    = mustSQL("delete_room.sql")
	getSettingSQL    = mustSQL("get_setting.sql")
	listRoomsSQL     = mustSQL("list_rooms.sql")
	upsertSettingSQL = mustSQL("upsert_setting.sql")
)
