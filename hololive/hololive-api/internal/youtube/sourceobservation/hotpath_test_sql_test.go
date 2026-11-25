package sourceobservation

import (
	"embed"

	"github.com/park285/shared-go/postgres/pgxdb"
)

//go:embed testdata/*.sql
var testSQLAssets embed.FS

var (
	hotpathChunkRollbackSQL   = pgxdb.MustStatement(testSQLAssets, "testdata/hotpath_chunk_rollback.sql")
	hotpathBatchChildSQL      = pgxdb.MustStatement(testSQLAssets, "testdata/hotpath_batch_child.sql")
	hotpathBatchParentSQL     = pgxdb.MustStatement(testSQLAssets, "testdata/hotpath_batch_parent.sql")
	hotpathBatchRegressionSQL = pgxdb.MustStatement(testSQLAssets, "testdata/hotpath_batch_regression.sql")
)
