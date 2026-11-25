package dbx

import (
	"embed"

	"github.com/park285/shared-go/postgres/pgxdb"
)

//go:embed testdata/*.sql
var testSQLAssets embed.FS

var (
	testSelectOneSQL              = pgxdb.MustStatement(testSQLAssets, "testdata/select_one.sql")
	testStatementFirstSQL         = pgxdb.MustStatement(testSQLAssets, "testdata/first.sql")
	testStatementSecondSQL        = pgxdb.MustStatement(testSQLAssets, "testdata/second.sql")
	testStatementThirdSQL         = pgxdb.MustStatement(testSQLAssets, "testdata/third.sql")
	testStatementNumberedSQL      = pgxdb.MustStatement(testSQLAssets, "testdata/statement.sql")
	testZeroRowsSQL               = pgxdb.MustStatement(testSQLAssets, "testdata/zero_rows.sql")
	testSelectQuestionMarkRowsSQL = pgxdb.MustStatement(testSQLAssets, "testdata/select_question_mark_rows.sql")
	testInsertUnnestRowsSQL       = pgxdb.MustStatement(testSQLAssets, "testdata/insert_unnest_rows.sql")
	testGetRowByValueSQL          = pgxdb.MustStatement(testSQLAssets, "testdata/get_row_by_value.sql")
)
