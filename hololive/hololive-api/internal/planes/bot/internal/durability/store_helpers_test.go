package durability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/ncruces/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func (s *Store) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := s.db.Writer().ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("test exec: %w", err)
	}

	return result, nil
}

func (s *Store) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.Writer().QueryRowContext(ctx, query, args...)
}

func requireConstraintViolation(t *testing.T, err error, message string) {
	t.Helper()
	require.Error(t, err)

	sqliteErr, ok := errors.AsType[*sqlite3.Error](err)
	if !ok {
		t.Fatalf("error type = %T, want *sqlite3.Error in chain", err)
	}

	require.Equal(t, sqlite3.CONSTRAINT, sqliteErr.Code())
	require.Contains(t, sqliteErr.Error(), message)
}

func (s *Store) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := s.db.Writer().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("test query: %w", err)
	}

	return rows, nil
}
