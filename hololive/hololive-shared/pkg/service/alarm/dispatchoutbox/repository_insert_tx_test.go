package dispatchoutbox

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type recordingDispatchTx struct {
	pgx.Tx
	rollbackCtxErr      error
	rollbackHasDeadline bool
	rollbackErr         error
}

func (tx *recordingDispatchTx) Rollback(ctx context.Context) error {
	tx.rollbackCtxErr = ctx.Err()
	_, tx.rollbackHasDeadline = ctx.Deadline()
	return tx.rollbackErr
}

func TestFinishDispatchBatchRollsBackCanceledRequestOnPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &recordingDispatchTx{}
	panicValue := errors.New("dispatch panic")

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		var err error
		defer finishDispatchBatch(ctx, tx, &err)
		panic(panicValue)
	}()

	require.Same(t, panicValue, recovered)
	require.NoError(t, tx.rollbackCtxErr)
	require.True(t, tx.rollbackHasDeadline)
}

func TestFinishDispatchBatchPreservesPanicWhenRollbackFails(t *testing.T) {
	tx := &recordingDispatchTx{rollbackErr: errors.New("rollback failed")}
	panicValue := errors.New("dispatch panic")

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		var err error
		defer finishDispatchBatch(context.Background(), tx, &err)
		panic(panicValue)
	}()

	require.Same(t, panicValue, recovered)
	require.True(t, tx.rollbackHasDeadline)
}

func TestFinishDispatchBatchRollsBackCanceledRequestOnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &recordingDispatchTx{}
	want := errors.New("insert failed")

	got := func() (err error) {
		err = want
		defer finishDispatchBatch(ctx, tx, &err)
		return err
	}()

	require.ErrorIs(t, got, want)
	require.NoError(t, tx.rollbackCtxErr)
	require.True(t, tx.rollbackHasDeadline)
}
