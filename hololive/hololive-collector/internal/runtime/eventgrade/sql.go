package eventgrade

import (
	"embed"

	"github.com/park285/shared-go/postgres/pgxdb"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var (
	claimCatalogSQL = pgxdb.MustStatement(sqlAssets, "queries/claim_catalog.sql")
	claimCutoffsSQL = pgxdb.MustStatement(sqlAssets, "queries/claim_cutoffs.sql")
	publishSQL      = pgxdb.MustStatement(sqlAssets, "queries/publish.sql")
	releaseSQL      = pgxdb.MustStatement(sqlAssets, "queries/release.sql")
	sessionUserSQL  = pgxdb.MustStatement(sqlAssets, "queries/session_user.sql")
)
