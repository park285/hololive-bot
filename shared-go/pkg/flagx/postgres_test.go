package flagx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestValidateTableName(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		wantErr error
	}{
		{"valid simple", "user_flags", nil},
		{"valid underscore prefix", "_flags", nil},
		{"valid mixed case", "UserFlags", nil},
		{"valid with numbers", "flags_v2", nil},
		{"empty string", "", ErrInvalidTableName},
		{"starts with number", "1flags", ErrInvalidTableName},
		{"contains hyphen", "user-flags", ErrInvalidTableName},
		{"contains space", "user flags", ErrInvalidTableName},
		{"contains dot", "user.flags", ErrInvalidTableName},
		{"contains special char", "user@flags", ErrInvalidTableName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTableName(tt.table)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validateTableName(%q) = %v, want %v", tt.table, err, tt.wantErr)
			}
		})
	}
}

func TestNewPostgresRepository(t *testing.T) {
	t.Run("valid table name", func(t *testing.T) {
		repo, err := NewPostgresRepository(nil, "user_flags")
		if err != nil {
			t.Errorf("NewPostgresRepository() error = %v, want nil", err)
		}
		if repo == nil {
			t.Error("NewPostgresRepository() returned nil")
		}
	})

	t.Run("invalid table name", func(t *testing.T) {
		repo, err := NewPostgresRepository(nil, "123invalid")
		if !errors.Is(err, ErrInvalidTableName) {
			t.Errorf("NewPostgresRepository() error = %v, want ErrInvalidTableName", err)
		}
		if repo != nil {
			t.Error("NewPostgresRepository() should return nil on error")
		}
	})

	t.Run("empty table name", func(t *testing.T) {
		_, err := NewPostgresRepository(nil, "")
		if !errors.Is(err, ErrInvalidTableName) {
			t.Errorf("NewPostgresRepository() error = %v, want ErrInvalidTableName", err)
		}
	})
}

func TestNewPostgresRepositoryWithDB(t *testing.T) {
	t.Run("valid with mock db", func(t *testing.T) {
		mock := &mockDB{}
		repo, err := NewPostgresRepositoryWithDB(mock, "test_flags")
		if err != nil {
			t.Errorf("NewPostgresRepositoryWithDB() error = %v", err)
		}
		if repo == nil {
			t.Error("NewPostgresRepositoryWithDB() returned nil")
		}
	})

	t.Run("invalid table name", func(t *testing.T) {
		mock := &mockDB{}
		_, err := NewPostgresRepositoryWithDB(mock, "123invalid")
		if !errors.Is(err, ErrInvalidTableName) {
			t.Errorf("NewPostgresRepositoryWithDB() error = %v, want ErrInvalidTableName", err)
		}
	})
}

func TestPostgresRepository_ValidateFlag(t *testing.T) {
	repo := &PostgresRepository{
		db:        nil,
		tableName: "test_flags",
	}

	t.Run("Set with empty flag returns error", func(t *testing.T) {
		err := repo.Set(context.TODO(), "entity1", Flag(""), "trace1")
		if !errors.Is(err, ErrEmptyFlag) {
			t.Errorf("Set() with empty flag error = %v, want ErrEmptyFlag", err)
		}
	})

	t.Run("Unset with empty flag returns error", func(t *testing.T) {
		err := repo.Unset(context.TODO(), "entity1", Flag(""))
		if !errors.Is(err, ErrEmptyFlag) {
			t.Errorf("Unset() with empty flag error = %v, want ErrEmptyFlag", err)
		}
	})

	t.Run("Has with empty flag returns error", func(t *testing.T) {
		_, err := repo.Has(context.TODO(), "entity1", Flag(""))
		if !errors.Is(err, ErrEmptyFlag) {
			t.Errorf("Has() with empty flag error = %v, want ErrEmptyFlag", err)
		}
	})

	t.Run("ListByFlag with empty flag returns error", func(t *testing.T) {
		_, err := repo.ListByFlag(context.TODO(), Flag(""))
		if !errors.Is(err, ErrEmptyFlag) {
			t.Errorf("ListByFlag() with empty flag error = %v, want ErrEmptyFlag", err)
		}
	})
}

func TestPostgresRepository_InterfaceCompliance(t *testing.T) {
	var _ Repository = (*PostgresRepository)(nil)
}

func TestErrInvalidTableName(t *testing.T) {
	if ErrInvalidTableName.Error() != "flagx: invalid table name" {
		t.Errorf("ErrInvalidTableName = %q, want %q", ErrInvalidTableName.Error(), "flagx: invalid table name")
	}
}

type mockDB struct {
	execFunc     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	queryRowFunc func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFunc    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (m *mockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFunc != nil {
		return m.queryRowFunc(ctx, sql, args...)
	}
	return &mockRow{}
}

func (m *mockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, sql, args...)
	}
	return &mockRows{}, nil
}

type mockRow struct {
	scanFunc func(dest ...any) error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.scanFunc != nil {
		return r.scanFunc(dest...)
	}
	if len(dest) > 0 {
		if b, ok := dest[0].(*bool); ok {
			*b = true
		}
	}
	return nil
}

type mockRows struct {
	index int
	data  [][]any
}

func (r *mockRows) Close()                                       {}
func (r *mockRows) Err() error                                   { return nil }
func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }
func (r *mockRows) Values() ([]any, error)                       { return nil, nil }

