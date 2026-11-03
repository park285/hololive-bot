package xspaces

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

// 고정 SQL 자산은 패키지 초기화 때 한 번 읽는다. 자산 바이트(끝 공백 포함)를 그대로 보존한다.
var (
	candidateFailureSQL = mustSQL("candidate_failure.sql")
	loginLatestSQL      = mustSQL("login_latest.sql")
	observeSQL          = mustSQL("observe.sql")
	promoteSQL          = mustSQL("promote.sql")
	rejectSQL           = mustSQL("reject.sql")
	snapshotSQL         = mustSQL("snapshot.sql")
	statusSQL           = mustSQL("status.sql")
	submitSQL           = mustSQL("submit.sql")
)
