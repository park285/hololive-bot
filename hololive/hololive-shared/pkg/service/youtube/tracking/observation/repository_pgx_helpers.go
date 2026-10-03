package observation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

// inPgxTx: 호출자가 연 pgx.Tx에는 그대로 합류해 commit/rollback을 호출자에게 남긴다.
// *pgxpool.Pool이면 트랜잭션 수명주기(panic rollback, 실패 rollback, commit)를 dbx.InPgxTx가 소유한다.
func inPgxTx(ctx context.Context, db trackingDB, fn func(tx trackingDB) error) error {
	switch typed := db.(type) {
	case pgx.Tx:
		if err := fn(typed); err != nil {
			return fmt.Errorf("fn: %w", err)
		}

		return nil
	case *pgxpool.Pool:
		if err := dbx.InPgxTx(ctx, typed, func(tx dbx.Tx) error { return fn(tx) }); err != nil {
			return fmt.Errorf("tracking pgx tx: %w", err)
		}

		return nil
	default:
		return errors.New("db does not support transactions")
	}
}