func (r *mockRows) Next() bool {
	if r.data == nil {
		return false
	}
	if r.index < len(r.data) {
		r.index++
		return true
	}
	return false
}

func (r *mockRows) Scan(dest ...any) error {
	if r.data == nil || r.index < 1 || r.index > len(r.data) {
		return errors.New("no row")
	}
	row := r.data[r.index-1]
	for i, v := range row {
		if i >= len(dest) {
			break
		}
		switch d := dest[i].(type) {
		case *string:
			if s, ok := v.(string); ok {
				*d = s
			}
		case *time.Time:
			if t, ok := v.(time.Time); ok {
				*d = t
			}
		}
	}
	return nil
}

func TestPostgresRepository_Set_Success(t *testing.T) {
	mock := &mockDB{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	err := repo.Set(context.Background(), "entity1", Flag("active"), "trace123")
	if err != nil {
		t.Errorf("Set() error = %v, want nil", err)
	}
}

func TestPostgresRepository_Set_DBError(t *testing.T) {
	dbErr := errors.New("connection refused")
	mock := &mockDB{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, dbErr
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	err := repo.Set(context.Background(), "entity1", Flag("active"), "trace123")
	if err == nil {
		t.Error("Set() error = nil, want error")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("Set() error should wrap dbErr")
	}
}

func TestPostgresRepository_Unset_Success(t *testing.T) {
	mock := &mockDB{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	err := repo.Unset(context.Background(), "entity1", Flag("active"))
	if err != nil {
		t.Errorf("Unset() error = %v, want nil", err)
	}
}

func TestPostgresRepository_Unset_DBError(t *testing.T) {
	dbErr := errors.New("connection refused")
	mock := &mockDB{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, dbErr
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	err := repo.Unset(context.Background(), "entity1", Flag("active"))
	if err == nil {
		t.Error("Unset() error = nil, want error")
	}
}

func TestPostgresRepository_Has_Success(t *testing.T) {
	mock := &mockDB{
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					if len(dest) > 0 {
						if b, ok := dest[0].(*bool); ok {
							*b = true
						}
					}
					return nil
				},
			}
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	has, err := repo.Has(context.Background(), "entity1", Flag("active"))
	if err != nil {
		t.Errorf("Has() error = %v, want nil", err)
	}
	if !has {
		t.Error("Has() = false, want true")
	}
}

func TestPostgresRepository_Has_NotFound(t *testing.T) {
	mock := &mockDB{
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					if len(dest) > 0 {
						if b, ok := dest[0].(*bool); ok {
							*b = false
						}
					}
					return nil
				},
			}
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	has, err := repo.Has(context.Background(), "entity1", Flag("nonexistent"))
	if err != nil {
		t.Errorf("Has() error = %v, want nil", err)
	}
	if has {
		t.Error("Has() = true, want false")
	}
}

func TestPostgresRepository_Has_DBError(t *testing.T) {
	dbErr := errors.New("connection refused")
	mock := &mockDB{
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				scanFunc: func(dest ...any) error {
					return dbErr
				},
			}
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	_, err := repo.Has(context.Background(), "entity1", Flag("active"))
	if err == nil {
		t.Error("Has() error = nil, want error")
	}
}

func TestPostgresRepository_List_Success(t *testing.T) {
	now := time.Now()
	mock := &mockDB{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data: [][]any{
					{"entity1", "active", now, "trace1"},
					{"entity1", "premium", now, "trace2"},
				},
			}, nil
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	records, err := repo.List(context.Background(), "entity1")
	if err != nil {
		t.Errorf("List() error = %v, want nil", err)
	}
	if len(records) != 2 {
		t.Errorf("List() returned %d records, want 2", len(records))
	}
}

func TestPostgresRepository_List_Empty(t *testing.T) {
	mock := &mockDB{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{data: nil}, nil
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	records, err := repo.List(context.Background(), "entity1")
	if err != nil {
		t.Errorf("List() error = %v, want nil", err)
	}
	if records != nil {
		t.Errorf("List() returned %v, want nil", records)
	}
}

func TestPostgresRepository_List_DBError(t *testing.T) {
	dbErr := errors.New("connection refused")
	mock := &mockDB{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, dbErr
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	_, err := repo.List(context.Background(), "entity1")
	if err == nil {
		t.Error("List() error = nil, want error")
	}
}

func TestPostgresRepository_ListByFlag_Success(t *testing.T) {
	now := time.Now()
	mock := &mockDB{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data: [][]any{
					{"entity1", "active", now, "trace1"},
					{"entity2", "active", now, "trace2"},
				},
			}, nil
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	records, err := repo.ListByFlag(context.Background(), Flag("active"))
	if err != nil {
		t.Errorf("ListByFlag() error = %v, want nil", err)
	}
	if len(records) != 2 {
		t.Errorf("ListByFlag() returned %d records, want 2", len(records))
	}
}

func TestPostgresRepository_ListByFlag_DBError(t *testing.T) {
	dbErr := errors.New("connection refused")
	mock := &mockDB{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, dbErr
		},
	}
	repo, _ := NewPostgresRepositoryWithDB(mock, "test_flags")

	_, err := repo.ListByFlag(context.Background(), Flag("active"))
	if err == nil {
		t.Error("ListByFlag() error = nil, want error")
	}
}
