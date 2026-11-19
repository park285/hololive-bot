package xspaces

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	candidateFailureSQL = mustSQL("candidate_failure.sql")
	observeSQL          = mustSQL("observe.sql")
	promoteSQL          = mustSQL("promote.sql")
	rejectSQL           = mustSQL("reject.sql")
	snapshotSQL         = mustSQL("snapshot.sql")
	statusSQL           = mustSQL("status.sql")
	submitSQL           = mustSQL("submit.sql")
)
