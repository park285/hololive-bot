package livequery

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var snapshotSQL = mustSQL("snapshot.sql")
