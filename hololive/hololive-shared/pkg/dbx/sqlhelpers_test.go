// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dbx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type sqlHelperRow struct {
	ID    int    `db:"id"`
	Value string `db:"value"`
}

// jsonb ? 연산자와 문자열 리터럴 안의 '?'가 native placeholder와 함께 그대로 서버에 도달해야 한다.
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
