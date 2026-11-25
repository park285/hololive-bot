package observation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func inPgxTx(ctx context.Context, db pgxdb.Querier, fn func(tx pgxdb.Querier) error) error {
	switch typed := db.(type) {
	case pgx.Tx:
		return fn(typed)
	case *pgxpool.Pool:
		if err := dbx.InPgxTx(ctx, typed, func(tx dbx.Tx) error { return fn(tx) }); err != nil {
			return fmt.Errorf("tracking pgx tx: %w", err)
		}

		return nil
	default:
		return errors.New("db does not support transactions")
	}
}
