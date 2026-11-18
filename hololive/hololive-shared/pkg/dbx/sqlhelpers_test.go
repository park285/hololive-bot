package dbx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type sqlHelperRow struct {
	ID    int    `db:"id"`
	Value string `db:"value"`
}

func TestSelectSQLKeepsQuestionMarkOperatorsWithNativePlaceholders(t *testing.T) {
	ctx := t.Context()
	pool := newTxTestPool(t)

	_, err := pool.Exec(ctx, `INSERT INTO dbx_tx_test (value) VALUES ('a?'), ('b'), ('c?')`)
	require.NoError(t, err)

	var rows []sqlHelperRow

	err = SelectSQL(ctx, pool, &rows, "select question mark rows", `
		SELECT id, value
		FROM dbx_tx_test
		WHERE jsonb_build_object('k', 1) ? 'k'
		  AND value LIKE '%?'
		  AND value = ANY($1::text[])
		ORDER BY id
	`, []string{"a?", "b", "c?"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "a?", rows[0].Value)
	require.Equal(t, "c?", rows[1].Value)
}

func TestExecSQLAndGetSQLUseNativePlaceholders(t *testing.T) {
	ctx := t.Context()
	pool := newTxTestPool(t)

	affected, err := ExecSQL(ctx, pool, "insert rows", `INSERT INTO dbx_tx_test (value) SELECT unnest($1::text[])`, []string{"x", "y"})
	require.NoError(t, err)
	require.Equal(t, int64(2), affected)

	var row sqlHelperRow

	found, err := GetSQL(ctx, pool, &row, "get row", `SELECT id, value FROM dbx_tx_test WHERE value = $1`, "y")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "y", row.Value)

	found, err = GetSQL(ctx, pool, &row, "get missing row", `SELECT id, value FROM dbx_tx_test WHERE value = $1`, "missing")
	require.NoError(t, err)
	require.False(t, found)
}
