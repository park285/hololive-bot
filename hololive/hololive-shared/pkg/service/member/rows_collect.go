package member

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type pgxRows = pgx.Rows

func collectJoinedRows[T any](rows pgxRows, iterLabel string, scan func(pgxRows) (T, error)) ([]T, error) {
	var (
		collected []T
		rowErrs   []error
	)

	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			rowErrs = append(rowErrs, err)
			continue
		}
		collected = append(collected, item)
	}

	if err := rows.Err(); err != nil {
		rowErrs = append(rowErrs, fmt.Errorf("%s: %w", iterLabel, err))
	}

	if len(rowErrs) > 0 {
		return collected, errors.Join(rowErrs...)
	}

	return collected, nil
}
