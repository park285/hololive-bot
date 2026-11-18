package kakaoroom

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	getSQL    = mustSQL("get.sql")
	upsertSQL = mustSQL("upsert.sql")
)
