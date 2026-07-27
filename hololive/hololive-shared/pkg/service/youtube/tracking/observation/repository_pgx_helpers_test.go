package observation

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type recordingTrackingRollbackTx struct {
	pgx.Tx
	rollbackCtxErr      error
	rollbackHasDeadline bool
	rollbackErr         error
}

func (tx *recordingTrackingRollbackTx) Rollback(ctx context.Context) error {
	tx.rollbackCtxErr = ctx.Err()
	_, tx.rollbackHasDeadline = ctx.Deadline()
	return tx.rollbackErr
}

type panicTrackingBeginner struct {
	trackingDB
	tx pgx.Tx
}

func (db *panicTrackingBeginner) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return db.tx, nil
}

func TestInPgxTxPreservesPanicWhenRollbackFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &recordingTrackingRollbackTx{rollbackErr: errors.New("rollback failed")}
	db := &panicTrackingBeginner{tx: tx}
	panicValue := &struct{ message string }{message: "tracking panic"}

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		require.NoError(t, inPgxTx(ctx, db, func(trackingDB) error {
			panic(panicValue)
		}))
	}()

	require.Same(t, panicValue, recovered)
	require.NoError(t, tx.rollbackCtxErr)
	require.True(t, tx.rollbackHasDeadline)
}
