package dbx

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/georgysavva/scany/v2/pgxscan"
)

func ExecSQL(ctx context.Context, db Querier, action, query string, args ...any) (int64, error) {
	tag, err := db.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", action, err)
	}

	return tag.RowsAffected(), nil
}

func SelectSQL(ctx context.Context, db Querier, dest any, action, query string, args ...any) error {
	if err := pgxscan.Select(ctx, db, dest, query, args...); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}

	return nil
}

func GetSQL(ctx context.Context, db Querier, dest any, action, query string, args ...any) (bool, error) {
	err := pgxscan.Get(ctx, db, dest, query, args...)
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
