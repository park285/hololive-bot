package dbx

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	sessionAdvisoryLockAcquireSQL = mustSQL("session_advisory_lock_acquire.sql")
	sessionAdvisoryLockReleaseSQL = mustSQL("session_advisory_lock_release.sql")
)
