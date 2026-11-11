package observation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/dbx"
)

func writeRowPlaceholders(sb *strings.Builder, rowIndex, columns int) {
	base := rowIndex * columns

	sb.WriteByte('(')

	for j := range columns {
		if j > 0 {
			sb.WriteString(", ")
		}

		sb.WriteByte('$')
		sb.WriteString(strconv.Itoa(base + j + 1))
	}

	sb.WriteByte(')')
}

func inPgxTx(ctx context.Context, db trackingDB, fn func(tx trackingDB) error) error {
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
