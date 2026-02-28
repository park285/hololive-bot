package database

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// IsDuplicateKeyError: PostgreSQL unique constraint violation (23505) 여부를 확인한다.
func IsDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	return false
}

// IsNoRows: 쿼리 결과가 없는 경우(pgx.ErrNoRows)인지 확인한다.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
