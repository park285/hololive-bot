package dispatchrun

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type recordingAlarmDispatchRollbackTx struct {
	pgx.Tx
	rollbackCtxErr      error
	rollbackHasDeadline bool
	rollbackErr         error
}

func (tx *recordingAlarmDispatchRollbackTx) Rollback(ctx context.Context) error {
	tx.rollbackCtxErr = ctx.Err()
	_, tx.rollbackHasDeadline = ctx.Deadline()
	return tx.rollbackErr
}

func TestRollbackAlarmDispatchTxOnPanicPreservesPanicWhenRollbackFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &recordingAlarmDispatchRollbackTx{rollbackErr: errors.New("rollback failed")}
	panicValue := &struct{ message string }{message: "alarm dispatch panic"}

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		defer rollbackAlarmDispatchTxOnPanic(ctx, tx)
		panic(panicValue)
	}()

	require.Same(t, panicValue, recovered)
	require.NoError(t, tx.rollbackCtxErr)
	require.True(t, tx.rollbackHasDeadline)
}
