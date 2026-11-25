package template

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	rendererLoadSQL         = mustSQL("renderer_load.sql")
	rendererResolveSQL      = mustSQL("renderer_resolve.sql")
	rendererResolveBatchSQL = mustSQL("renderer_resolve_batch.sql")
)
