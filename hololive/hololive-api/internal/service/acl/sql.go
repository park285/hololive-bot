package acl

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	createRoomSQL    = mustSQL("create_room.sql")
	createSettingSQL = mustSQL("create_setting.sql")
	deleteRoomSQL    = mustSQL("delete_room.sql")
	getSettingSQL    = mustSQL("get_setting.sql")
	listRoomsSQL     = mustSQL("list_rooms.sql")
	upsertSettingSQL = mustSQL("upsert_setting.sql")
)
