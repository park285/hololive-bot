package durabilitytest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/park285/shared-go/sqlite/sqlitedb"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
)

type Fixture struct {
	Store *durability.Store
	DB    *sql.DB
	Path  string
}

func Open(tb testing.TB) Fixture {
	tb.Helper()

	path := filepath.Join(tb.TempDir(), "durable", "bot-durable.db")
	opts := durability.StoreOptions(path, nil)

	store, err := durability.OpenStore(tb.Context(), opts)
	require.NoError(tb, err)

	raw, err := sqlitedb.Attach(tb.Context(), opts)
	require.NoError(tb, err)

	tb.Cleanup(func() {
		closeCtx := context.WithoutCancel(tb.Context())
		require.NoError(tb, raw.Close(closeCtx))
		require.NoError(tb, store.Close(closeCtx))
	})

	return Fixture{Store: store, DB: raw.Writer(), Path: path}
}
