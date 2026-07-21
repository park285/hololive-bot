package batchrepo

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type recordingBatchRollbackTx struct {
	pgx.Tx
	rollbackCtxErr      error
	rollbackHasDeadline bool
	rollbackErr         error
}

func (tx *recordingBatchRollbackTx) Rollback(ctx context.Context) error {
	tx.rollbackCtxErr = ctx.Err()
	_, tx.rollbackHasDeadline = ctx.Deadline()
	return tx.rollbackErr
}

type panicBatchBeginner struct {
	batchDB
	tx pgx.Tx
}

func (db *panicBatchBeginner) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return db.tx, nil
}

func TestInBatchTxPreservesPanicWhenRollbackFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &recordingBatchRollbackTx{rollbackErr: errors.New("rollback failed")}
	db := &panicBatchBeginner{tx: tx}
	panicValue := &struct{ message string }{message: "batch panic"}

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_ = inBatchTx(ctx, db, func(batchDB) error {
			panic(panicValue)
		})
	}()

	require.Same(t, panicValue, recovered)
	require.NoError(t, tx.rollbackCtxErr)
	require.True(t, tx.rollbackHasDeadline)
}
