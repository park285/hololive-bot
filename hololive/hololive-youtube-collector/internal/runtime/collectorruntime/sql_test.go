package collectorruntime

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed testqueries/*
var testSQLAssets embed.FS

var mustTestSQL = sqlassets.MustReader(testSQLAssets, "testqueries")
