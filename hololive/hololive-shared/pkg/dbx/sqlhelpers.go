package dbx

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/park285/shared-go/postgres/pgxdb"
)

func ExecSQL(ctx context.Context, db pgxdb.Querier, action string, query pgxdb.Statement, args ...any) (int64, error) {
	tag, err := pgxdb.Exec(ctx, db, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", action, err)
	}

	return tag.RowsAffected(), nil
}

func SelectSQL(ctx context.Context, db pgxdb.Querier, dest any, action string, query pgxdb.Statement, args ...any) error {
	rows, err := pgxdb.Query(ctx, db, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	if err := pgxscan.ScanAll(dest, rows); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	return nil
}

func GetSQL(ctx context.Context, db pgxdb.Querier, dest any, action string, query pgxdb.Statement, args ...any) (bool, error) {
	rows, err := pgxdb.Query(ctx, db, query, args...)
	if err != nil {
		return false, fmt.Errorf("%s: %w", action, err)
	}

	err = pgxscan.ScanOne(dest, rows)
	if err == nil {
		return true, nil
	}

	if pgxscan.NotFound(err) {
		return false, nil
	}

	return false, fmt.Errorf("%s: %w", action, err)
}

func WriteRowPlaceholders(sb *strings.Builder, rowIndex, columns int) {
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
