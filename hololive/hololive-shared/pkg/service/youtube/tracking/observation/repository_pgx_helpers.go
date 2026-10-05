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

// writeRowPlaceholders는 batch VALUES의 rowIndex번째 행 ($k+1, ..., $k+columns)를 쓴다. K = rowIndex*columns.
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
