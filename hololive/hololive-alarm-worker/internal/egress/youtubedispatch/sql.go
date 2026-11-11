package youtubedispatch

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var loadOutboxItemsByIDsSQL = mustSQL("load_outbox_items_by_ids.sql")
