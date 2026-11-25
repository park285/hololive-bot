package observation

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/stretchr/testify/require"
)

type recordingTrackingTx struct {
	pgx.Tx

	commits   int
	rollbacks int
}

func (tx *recordingTrackingTx) Commit(context.Context) error {
	tx.commits++

	return nil
}

func (tx *recordingTrackingTx) Rollback(context.Context) error {
	tx.rollbacks++

	return nil
}

type queryOnlyTrackingDB struct {
	pgxdb.Querier
}

func TestInPgxTxJoinsCallerTransaction(t *testing.T) {
	tx := &recordingTrackingTx{}
	fnErr := errors.New("apply marks failed")

	var received pgxdb.Querier

	err := inPgxTx(t.Context(), tx, func(db pgxdb.Querier) error {
		received = db

		return fnErr
	})

	require.ErrorIs(t, err, fnErr)
	require.Same(t, tx, received)
	require.Zero(t, tx.commits)
	require.Zero(t, tx.rollbacks)
}

func TestInPgxTxRejectsUnsupportedDB(t *testing.T) {
	called := false

	err := inPgxTx(t.Context(), queryOnlyTrackingDB{}, func(pgxdb.Querier) error {
		called = true

		return nil
	})

	require.EqualError(t, err, "db does not support transactions")
	require.False(t, called)
}
