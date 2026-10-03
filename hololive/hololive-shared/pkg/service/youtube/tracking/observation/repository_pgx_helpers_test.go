package observation

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
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
	trackingDB
}

// 호출자 트랜잭션에 합류할 때는 commit/rollback을 호출자에게 남기고 fn 오류만 감싸 돌려준다.
func TestInPgxTxJoinsCallerTransaction(t *testing.T) {
	tx := &recordingTrackingTx{}
	fnErr := errors.New("apply marks failed")

	var received trackingDB

	err := inPgxTx(t.Context(), tx, func(db trackingDB) error {
		received = db

		return fnErr
	})

	require.ErrorIs(t, err, fnErr)
	require.Same(t, tx, received)
	require.Zero(t, tx.commits)
	require.Zero(t, tx.rollbacks)
}

// pgx.Tx도 *pgxpool.Pool도 아닌 DB는 트랜잭션 없이 fn을 실행하지 않는다.
func TestInPgxTxRejectsUnsupportedDB(t *testing.T) {
	called := false

	err := inPgxTx(t.Context(), queryOnlyTrackingDB{}, func(trackingDB) error {
		called = true

		return nil
	})

	require.EqualError(t, err, "db does not support transactions")
	require.False(t, called)
}
