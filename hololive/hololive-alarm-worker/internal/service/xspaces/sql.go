package xspaces

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	insertStartSQL = mustSQL("insert_start.sql")
	loadStartSQL   = mustSQL("load_start.sql")
	pruneStartsSQL = mustSQL("prune_starts.sql")
)
