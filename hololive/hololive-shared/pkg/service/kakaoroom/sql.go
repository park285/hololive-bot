package kakaoroom

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	getSQL        = mustSQL("get.sql")
	upsertSQL     = mustSQL("upsert.sql")
	resumeSQL     = mustSQL("resume.sql")
	listPausesSQL = mustSQL("list_pauses.sql")
)
