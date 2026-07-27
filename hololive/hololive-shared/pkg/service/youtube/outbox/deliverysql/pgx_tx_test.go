package deliverysql

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/internal/dbx"
)

type recordingDeliveryRollbackTx struct {
	pgx.Tx
	rollbackCtxErr      error
	rollbackHasDeadline bool
	rollbackErr         error
}

func (tx *recordingDeliveryRollbackTx) Rollback(ctx context.Context) error {
	tx.rollbackCtxErr = ctx.Err()
	_, tx.rollbackHasDeadline = ctx.Deadline()
	return tx.rollbackErr
}

type panicDeliveryDB struct {
	dbx.Querier
	tx pgx.Tx
}

func (db *panicDeliveryDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return db.tx, nil
}

func TestInDeliveryTxPreservesPanicWhenRollbackFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &recordingDeliveryRollbackTx{rollbackErr: errors.New("rollback failed")}
	db := &panicDeliveryDB{tx: tx}
	panicValue := &struct{ message string }{message: "delivery panic"}

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		require.NoError(t, InDeliveryTx(ctx, db, func(dbx.Querier) error {
			panic(panicValue)
		}))
	}()

	require.Same(t, panicValue, recovered)
	require.NoError(t, tx.rollbackCtxErr)
	require.True(t, tx.rollbackHasDeadline)
}
