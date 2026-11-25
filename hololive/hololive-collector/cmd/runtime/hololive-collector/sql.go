package main

import (
	"embed"

	"github.com/park285/shared-go/postgres/pgxdb"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var (
	readOnlySessionLimitsSQL = pgxdb.MustStatement(sqlAssets, "queries/read_only_session_limits.sql")
	verifyDatabaseSessionSQL = pgxdb.MustStatement(sqlAssets, "queries/verify_database_session.sql")
)
